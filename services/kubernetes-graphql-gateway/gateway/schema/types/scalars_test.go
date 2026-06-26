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

package types_test

import (
	"encoding/json"
	"testing"

	"github.com/graphql-go/graphql/language/ast"

	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/schema/types"
)

func TestStringMapScalar_ParseLiteral(t *testing.T) {
	tests := []struct {
		name     string
		input    ast.Value
		expected map[string]string
	}{
		{
			name: "list with multiple entries",
			input: &ast.ListValue{
				Values: []ast.Value{
					&ast.ObjectValue{
						Fields: []*ast.ObjectField{
							{Name: &ast.Name{Value: "key"}, Value: &ast.StringValue{Value: "foo"}},
							{Name: &ast.Name{Value: "value"}, Value: &ast.StringValue{Value: "bar"}},
						},
					},
					&ast.ObjectValue{
						Fields: []*ast.ObjectField{
							{Name: &ast.Name{Value: "key"}, Value: &ast.StringValue{Value: "baz"}},
							{Name: &ast.Name{Value: "value"}, Value: &ast.StringValue{Value: "qux"}},
						},
					},
				},
			},
			expected: map[string]string{"foo": "bar", "baz": "qux"},
		},
		{
			name: "list with value before key",
			input: &ast.ListValue{
				Values: []ast.Value{
					&ast.ObjectValue{
						Fields: []*ast.ObjectField{
							{Name: &ast.Name{Value: "value"}, Value: &ast.StringValue{Value: "bar"}},
							{Name: &ast.Name{Value: "key"}, Value: &ast.StringValue{Value: "foo"}},
						},
					},
				},
			},
			expected: map[string]string{"foo": "bar"},
		},
		{
			name: "object value",
			input: &ast.ObjectValue{
				Fields: []*ast.ObjectField{
					{Name: &ast.Name{Value: "foo"}, Value: &ast.StringValue{Value: "bar"}},
					{Name: &ast.Name{Value: "baz"}, Value: &ast.StringValue{Value: "qux"}},
				},
			},
			expected: map[string]string{"foo": "bar", "baz": "qux"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := types.StringMapScalar.ParseLiteral(tt.input)
			resultMap, ok := result.(map[string]string)
			if !ok {
				t.Fatalf("ParseLiteral() returned %T, want map[string]string", result)
			}

			for key, want := range tt.expected {
				if got := resultMap[key]; got != want {
					t.Errorf("resultMap[%q] = %q, want %q", key, got, want)
				}
			}
		})
	}
}

func TestStringMapScalar_ParseValue(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected map[string]string
	}{
		{
			name: "array with key and value",
			input: []any{
				map[string]any{"key": "foo", "value": "bar"},
				map[string]any{"key": "baz", "value": "qux"},
			},
			expected: map[string]string{"foo": "bar", "baz": "qux"},
		},
		{
			name: "array with missing value",
			input: []any{
				map[string]any{"key": "foo"},
			},
			expected: map[string]string{"foo": ""},
		},
		{
			name: "array with non-string value",
			input: []any{
				map[string]any{"key": "foo", "value": 123},
			},
			expected: map[string]string{"foo": ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := types.StringMapScalar.ParseValue(tt.input)
			resultMap, ok := result.(map[string]string)
			if !ok {
				t.Fatalf("ParseValue() returned %T, want map[string]string", result)
			}

			for key, want := range tt.expected {
				got, exists := resultMap[key]
				if !exists {
					t.Errorf("resultMap[%q] does not exist, want %q", key, want)
				} else if got != want {
					t.Errorf("resultMap[%q] = %q, want %q", key, got, want)
				}
			}
		})
	}
}

func TestJSONStringScalar_ProperSerialization(t *testing.T) {
	testObject := map[string]any{
		"name":      "example-config",
		"namespace": "default",
		"labels": map[string]string{
			"hello": "world",
		},
		"annotations": map[string]string{
			"kcp.io/cluster": "root",
		},
	}

	result := types.JSONStringScalar.Serialize(testObject)

	if result == nil {
		t.Fatal("JSONStringScalar.Serialize returned nil")
	}

	resultStr, ok := result.(string)
	if !ok {
		t.Fatalf("JSONStringScalar.Serialize returned %T, expected string", result)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(resultStr), &parsed); err != nil {
		t.Fatalf("Result is not valid JSON: %s\nResult: %s", err, resultStr)
	}

	if parsed["name"] != "example-config" {
		t.Errorf("Name not preserved: got %v, want %v", parsed["name"], "example-config")
	}

	if parsed["namespace"] != "default" {
		t.Errorf("Namespace not preserved: got %v, want %v", parsed["namespace"], "default")
	}

	if len(resultStr) > 10 && resultStr[:4] == "map[" {
		t.Errorf("Result is in Go map format, not JSON: %s", resultStr)
	}
}
