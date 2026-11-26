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

package enricher_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	pmgateway "go.platform-mesh.io/apis/gateway"
	"go.platform-mesh.io/kubernetes-graphql-gateway/apischema"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/pkg/apischema/enricher"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

func gvkSchema(group, version, kind string) *spec.Schema {
	return &spec.Schema{
		VendorExtensible: spec.VendorExtensible{
			Extensions: map[string]any{
				pmgateway.GVKExtensionKey: []map[string]any{
					{"group": group, "version": version, "kind": kind},
				},
			},
		},
	}
}

// TestScopeEnricher_FromMapper verifies scope resolution via the RESTMapper
// (used as the fallback source when discovery lacks the GVK).
func TestScopeEnricher_FromMapper(t *testing.T) {
	podSchema := gvkSchema("", "v1", "Pod")
	nodeSchema := gvkSchema("", "v1", "Node")

	schemas := apischema.NewSchemaSetFromMap(map[string]*spec.Schema{
		"v1.Pod":  podSchema,
		"v1.Node": nodeSchema,
	})

	// Create a REST mapper that marks Pod as namespaced, Node as cluster-scoped
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Pod"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Node"}, meta.RESTScopeRoot)

	// nil discovery list forces the RESTMapper fallback path.
	e := enricher.NewScope(mapper, nil)

	err := e.Enrich(t.Context(), schemas)
	assert.NoError(t, err)

	podEntry, ok := schemas.Get("v1.Pod")
	assert.True(t, ok, "expected v1.Pod to exist in schema set")
	assert.Equal(t, apiextensionsv1.NamespaceScoped, podEntry.Schema.Extensions[pmgateway.ScopeExtensionKey])

	nodeEntry, ok := schemas.Get("v1.Node")
	assert.True(t, ok, "expected v1.Node to exist in schema set")
	assert.Equal(t, apiextensionsv1.ClusterScoped, nodeEntry.Schema.Extensions[pmgateway.ScopeExtensionKey])
}

// TestScopeEnricher_FromDiscovery verifies scope is derived from the discovery
// resource list (the primary source), with no RESTMapper provided.
func TestScopeEnricher_FromDiscovery(t *testing.T) {
	podSchema := gvkSchema("", "v1", "Pod")
	nodeSchema := gvkSchema("", "v1", "Node")

	schemas := apischema.NewSchemaSetFromMap(map[string]*spec.Schema{
		"v1.Pod":  podSchema,
		"v1.Node": nodeSchema,
	})

	resources := []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "pods", Kind: "Pod", Namespaced: true},
				{Name: "nodes", Kind: "Node", Namespaced: false},
			},
		},
	}

	// No RESTMapper: discovery alone must resolve scope.
	e := enricher.NewScope(nil, resources)
	assert.NoError(t, e.Enrich(t.Context(), schemas))

	podEntry, ok := schemas.Get("v1.Pod")
	assert.True(t, ok)
	assert.Equal(t, apiextensionsv1.NamespaceScoped, podEntry.Schema.Extensions[pmgateway.ScopeExtensionKey])

	nodeEntry, ok := schemas.Get("v1.Node")
	assert.True(t, ok)
	assert.Equal(t, apiextensionsv1.ClusterScoped, nodeEntry.Schema.Extensions[pmgateway.ScopeExtensionKey])
}

// TestScopeEnricher_DiscoveryPreferredOverMapper verifies the discovery list is
// used even when a RESTMapper is also provided (and lacks the mapping).
func TestScopeEnricher_DiscoveryPreferredOverMapper(t *testing.T) {
	widgetSchema := gvkSchema("example.com", "v1", "Widget")
	schemas := apischema.NewSchemaSetFromMap(map[string]*spec.Schema{
		"example.com.v1.Widget": widgetSchema,
	})

	resources := []*metav1.APIResourceList{
		{
			GroupVersion: "example.com/v1",
			APIResources: []metav1.APIResource{
				{Name: "widgets", Kind: "Widget", Namespaced: true},
			},
		},
	}
	// Mapper intentionally left without the mapping; discovery should win.
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "example.com", Version: "v1"}})

	e := enricher.NewScope(mapper, resources)
	assert.NoError(t, e.Enrich(t.Context(), schemas))

	widgetEntry, ok := schemas.Get("example.com.v1.Widget")
	assert.True(t, ok)
	assert.Equal(t, apiextensionsv1.NamespaceScoped, widgetEntry.Schema.Extensions[pmgateway.ScopeExtensionKey])
}

// TestScopeEnricher_UnresolvedLeavesNoExtension verifies that when neither
// discovery nor the mapper can resolve scope, no extension is added (the
// resource will be dropped downstream) and Enrich does not error.
func TestScopeEnricher_UnresolvedLeavesNoExtension(t *testing.T) {
	orphanSchema := gvkSchema("example.com", "v1", "Orphan")
	schemas := apischema.NewSchemaSetFromMap(map[string]*spec.Schema{
		"example.com.v1.Orphan": orphanSchema,
	})

	// No discovery entry, empty mapper -> cannot resolve.
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "example.com", Version: "v1"}})
	e := enricher.NewScope(mapper, nil)

	assert.NoError(t, e.Enrich(t.Context(), schemas))

	orphanEntry, ok := schemas.Get("example.com.v1.Orphan")
	assert.True(t, ok)
	_, hasScope := orphanEntry.Schema.Extensions[pmgateway.ScopeExtensionKey]
	assert.False(t, hasScope, "unresolved scope must not add an extension")
}

func TestScopeEnricherName(t *testing.T) {
	e := enricher.NewScope(nil, nil)
	assert.Equal(t, "scope", e.Name())
}
