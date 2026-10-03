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

package apischema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmgateway "go.platform-mesh.io/apis/gateway"
	"go.platform-mesh.io/kubernetes-graphql-gateway/apischema"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

func TestResourceSelectorMatches(t *testing.T) {
	tests := []struct {
		name     string
		selector apischema.ResourceSelector
		gvr      schema.GroupVersionResource
		want     bool
	}{
		{
			name:     "group only",
			selector: apischema.ResourceSelector{Group: "apps"},
			gvr:      schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			want:     true,
		},
		{
			name:     "group and version",
			selector: apischema.ResourceSelector{Group: "apps", Version: "v1"},
			gvr:      schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			want:     true,
		},
		{
			name:     "group and resource",
			selector: apischema.ResourceSelector{Group: "apps", Resource: "deployments"},
			gvr:      schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			want:     true,
		},
		{
			name:     "exact GVR",
			selector: apischema.ResourceSelector{Group: "apps", Version: "v1", Resource: "deployments"},
			gvr:      schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			want:     true,
		},
		{
			name:     "core group",
			selector: apischema.ResourceSelector{Group: "", Version: "v1", Resource: "pods"},
			gvr:      schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			want:     true,
		},
		{
			name:     "resource name is not derived from kind",
			selector: apischema.ResourceSelector{Group: "example.io", Resource: "people"},
			gvr:      schema.GroupVersionResource{Group: "example.io", Version: "v1", Resource: "people"},
			want:     true,
		},
		{
			name:     "group mismatch",
			selector: apischema.ResourceSelector{Group: "batch"},
			gvr:      schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			want:     false,
		},
		{
			name:     "version mismatch",
			selector: apischema.ResourceSelector{Group: "apps", Version: "v1beta1"},
			gvr:      schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			want:     false,
		},
		{
			name:     "resource mismatch",
			selector: apischema.ResourceSelector{Group: "apps", Resource: "statefulsets"},
			gvr:      schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.selector.Matches(tt.gvr))
		})
	}
}

