/*
Copyright 2025.

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

package migration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInterpolate(t *testing.T) {
	t.Parallel()

	vars := map[string]any{
		"from": map[string]any{
			"status": map[string]any{
				"relatedResources": map[string]any{
					"credentials": map[string]any{"name": "pg-secret"},
				},
			},
		},
		"to": map[string]any{
			"spec": map[string]any{"replicas": int64(3)},
		},
	}

	tests := []struct {
		name     string
		value    any
		expected any
		wantErr  string
	}{
		{
			name:     "plain string unchanged",
			value:    "no expressions here",
			expected: "no expressions here",
		},
		{
			name:     "string with braces but no expression",
			value:    "{a: b}",
			expected: "{a: b}",
		},
		{
			name:     "whole-string expression keeps type",
			value:    "${to.spec.replicas}",
			expected: int64(3),
		},
		{
			name:     "embedded expression stringified",
			value:    "from-${from.status.relatedResources.credentials.name}",
			expected: "from-pg-secret",
		},
		{
			name:     "multiple expressions",
			value:    "${from.status.relatedResources.credentials.name}:${to.spec.replicas}",
			expected: "pg-secret:3",
		},
		{
			name:     "expression with string literal containing brace",
			value:    `${"}" + from.status.relatedResources.credentials.name}`,
			expected: "}pg-secret",
		},
		{
			name:     "nested map and slice",
			value:    map[string]any{"env": []any{map[string]any{"name": "URI", "value": "${from.status.relatedResources.credentials.name}"}}},
			expected: map[string]any{"env": []any{map[string]any{"name": "URI", "value": "pg-secret"}}},
		},
		{
			name:     "non-string values untouched",
			value:    map[string]any{"count": int64(2), "flag": true},
			expected: map[string]any{"count": int64(2), "flag": true},
		},
		{
			name:    "unterminated expression",
			value:   "${to.spec.replicas",
			wantErr: "unterminated expression",
		},
		{
			name:    "compile error",
			value:   "${this is not CEL(((}",
			wantErr: "compiling expression",
		},
		{
			name:    "unknown variable",
			value:   "${nope.field}",
			wantErr: "compiling expression",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, err := interpolate(t.Context(), test.value, vars)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.expected, result)
		})
	}
}
