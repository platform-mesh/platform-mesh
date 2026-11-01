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

package kapply

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

// patchRecord captures details of a Patch call for assertions.
type patchRecord struct {
	GVR          schema.GroupVersionResource
	Namespace    string
	Name         string
	Kind         string
	APIVersion   string
	FieldManager string
	Force        bool
	DryRun       bool
}

type recorder struct {
	mu      sync.Mutex
	records []patchRecord
}

func (r *recorder) add(rec patchRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec)
}

// intercepting dynamic client that records Patch options while delegating method sets.
type interceptingDynamic struct {
	delegate dynamic.Interface
	rec      *recorder
}

func (d *interceptingDynamic) Resource(gvr schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	nsable := d.delegate.Resource(gvr)
	return &interceptNsable{NamespaceableResourceInterface: nsable, gvr: gvr, rec: d.rec}
}

type interceptNsable struct {
	dynamic.NamespaceableResourceInterface
	gvr schema.GroupVersionResource
	rec *recorder
}

func (n *interceptNsable) Namespace(ns string) dynamic.ResourceInterface {
	ri := n.NamespaceableResourceInterface.Namespace(ns)
	return &interceptRI{ResourceInterface: ri, gvr: n.gvr, ns: ns, rec: n.rec}
}

// Patch for root-scoped resources (no namespace).
func (n *interceptNsable) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	apiVersion, _ := m["apiVersion"].(string)
	kind, _ := m["kind"].(string)
	rec := patchRecord{
		GVR:          n.gvr,
		Namespace:    "",
		Name:         name,
		Kind:         kind,
		APIVersion:   apiVersion,
		FieldManager: opts.FieldManager,
		Force:        opts.Force != nil && *opts.Force,
		DryRun:       len(opts.DryRun) > 0,
	}
	n.rec.add(rec)
	return &unstructured.Unstructured{Object: m}, nil
}

type interceptRI struct {
	dynamic.ResourceInterface
	gvr schema.GroupVersionResource
	ns  string
	rec *recorder
}

func (r *interceptRI) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	// Decode the object to capture GVK and metadata.
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	apiVersion, _ := m["apiVersion"].(string)
	kind, _ := m["kind"].(string)
	rec := patchRecord{
		GVR:          r.gvr,
		Namespace:    r.ns,
		Name:         name,
		Kind:         kind,
		APIVersion:   apiVersion,
		FieldManager: opts.FieldManager,
		Force:        opts.Force != nil && *opts.Force,
		DryRun:       len(opts.DryRun) > 0,
	}
	r.rec.add(rec)
	// Return the object back to satisfy callers; no need to call the delegate.
	return &unstructured.Unstructured{Object: m}, nil
}

// fakeRESTMapper is a tiny mapper with optional Reset support to observe refreshes after CRDs.
type fakeRESTMapper struct {
	m      map[schema.GroupKind]*RESTMappingLite
	mu     sync.Mutex
	resets int
}

type RESTMappingLite struct {
	Resource schema.GroupVersionResource
	Scope    string // "root" or "namespace"
}

func newFakeRESTMapper() *fakeRESTMapper {
	fm := &fakeRESTMapper{m: map[schema.GroupKind]*RESTMappingLite{}}
	// Register common kinds used in tests.
	fm.m[schema.GroupKind{Group: "", Kind: "Namespace"}] = &RESTMappingLite{Resource: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}, Scope: "root"}
	fm.m[schema.GroupKind{Group: "", Kind: "ConfigMap"}] = &RESTMappingLite{Resource: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}, Scope: "namespace"}
	fm.m[schema.GroupKind{Group: "apiextensions.k8s.io", Kind: "CustomResourceDefinition"}] = &RESTMappingLite{Resource: schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}, Scope: "root"}
	return fm
}

// Implement meta.RESTMapper methods used by kapply.
// We only need RESTMapping; other methods return minimal values.

func (f *fakeRESTMapper) Reset() { // optional method used by kapply
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resets++
}

// RESTMapping returns a mapping for the given GroupKind.
func (f *fakeRESTMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*apimeta.RESTMapping, error) {
	if ml, ok := f.m[gk]; ok {
		var scope apimeta.RESTScope
		if ml.Scope == "namespace" {
			scope = apimeta.RESTScopeNamespace
		} else {
			scope = apimeta.RESTScopeRoot
		}
		return &apimeta.RESTMapping{
			Resource: ml.Resource,
			Scope:    scope,
		}, nil
	}
	return nil, fmt.Errorf("no mapping for %s", gk.String())
}

func (f *fakeRESTMapper) KindFor(resource schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, fmt.Errorf("not implemented")
}

func (f *fakeRESTMapper) KindsFor(resource schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeRESTMapper) ResourceFor(input schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	return schema.GroupVersionResource{}, fmt.Errorf("not implemented")
}

func (f *fakeRESTMapper) ResourcesFor(input schema.GroupVersionResource) ([]schema.GroupVersionResource, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeRESTMapper) RESTMappings(gk schema.GroupKind, versions ...string) ([]*apimeta.RESTMapping, error) {
	m, err := f.RESTMapping(gk, versions...)
	if err != nil {
		return nil, err
	}
	return []*apimeta.RESTMapping{m}, nil
}

