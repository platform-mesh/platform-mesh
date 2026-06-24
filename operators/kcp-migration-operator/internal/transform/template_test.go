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

package transform

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestEvaluateTemplate(t *testing.T) {
	tests := []struct {
		name        string
		template    string
		data        TemplateData
		expected    string
		expectError bool
	}{
		{
			name:     "simple static string",
			template: "root:orgs:sap",
			data:     TemplateData{Source: map[string]any{}},
			expected: "root:orgs:sap",
		},
		{
			name:     "template with namespace",
			template: "root:orgs:{{ .Source.metadata.namespace }}",
			data: TemplateData{
				Source: map[string]any{
					"metadata": map[string]any{
						"namespace": "test-ns",
					},
				},
			},
			expected: "root:orgs:test-ns",
		},
		{
			name:     "template with name",
			template: "root:{{ .Source.metadata.name }}",
			data: TemplateData{
				Source: map[string]any{
					"metadata": map[string]any{
						"name": "my-resource",
					},
				},
			},
			expected: "root:my-resource",
		},
		{
			name:     "template with label",
			template: "root:orgs:{{ index .Source.metadata.labels \"org\" }}",
			data: TemplateData{
				Source: map[string]any{
					"metadata": map[string]any{
						"labels": map[string]any{
							"org": "sap",
						},
					},
				},
			},
			expected: "root:orgs:sap",
		},
		{
			name:     "template with nested spec field",
			template: "root:{{ .Source.spec.type }}",
			data: TemplateData{
				Source: map[string]any{
					"spec": map[string]any{
						"type": "account",
					},
				},
			},
			expected: "root:account",
		},
		{
			name:        "empty template",
			template:    "",
			data:        TemplateData{},
			expectError: true,
		},
		{
			name:     "invalid template syntax",
			template: "root:{{ .Source.invalid",
			data: TemplateData{
				Source: map[string]any{},
			},
			expectError: true,
		},
		{
			name:     "missing key in template",
			template: "root:{{ .Source.metadata.nonexistent }}",
			data: TemplateData{
				Source: map[string]any{
					"metadata": map[string]any{},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := EvaluateTemplate(tt.template, tt.data)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewTemplateData(t *testing.T) {
	source := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "test-cm",
				"namespace": "default",
			},
		},
	}

	data := NewTemplateData(source)

	assert.Equal(t, source.Object, data.Source)
	assert.Equal(t, "ConfigMap", data.Source["kind"])
}

func TestEvaluateWorkspaceExpression(t *testing.T) {
	tests := []struct {
		name        string
		expression  string
		source      *unstructured.Unstructured
		expected    string
		expectError bool
	}{
		{
			name:       "static workspace path",
			expression: "root:orgs:sap",
			source: &unstructured.Unstructured{
				Object: map[string]any{},
			},
			expected: "root:orgs:sap",
		},
		{
			name:       "dynamic workspace from namespace",
			expression: "root:orgs:{{ .Source.metadata.namespace }}",
			source: &unstructured.Unstructured{
				Object: map[string]any{
					"metadata": map[string]any{
						"namespace": "sap",
					},
				},
			},
			expected: "root:orgs:sap",
		},
		{
			name:       "dynamic workspace from label",
			expression: "root:orgs:{{ index .Source.metadata.labels \"platform-mesh.io/org\" }}",
			source: &unstructured.Unstructured{
				Object: map[string]any{
					"metadata": map[string]any{
						"labels": map[string]any{
							"platform-mesh.io/org": "my-org",
						},
					},
				},
			},
			expected: "root:orgs:my-org",
		},
		{
			name:       "complex workspace path with multiple fields",
			expression: "root:{{ .Source.metadata.namespace }}:{{ .Source.metadata.name }}",
			source: &unstructured.Unstructured{
				Object: map[string]any{
					"metadata": map[string]any{
						"namespace": "orgs",
						"name":      "sap",
					},
				},
			},
			expected: "root:orgs:sap",
		},
		{
			name:       "missing namespace field",
			expression: "root:{{ .Source.metadata.namespace }}",
			source: &unstructured.Unstructured{
				Object: map[string]any{
					"metadata": map[string]any{},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := EvaluateWorkspaceExpression(tt.expression, tt.source)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEvaluateTemplateWithAccountResource(t *testing.T) {
	// Test with a realistic Account resource structure
	account := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "core.platform-mesh.io/v1alpha1",
			"kind":       "Account",
			"metadata": map[string]any{
				"name":      "test-account",
				"namespace": "sap",
				"labels": map[string]any{
					"org":                    "sap",
					"platform-mesh.io/owner": "admin",
				},
			},
			"spec": map[string]any{
				"type":        "account",
				"displayName": "Test Account",
			},
		},
	}

	tests := []struct {
		name       string
		expression string
		expected   string
	}{
		{
			name:       "workspace from namespace",
			expression: "root:orgs:{{ .Source.metadata.namespace }}",
			expected:   "root:orgs:sap",
		},
		{
			name:       "workspace from org label",
			expression: "root:orgs:{{ index .Source.metadata.labels \"org\" }}",
			expected:   "root:orgs:sap",
		},
		{
			name:       "workspace from name",
			expression: "root:accounts:{{ .Source.metadata.name }}",
			expected:   "root:accounts:test-account",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := EvaluateWorkspaceExpression(tt.expression, account)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
