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

	pmgateway "go.platform-mesh.io/apis/gateway"
	"go.platform-mesh.io/kubernetes-graphql-gateway/apischema"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

func TestExtractGVK(t *testing.T) {
	tests := []struct {
		name    string
		schema  *spec.Schema
		wantGVK *schema.GroupVersionKind
		wantErr bool
	}{
		{
			name: "valid GVK",
			schema: &spec.Schema{
				VendorExtensible: spec.VendorExtensible{
					Extensions: map[string]any{
						pmgateway.GVKExtensionKey: []any{
							map[string]any{
								"group":   "apps",
								"version": "v1",
								"kind":    "Deployment",
							},
						},
					},
				},
			},
			wantGVK: &schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			wantErr: false,
		},
		{
			name: "core group (empty)",
			schema: &spec.Schema{
				VendorExtensible: spec.VendorExtensible{
					Extensions: map[string]any{
						pmgateway.GVKExtensionKey: []any{
							map[string]any{
								"group":   "",
								"version": "v1",
								"kind":    "Pod",
							},
						},
					},
				},
			},
			wantGVK: &schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			wantErr: false,
		},
		{
			name:    "no extensions",
			schema:  &spec.Schema{},
			wantGVK: nil,
			wantErr: false,
		},
		{
			name: "no GVK extension",
			schema: &spec.Schema{
				VendorExtensible: spec.VendorExtensible{
					Extensions: map[string]any{
						"x-other": "value",
					},
				},
			},
			wantGVK: nil,
			wantErr: false,
		},
		{
			name: "multiple GVKs (skipped)",
			schema: &spec.Schema{
				VendorExtensible: spec.VendorExtensible{
					Extensions: map[string]any{
						pmgateway.GVKExtensionKey: []any{
							map[string]any{"group": "a", "version": "v1", "kind": "A"},
							map[string]any{"group": "b", "version": "v1", "kind": "B"},
						},
					},
				},
			},
			wantGVK: nil, // Schemas with multiple GVKs are skipped
			wantErr: false,
		},
		{
			name: "empty GVK list",
			schema: &spec.Schema{
				VendorExtensible: spec.VendorExtensible{
					Extensions: map[string]any{
						pmgateway.GVKExtensionKey: []any{},
					},
				},
			},
			wantGVK: nil,
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gvk, err := apischema.ExtractGVK(tc.schema)

			if tc.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tc.wantGVK, gvk)
		})
	}
}
