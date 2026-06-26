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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

func TestScopeEnricher(t *testing.T) {
	podSchema := &spec.Schema{
		VendorExtensible: spec.VendorExtensible{
			Extensions: map[string]any{
				pmgateway.GVKExtensionKey: []map[string]any{
					{"group": "", "version": "v1", "kind": "Pod"},
				},
			},
		},
	}
	nodeSchema := &spec.Schema{
		VendorExtensible: spec.VendorExtensible{
			Extensions: map[string]any{
				pmgateway.GVKExtensionKey: []map[string]any{
					{"group": "", "version": "v1", "kind": "Node"},
				},
			},
		},
	}

	schemas := apischema.NewSchemaSetFromMap(map[string]*spec.Schema{
		"v1.Pod":  podSchema,
		"v1.Node": nodeSchema,
	})

	// Create a REST mapper that marks Pod as namespaced, Node as cluster-scoped
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Pod"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Node"}, meta.RESTScopeRoot)

	e := enricher.NewScope(mapper)

	err := e.Enrich(t.Context(), schemas)
	assert.NoError(t, err)

	// Check Pod is namespaced
	podEntry, ok := schemas.Get("v1.Pod")
	assert.True(t, ok, "expected v1.Pod to exist in schema set")
	assert.Equal(t, apiextensionsv1.NamespaceScoped, podEntry.Schema.Extensions[pmgateway.ScopeExtensionKey])

	// Check Node is cluster-scoped
	nodeEntry, ok := schemas.Get("v1.Node")
	assert.True(t, ok, "expected v1.Node to exist in schema set")
	assert.Equal(t, apiextensionsv1.ClusterScoped, nodeEntry.Schema.Extensions[pmgateway.ScopeExtensionKey])
}

func TestScopeEnricherName(t *testing.T) {
	e := enricher.NewScope(nil)
	assert.Equal(t, "scope", e.Name())
}
