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

// Package fixtures loads manifests into kcp workspaces and installs the
// platform-mesh-system tree (Install).
package fixtures

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const apisGroup = "apis.kcp.io"

// Decode splits multi-document YAML (or JSON) into unstructured objects.
// Empty documents are skipped.
func Decode(t testing.TB, raw []byte) []*unstructured.Unstructured {
	t.Helper()
	objs, err := DecodeYAML(raw)
	require.NoError(t, err)
	return objs
}

// DecodeYAML is Decode for a caller without a test at hand.
func DecodeYAML(raw []byte) ([]*unstructured.Unstructured, error) {
	var objs []*unstructured.Unstructured
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(raw), 4096)
	for {
		obj := map[string]any{}
		err := decoder.Decode(&obj)
		if errors.Is(err, io.EOF) {
			return objs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("decoding YAML: %w", err)
		}
		if len(obj) == 0 {
			continue
		}
		objs = append(objs, &unstructured.Unstructured{Object: obj})
	}
}

// Apply creates objs in cl's workspace, tolerating AlreadyExists. Objects may
// be typed or unstructured.
func Apply(t testing.TB, cl ctrlruntimeclient.Client, objs ...ctrlruntimeclient.Object) {
	t.Helper()
	require.NoError(t, Create(t.Context(), cl, objs...))
}

// Create is Apply for a caller without a test at hand: it creates objs in
// order, leaves what exists as it is, and returns the first other error.
func Create(ctx context.Context, cl ctrlruntimeclient.Client, objs ...ctrlruntimeclient.Object) error {
	for _, obj := range objs {
		if err := cl.Create(ctx, obj); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("creating %s %s: %w", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(), err)
		}
	}
	return nil
}

// ApplyFS applies every file in fsys matching glob into cl's workspace,
// schemas first, then exports, then the rest, with permission claim
// identity hashes filled in from the workspace's bindings.
func ApplyFS(t testing.TB, cl ctrlruntimeclient.Client, fsys fs.FS, glob string) {
	t.Helper()
	objs := readFS(t, fsys, glob)
	ids := boundIdentities(t, cl)
	for _, obj := range objs {
		ids.patch(obj)
	}
	Apply(t, cl, toObjects(sortForApply(objs))...)
}

// readFS decodes every file in fsys matching glob, in lexical file order.
func readFS(t testing.TB, fsys fs.FS, glob string) []*unstructured.Unstructured {
	t.Helper()
	files, err := fs.Glob(fsys, glob)
	require.NoError(t, err, "matching %q", glob)
	require.NotEmpty(t, files, "no files match %q", glob)
	objs := make([]*unstructured.Unstructured, 0, len(files))
	for _, file := range files {
		raw, err := fs.ReadFile(fsys, file)
		require.NoError(t, err, "reading %s", file)
		objs = append(objs, Decode(t, raw)...)
	}
	return objs
}

// sortForApply orders APIResourceSchemas before APIExports before the rest,
// keeping the order within each kind.
func sortForApply(objs []*unstructured.Unstructured) []*unstructured.Unstructured {
	rank := func(obj *unstructured.Unstructured) int {
		if obj.GroupVersionKind().Group != apisGroup {
			return 2
		}
		switch obj.GetKind() {
		case "APIResourceSchema":
			return 0
		case "APIExport":
			return 1
		default:
			return 2
		}
	}
	sorted := slices.Clone(objs)
	slices.SortStableFunc(sorted, func(a, b *unstructured.Unstructured) int { return rank(a) - rank(b) })
	return sorted
}

func toObjects(objs []*unstructured.Unstructured) []ctrlruntimeclient.Object {
	out := make([]ctrlruntimeclient.Object, 0, len(objs))
	for _, obj := range objs {
		out = append(out, obj)
	}
	return out
}
