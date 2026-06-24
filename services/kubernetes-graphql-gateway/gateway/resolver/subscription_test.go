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

package resolver

import (
	"testing"

	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestDetermineFieldChanged(t *testing.T) {
	tests := []struct {
		name           string
		oldObj         *unstructured.Unstructured
		newObj         *unstructured.Unstructured
		fields         []string
		isFieldChanged bool
		expectError    bool
	}{
		{
			name:           "oldObj_is_nil",
			oldObj:         nil,
			newObj:         &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"ready": true}}},
			fields:         []string{"status.ready"},
			isFieldChanged: true,
			expectError:    false,
		},
		{
			name:           "both_objects_are_empty",
			oldObj:         &unstructured.Unstructured{Object: map[string]any{}},
			newObj:         &unstructured.Unstructured{Object: map[string]any{}},
			fields:         []string{"status.ready"},
			isFieldChanged: false,
			expectError:    false,
		},
		{
			name:           "field_missing_in_both",
			oldObj:         &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"ready": true}}},
			newObj:         &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"ready": true}}},
			fields:         []string{"status.missing"},
			isFieldChanged: false,
			expectError:    false,
		},
		{
			name:           "field_present_in_oldObj_but_missing_in_newObj",
			oldObj:         &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"ready": true}}},
			newObj:         &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{}}},
			fields:         []string{"status.ready"},
			isFieldChanged: true,
			expectError:    false,
		},
		{
			name:           "field_present_in_newObj_but_missing_in_oldObj",
			oldObj:         &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{}}},
			newObj:         &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"ready": true}}},
			fields:         []string{"status.ready"},
			isFieldChanged: true,
			expectError:    false,
		},
		{
			name:           "field_value_changed",
			oldObj:         &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"ready": false}}},
			newObj:         &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"ready": true}}},
			fields:         []string{"status.ready"},
			isFieldChanged: true,
			expectError:    false,
		},
		{
			name: "field_value_changed",
			oldObj: &unstructured.Unstructured{Object: map[string]any{
				"status": map[string]any{"ready": true, "healthy": true},
			}},
			newObj: &unstructured.Unstructured{Object: map[string]any{
				"status": map[string]any{"ready": true, "healthy": false},
			}},
			fields:         []string{"status.ready", "status.healthy"},
			isFieldChanged: true,
			expectError:    false,
		},
		{
			name:           "field_value_unchanged",
			oldObj:         &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"ready": true}}},
			newObj:         &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"ready": true}}},
			fields:         []string{"status.ready"},
			isFieldChanged: false,
			expectError:    false,
		},
		{
			name: "nested_field_changed",
			oldObj: &unstructured.Unstructured{
				Object: map[string]any{
					"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
				},
			},
			newObj: &unstructured.Unstructured{
				Object: map[string]any{
					"conditions": []any{map[string]any{"type": "Ready", "status": "False"}},
				},
			},
			fields:         []string{"conditions.0.status", "conditions.0.type"},
			isFieldChanged: true,
			expectError:    false,
		},
		{
			name:           "nested_field_unchanged",
			oldObj:         &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}}}},
			newObj:         &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}}}},
			fields:         []string{"status.conditions.0.status"},
			isFieldChanged: false,
			expectError:    false,
		},
		{
			name:           "invalid_field_path",
			oldObj:         &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"ready": true}}},
			newObj:         &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"ready": true}}},
			fields:         []string{"invalid.path"},
			isFieldChanged: false,
			expectError:    false,
		},
		{
			name:           "unexpected_type_in_field_path",
			oldObj:         &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"ready": true}}},
			newObj:         &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"ready": true}}},
			fields:         []string{"status.ready.invalid"},
			isFieldChanged: false,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := determineFieldChanged(tt.oldObj, tt.newObj, tt.fields)
			if tt.expectError {
				require.NotNil(t, err)
			}
			require.Equal(t, tt.isFieldChanged, result)
		})
	}
}