func (f *fakeRESTMapper) ResourceSingularizer(resource string) (string, error) {
	// naive singularizer
	return strings.TrimSuffix(resource, "s"), nil
}

// Helpers to create kustomize directories for tests.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestApplyDir_DryRun_DefaultNamespace(t *testing.T) {
	t.Parallel()
	td := t.TempDir()
	// Kustomization with a single ConfigMap missing namespace.
	writeFile(t, td, "kustomization.yaml", "resources:\n- cm.yaml\n")
	writeFile(t, td, "cm.yaml", `apiVersion: v1
kind: ConfigMap
metadata:
  name: app-cm
data:
  k: v
`)

	// Dynamic client with interceptor to capture Patch options.
	delegate := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	rec := &recorder{}
	dyn := &interceptingDynamic{delegate: delegate, rec: rec}
	mapper := newFakeRESTMapper()

	ctx := context.Background()
	err := ApplyDir(ctx, td, Clients{Dynamic: dyn, Mapper: mapper},
		WithDefaultNamespace("test-ns"),
		WithDryRun(),
		WithFieldManager("tester"),
		WithForceConflicts(false),
	)
	require.NoError(t, err)

	require.Len(t, rec.records, 1)
	r := rec.records[0]
	require.Equal(t, "configmaps", r.GVR.Resource)
	require.Equal(t, "test-ns", r.Namespace)
	require.Equal(t, "app-cm", r.Name)
	require.Equal(t, "v1", r.APIVersion)
	require.Equal(t, "ConfigMap", r.Kind)
	require.True(t, r.DryRun)
	require.False(t, r.Force)
	require.Equal(t, "tester", r.FieldManager)
}

func TestApplyDir_PreapplyOrder(t *testing.T) {
	t.Parallel()
	td := t.TempDir()
	writeFile(t, td, "kustomization.yaml", "resources:\n- ns.yaml\n- cm.yaml\n")
	writeFile(t, td, "ns.yaml", `apiVersion: v1
kind: Namespace
metadata:
  name: demo
`)
	writeFile(t, td, "cm.yaml", `apiVersion: v1
kind: ConfigMap
metadata:
  name: demo-cm
data:
  a: b
`)

	delegate := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	rec := &recorder{}
	dyn := &interceptingDynamic{delegate: delegate, rec: rec}
	mapper := newFakeRESTMapper()

	// Default namespace will be applied to the ConfigMap.
	err := ApplyDir(context.Background(), td, Clients{Dynamic: dyn, Mapper: mapper}, WithDefaultNamespace("demo"))
	require.NoError(t, err)

	// Expect first call to be Namespace (pre-applied), and one of the calls to be ConfigMap last in overall apply.
	require.GreaterOrEqual(t, len(rec.records), 2)
	require.Equal(t, "Namespace", rec.records[0].Kind)
	last := rec.records[len(rec.records)-1]
	require.Equal(t, "ConfigMap", last.Kind)
	require.Equal(t, "demo", last.Namespace)
}

func TestApplyDir_CRD_ResetCalled(t *testing.T) {
	t.Parallel()
	td := t.TempDir()
	writeFile(t, td, "kustomization.yaml", "resources:\n- crd.yaml\n")
	writeFile(t, td, "crd.yaml", `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: examples.example.com
spec:
  group: example.com
  names:
    kind: Example
    plural: examples
  scope: Namespaced
  versions:
  - name: v1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
`)

	delegate := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	rec := &recorder{}
	dyn := &interceptingDynamic{delegate: delegate, rec: rec}
	mapper := newFakeRESTMapper()

	err := ApplyDir(context.Background(), td, Clients{Dynamic: dyn, Mapper: mapper}, WithWaitAfterCRDs(time.Nanosecond))
	require.NoError(t, err)

	// Accept one or more CRD applies (pre-apply + full apply).
	require.GreaterOrEqual(t, len(rec.records), 1)
	for _, r := range rec.records {
		require.Equal(t, "CustomResourceDefinition", r.Kind)
		require.Equal(t, "customresourcedefinitions", r.GVR.Resource)
		require.Equal(t, "apiextensions.k8s.io/v1", r.APIVersion)
	}

	mapper.mu.Lock()
	resets := mapper.resets
	mapper.mu.Unlock()
	require.GreaterOrEqual(t, resets, 1)
}

func TestApplyDir_RESTMappingErrorForUnknownKind(t *testing.T) {
	t.Parallel()
	td := t.TempDir()
	writeFile(t, td, "kustomization.yaml", "resources:\n- x.yaml\n")
	writeFile(t, td, "x.yaml", `apiVersion: v1
kind: FooBar
metadata:
  name: foo
`)

	delegate := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	rec := &recorder{}
	dyn := &interceptingDynamic{delegate: delegate, rec: rec}
	mapper := newFakeRESTMapper()

	err := ApplyDir(context.Background(), td, Clients{Dynamic: dyn, Mapper: mapper})
	require.Error(t, err)
	require.Contains(t, err.Error(), "RESTMapping")
}
