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
	"context"
	"embed"
	"fmt"

	"go.platform-mesh.io/platform-mesh/tests/framework/fixtures"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// The objects a scenario creates that are not a few fields long are kept as
// YAML in manifests/, the way a user would apply them. They are read as
// unstructured objects (fixtures.DecodeYAML), so that what is written is
// what the file says and not what a Go type makes of it.

//go:embed manifests/*.yaml
var manifests embed.FS

// manifest returns the objects of the file name in manifests/.
func manifest(name string) ([]*unstructured.Unstructured, error) {
	data, err := manifests.ReadFile("manifests/" + name)
	if err != nil {
		return nil, err
	}
	objects, err := fixtures.DecodeYAML(data)
	if err != nil {
		return nil, fmt.Errorf("manifest %s: %w", name, err)
	}
	if len(objects) == 0 {
		return nil, fmt.Errorf("manifest %s holds no object", name)
	}
	return objects, nil
}

// create creates the objects of the manifest with the client, in the order
// of the file, unless they exist. What exists is left as it is.
func create(ctx context.Context, cl ctrlruntimeclient.Client, name string) error {
	objects, err := manifest(name)
	if err != nil {
		return err
	}
	for _, obj := range objects {
		if err := fixtures.Create(ctx, cl, obj); err != nil {
			return err
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

// copyOf returns the first object of the manifest under another name: its
// own with the suffix. The After steps create and delete such copies, so
// that what Seed created stays as Validate expects it.
func copyOf(name, suffix string) (*unstructured.Unstructured, error) {
	objects, err := manifest(name)
	if err != nil {
		return nil, err
	}
	objects[0].SetName(objects[0].GetName() + suffix)
	return objects[0], nil
}

// createCopy creates the copy of the first object of the manifest with the
// client, unless it exists, and reads it back.
func createCopy(ctx context.Context, cl ctrlruntimeclient.Client, name, suffix string) error {
	obj, err := copyOf(name, suffix)
	if err != nil {
		return err
	}
	if err := fixtures.Create(ctx, cl, obj); err != nil {
		return err
	}
	return cl.Get(ctx, ctrlruntimeclient.ObjectKeyFromObject(obj), obj)
}

// deleteCopy deletes the copy createCopy made with the client, and returns
// an error until it is gone.
func deleteCopy(ctx context.Context, cl ctrlruntimeclient.Client, name, suffix string) error {
	obj, err := copyOf(name, suffix)
	if err != nil {
		return err
	}
	if err := ctrlruntimeclient.IgnoreNotFound(cl.Delete(ctx, obj)); err != nil {
		return fmt.Errorf("deleting %s %s: %w", obj.GetKind(), obj.GetName(), err)
	}
	return wait.CheckGone(cl.Get(ctx, ctrlruntimeclient.ObjectKeyFromObject(obj), obj), obj.GetKind()+" "+obj.GetName())
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
