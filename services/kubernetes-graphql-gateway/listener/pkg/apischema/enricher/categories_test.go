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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

func TestCategoriesEnricher(t *testing.T) {
	podSchema := &spec.Schema{
		VendorExtensible: spec.VendorExtensible{
			Extensions: map[string]any{
				pmgateway.GVKExtensionKey: []map[string]any{
					{"group": "", "version": "v1", "kind": "Pod"},
				},
			},
		},
	}

	schemas := apischema.NewSchemaSetFromMap(map[string]*spec.Schema{
		"v1.Pod": podSchema,
	})

	apiResources := []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{
					Name:       "pods",
					Kind:       "Pod",
					Namespaced: true,
					Categories: []string{"all"},
				},
			},
		},
	}

	e := enricher.NewCategories(apiResources)

	err := e.Enrich(t.Context(), schemas)
	assert.NoError(t, err)

	podEntry, _ := schemas.Get("v1.Pod")
	categories := podEntry.Schema.Extensions[pmgateway.CategoriesExtensionKey]
	assert.Equal(t, []string{"all"}, categories)
}

func TestCategoriesEnricher_NoCategories(t *testing.T) {
	podSchema := &spec.Schema{
		VendorExtensible: spec.VendorExtensible{
			Extensions: map[string]any{
				pmgateway.GVKExtensionKey: []map[string]any{
					{"group": "", "version": "v1", "kind": "Pod"},
				},
			},
		},
	}

	schemas := apischema.NewSchemaSetFromMap(map[string]*spec.Schema{
		"v1.Pod": podSchema,
	})

	// API resource with no categories
	apiResources := []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{
					Name:       "pods",
					Kind:       "Pod",
					Namespaced: true,
					// No Categories field
				},
			},
		},
	}

	e := enricher.NewCategories(apiResources)

	err := e.Enrich(t.Context(), schemas)
	assert.NoError(t, err)

	podEntry, _ := schemas.Get("v1.Pod")
	_, hasCategories := podEntry.Schema.Extensions[pmgateway.CategoriesExtensionKey]
	assert.False(t, hasCategories, "should not have categories extension")
}

func TestCategoriesEnricherName(t *testing.T) {
	e := enricher.NewCategories(nil)
	assert.Equal(t, "categories", e.Name())
}