func TestSchemaSetSelectResources(t *testing.T) {
	pod := resourceSchema("", "v1", "Pod", "support.a")
	deployment := resourceSchema("apps", "v1", "Deployment")
	supportA := schemaWithRefs("support.b", "apps.v1.StatefulSet")
	supportB := schemaWithRefs("support.a")
	statefulSet := resourceSchema("apps", "v1", "StatefulSet")
	unreferenced := &spec.Schema{}

	original := apischema.NewSchemaSetFromMap(map[string]*spec.Schema{
		"core.v1.Pod":          pod,
		"apps.v1.Deployment":   deployment,
		"apps.v1.StatefulSet":  statefulSet,
		"support.a":            supportA,
		"support.b":            supportB,
		"support.unreferenced": unreferenced,
	})

	mapper := newRESTMapper(map[schema.GroupVersionKind]schema.GroupVersionResource{
		{Group: "", Version: "v1", Kind: "Pod"}: {Group: "", Version: "v1", Resource: "pods"},
	})
	selected, err := original.SelectResources([]apischema.ResourceSelector{
		{Group: "", Version: "v1", Resource: "pods"},
		{Group: "", Version: "v1", Resource: "pods"},
	}, mapper)
	require.NoError(t, err)

	assert.Equal(t, 6, original.Size(), "source set must not be changed")
	assert.Equal(t, 4, selected.Size())
	assert.Contains(t, selected.All(), "core.v1.Pod")
	assert.Contains(t, selected.All(), "support.a")
	assert.Contains(t, selected.All(), "support.b")
	assert.Contains(t, selected.All(), "apps.v1.StatefulSet")
	assert.NotContains(t, selected.All(), "apps.v1.Deployment")
	assert.NotContains(t, selected.All(), "support.unreferenced")

	_, ok := selected.GetByGVK(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"})
	assert.True(t, ok)
	_, ok = selected.GetByGVK(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"})
	assert.False(t, ok, "excluded referenced GVK must not remain a resource root")

	retainedDependency, ok := selected.Get("apps.v1.StatefulSet")
	require.True(t, ok)
	assert.Nil(t, retainedDependency.GVK)
	assert.NotContains(t, retainedDependency.Schema.Extensions, pmgateway.GVKExtensionKey)
	assert.Contains(t, statefulSet.Extensions, pmgateway.GVKExtensionKey, "source schema must not be mutated")
}

func TestSchemaSetSelectResourcesMultipleSelectors(t *testing.T) {
	schemas := apischema.NewSchemaSetFromMap(map[string]*spec.Schema{
		"core.v1.Pod":        resourceSchema("", "v1", "Pod"),
		"apps.v1.Deployment": resourceSchema("apps", "v1", "Deployment"),
		"batch.v1.Job":       resourceSchema("batch", "v1", "Job"),
	})

	mapper := newRESTMapper(map[schema.GroupVersionKind]schema.GroupVersionResource{
		{Group: "", Version: "v1", Kind: "Pod"}:            {Group: "", Version: "v1", Resource: "pods"},
		{Group: "apps", Version: "v1", Kind: "Deployment"}: {Group: "apps", Version: "v1", Resource: "deployments"},
	})
	selected, err := schemas.SelectResources([]apischema.ResourceSelector{
		{Group: "", Resource: "pods"},
		{Group: "apps"},
	}, mapper)
	require.NoError(t, err)
	assert.Equal(t, 2, selected.Size())
	assert.Contains(t, selected.All(), "core.v1.Pod")
	assert.Contains(t, selected.All(), "apps.v1.Deployment")
}

func TestSchemaSetSelectResourcesUsesRESTMapping(t *testing.T) {
	personGVK := schema.GroupVersionKind{Group: "example.io", Version: "v1", Kind: "Person"}
	schemas := apischema.NewSchemaSetFromMap(map[string]*spec.Schema{
		"example.v1.Person": resourceSchema(personGVK.Group, personGVK.Version, personGVK.Kind),
	})
	mapper := newRESTMapper(map[schema.GroupVersionKind]schema.GroupVersionResource{
		personGVK: {Group: "example.io", Version: "v1", Resource: "people"},
	})

	selected, err := schemas.SelectResources(
		[]apischema.ResourceSelector{{Group: "example.io", Resource: "people"}},
		mapper,
	)
	require.NoError(t, err)
	assert.Contains(t, selected.All(), "example.v1.Person")
}

func TestSchemaSetSelectResourcesMissingReference(t *testing.T) {
	schemas := apischema.NewSchemaSetFromMap(map[string]*spec.Schema{
		"core.v1.Pod": resourceSchema("", "v1", "Pod", "missing.definition"),
	})

	mapper := newRESTMapper(map[schema.GroupVersionKind]schema.GroupVersionResource{
		{Group: "", Version: "v1", Kind: "Pod"}: {Group: "", Version: "v1", Resource: "pods"},
	})
	selected, err := schemas.SelectResources(
		[]apischema.ResourceSelector{{Group: "", Resource: "pods"}},
		mapper,
	)
	assert.Nil(t, selected)
	assert.ErrorIs(t, err, apischema.ErrSchemaReferenceNotFound)
	assert.Contains(t, err.Error(), "missing.definition")
}

func newRESTMapper(mappings map[schema.GroupVersionKind]schema.GroupVersionResource) meta.RESTMapper {
	groupVersions := make([]schema.GroupVersion, 0, len(mappings))
	for gvk := range mappings {
		groupVersions = append(groupVersions, gvk.GroupVersion())
	}

	mapper := meta.NewDefaultRESTMapper(groupVersions)
	for gvk, gvr := range mappings {
		mapper.AddSpecific(gvk, gvr, gvr, meta.RESTScopeNamespace)
	}
	return mapper
}

func resourceSchema(group, version, kind string, refs ...string) *spec.Schema {
	schema := schemaWithRefs(refs...)
	schema.Extensions = map[string]any{
		pmgateway.GVKExtensionKey: []map[string]any{{
			"group": group, "version": version, "kind": kind,
		}},
	}
	return schema
}

func schemaWithRefs(refs ...string) *spec.Schema {
	schema := &spec.Schema{}
	for _, ref := range refs {
		schema.AllOf = append(schema.AllOf, spec.Schema{
			SchemaProps: spec.SchemaProps{Ref: spec.MustCreateRef(ref)},
		})
	}
	return schema
}
