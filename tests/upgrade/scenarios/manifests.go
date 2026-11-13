/*
Copyright The Platform Mesh Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package scenarios

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// The objects a scenario creates that are not a few fields long are kept as
// YAML in manifests/, the way a user would apply them. They are read as
// unstructured objects, so their types need not be in the scheme of the
// clients.

//go:embed manifests/*.yaml
var manifests embed.FS

// manifest returns the objects of the file name in manifests/.
func manifest(name string) ([]*unstructured.Unstructured, error) {
	data, err := manifests.ReadFile("manifests/" + name)
	if err != nil {
		return nil, err
	}
	var objects []*unstructured.Unstructured
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	for {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(&obj.Object); errors.Is(err, io.EOF) {
			return objects, nil
		} else if err != nil {
			return nil, fmt.Errorf("manifest %s: %w", name, err)
		}
		if len(obj.Object) != 0 {
			objects = append(objects, obj)
		}
	}
}

// create creates the objects of the manifest with the client, in the order
// of the file, unless they exist. What exists is left as it is.
func create(ctx context.Context, cl ctrlruntimeclient.Client, name string) error {
	objects, err := manifest(name)
	if err != nil {
		return err
	}
	for _, obj := range objects {
		if err := cl.Create(ctx, obj); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("creating %s %s: %w", obj.GetKind(), obj.GetName(), err)
		}
	}
	return nil
}

// existing reads the objects of the manifest with the client, as they are
// in the installation. It returns the error of the first that is not
// there.
func existing(ctx context.Context, cl ctrlruntimeclient.Client, name string) ([]*unstructured.Unstructured, error) {
	objects, err := manifest(name)
	if err != nil {
		return nil, err
	}
	for i, want := range objects {
		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(want.GroupVersionKind())
		if err := cl.Get(ctx, ctrlruntimeclient.ObjectKeyFromObject(want), got); err != nil {
			return nil, fmt.Errorf("%s %s: %w", want.GetKind(), want.GetName(), err)
		}
		objects[i] = got
	}
	return objects, nil
}

// seedManifest creates the objects of the manifest in the workspace at
// path, as the kcp admin.
func seedManifest(path, name string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := adminClient(ctx, env, path)
		if err != nil {
			return err
		}
		return create(ctx, cl, name)
	}
}

// validateManifest: the objects of the manifest are in the workspace at
// path.
func validateManifest(path, name string) stepFunc {
	return func(ctx context.Context, env Environment) error {
		cl, err := adminClient(ctx, env, path)
		if err != nil {
			return err
		}
		_, err = existing(ctx, cl, name)
		return err
	}
}

// ready returns an error unless the object has the condition Ready with the
// status True.
func ready(obj *unstructured.Unstructured) error {
	conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conditions {
		if c, ok := c.(map[string]any); ok && c["type"] == "Ready" {
			if c["status"] == "True" {
				return nil
			}
			return fmt.Errorf("%s %s is not ready: %v: %v", obj.GetKind(), obj.GetName(), c["reason"], c["message"])
		}
	}
	return fmt.Errorf("%s %s has no Ready condition yet", obj.GetKind(), obj.GetName())
}
