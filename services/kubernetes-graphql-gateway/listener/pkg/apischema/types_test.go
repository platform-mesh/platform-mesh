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

	"github.com/go-openapi/testify/v2/assert"

	pmgateway "go.platform-mesh.io/apis/gateway"
	"go.platform-mesh.io/kubernetes-graphql-gateway/apischema"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

func TestSchemaSet_O1_Lookups(t *testing.T) {
	// Create test schemas with GVK extensions
	podSchema := &spec.Schema{
		VendorExtensible: spec.VendorExtensible{
			Extensions: map[string]any{
				pmgateway.GVKExtensionKey: []map[string]any{
					{"group": "", "version": "v1", "kind": "Pod"},
				},
			},
		},
	}
	deploymentSchema := &spec.Schema{
		VendorExtensible: spec.VendorExtensible{
			Extensions: map[string]any{
				pmgateway.GVKExtensionKey: []map[string]any{
					{"group": "apps", "version": "v1", "kind": "Deployment"},
				},
			},
		},
	}
	// Another Pod from a custom group (same kind name)
	customPodSchema := &spec.Schema{
		VendorExtensible: spec.VendorExtensible{
			Extensions: map[string]any{
				pmgateway.GVKExtensionKey: []map[string]any{
					{"group": "custom.io", "version": "v1", "kind": "Pod"},
				},
			},
		},
	}

	schemas := apischema.NewSchemaSetFromMap(map[string]*spec.Schema{
		"v1.Pod":             podSchema,
		"apps.v1.Deployment": deploymentSchema,
		"custom.io.v1.Pod":   customPodSchema,
	})

	t.Run("Get by key", func(t *testing.T) {
		entry, ok := schemas.Get("v1.Pod")
		assert.True(t, ok)
		assert.Equal(t, "v1.Pod", entry.Key)
	})

	t.Run("Get by GVK - core Pod", func(t *testing.T) {
		entry, ok := schemas.GetByGVK(schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Pod",
		})
		assert.True(t, ok)
		assert.Equal(t, "v1.Pod", entry.Key)
	})

	t.Run("Get by GVK - apps Deployment", func(t *testing.T) {
		entry, ok := schemas.GetByGVK(schema.GroupVersionKind{
			Group:   "apps",
			Version: "v1",
			Kind:    "Deployment",
		})
		assert.True(t, ok)
		assert.Equal(t, "apps.v1.Deployment", entry.Key)
	})

	t.Run("FindByKind returns all matching kinds", func(t *testing.T) {
		// Should find both core Pod and custom.io Pod
		pods := schemas.FindByKind("Pod")
		assert.Len(t, pods, 2)

		// Should find only Deployment
		deployments := schemas.FindByKind("Deployment")
		assert.Len(t, deployments, 1)

		// Case insensitive
		podsLower := schemas.FindByKind("pod")
		assert.Len(t, podsLower, 2)
	})

	t.Run("FindByKind returns nil for unknown kind", func(t *testing.T) {
		unknown := schemas.FindByKind("Unknown")
		assert.Nil(t, unknown)
	})

	t.Run("Size returns correct count", func(t *testing.T) {
		assert.Equal(t, 3, schemas.Size())
	})

	t.Run("All returns all entries", func(t *testing.T) {
		all := schemas.All()
		assert.Len(t, all, 3)
	})
}
