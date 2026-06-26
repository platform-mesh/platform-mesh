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
	"testing"

	"github.com/graphql-go/graphql"
	"github.com/stretchr/testify/assert"

	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/schema/types"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestSanitizeGroupName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty string", input: "", want: ""},
		{name: "simple group", input: "apps", want: "apps"},
		{name: "group with dots", input: "networking.k8s.io", want: "networking_k8s_io"},
		{name: "group with hyphens", input: "my-group", want: "my_group"},
		{name: "group starting with number", input: "1group", want: "_1group"},
		{name: "group with special chars", input: "my@group!", want: "my_group_"},
		{name: "already valid", input: "_valid_group", want: "_valid_group"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, types.SanitizeGroupName(tt.input))
		})
	}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	registry := types.NewRegistry()

	// Create test types
	outputType := graphql.NewObject(graphql.ObjectConfig{
		Name:   "TestType",
		Fields: graphql.Fields{"name": &graphql.Field{Type: graphql.String}},
	})
	inputType := graphql.NewInputObject(graphql.InputObjectConfig{
		Name:   "TestType_Input",
		Fields: graphql.InputObjectConfigFieldMap{"name": &graphql.InputObjectFieldConfig{Type: graphql.String}},
	})

	// Register types
	registry.Register("test-key", outputType, inputType)

	// Get types back
	gotOutput, gotInput := registry.Get("test-key")
	assert.Equal(t, outputType, gotOutput)
	assert.Equal(t, inputType, gotInput)
}

func TestRegistry_GetNonExistent(t *testing.T) {
	registry := types.NewRegistry()

	output, input := registry.Get("non-existent")
	assert.Nil(t, output)
	assert.Nil(t, input)
}

func TestRegistry_ProcessingState(t *testing.T) {
	registry := types.NewRegistry()
	key := "test-key"

	// Initially not processing
	assert.False(t, registry.IsProcessing(key))

	// Mark as processing
	registry.MarkProcessing(key)
	assert.True(t, registry.IsProcessing(key))

	// Get should return nil while processing
	output, input := registry.Get(key)
	assert.Nil(t, output)
	assert.Nil(t, input)

	// Unmark processing
	registry.UnmarkProcessing(key)
	assert.False(t, registry.IsProcessing(key))
}

func TestRegistry_GetUniqueTypeName(t *testing.T) {
	tests := []struct {
		name     string
		gvk      schema.GroupVersionKind
		expected string
	}{
		{
			name:     "core resource always gets version prefix",
			gvk:      schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
			expected: "V1Pod",
		},
		{
			name:     "apps group resource gets group+version prefix",
			gvk:      schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
			expected: "AppsV1Deployment",
		},
		{
			name:     "extensions group resource gets group+version prefix",
			gvk:      schema.GroupVersionKind{Group: "extensions", Version: "v1beta1", Kind: "Deployment"},
			expected: "ExtensionsV1beta1Deployment",
		},
		{
			name:     "dotted group name is sanitized",
			gvk:      schema.GroupVersionKind{Group: "networking.k8s.io", Version: "v1", Kind: "Ingress"},
			expected: "NetworkingK8sIoV1Ingress",
		},
		{
			name:     "CRD with custom group",
			gvk:      schema.GroupVersionKind{Group: "custom.io", Version: "v1", Kind: "Component"},
			expected: "CustomIoV1Component",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := types.NewRegistry()
			got := registry.GetUniqueTypeName(&tt.gvk)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestRegistry_GetUniqueTypeName_EmptyGroup(t *testing.T) {
	registry := types.NewRegistry()

	gvk1 := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	assert.Equal(t, "V1Pod", registry.GetUniqueTypeName(&gvk1))

	// Same GVK returns the same name
	assert.Equal(t, "V1Pod", registry.GetUniqueTypeName(&gvk1))
}

func TestRegistry_GetUniqueTypeName_GroupWithDots(t *testing.T) {
	registry := types.NewRegistry()

	gvk1 := schema.GroupVersionKind{Group: "networking.k8s.io", Version: "v1", Kind: "Ingress"}
	assert.Equal(t, "NetworkingK8sIoV1Ingress", registry.GetUniqueTypeName(&gvk1))

	gvk2 := schema.GroupVersionKind{Group: "extensions", Version: "v1beta1", Kind: "Ingress"}
	assert.Equal(t, "ExtensionsV1beta1Ingress", registry.GetUniqueTypeName(&gvk2))
}

func TestRegistry_IsProcessing_AfterMark(t *testing.T) {
	registry := types.NewRegistry()

	assert.False(t, registry.IsProcessing("test-key"))

	registry.MarkProcessing("test-key")
	assert.True(t, registry.IsProcessing("test-key"))
}
