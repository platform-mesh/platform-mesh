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

	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/schema/types"

	"k8s.io/kube-openapi/pkg/validation/spec"
)

// TestConvert_TypelessFieldUsesJSONScalar verifies that a schema field with no
// explicit type (as produced by apiextensionsv1.JSON / runtime.RawExtension with
// x-kubernetes-preserve-unknown-fields) is mapped to JSONStringScalar instead of
// graphql.String, so values are serialized via json.Marshal rather than fmt.Sprintf.
// Regression test for https://go.platform-mesh.io/kubernetes-graphql-gateway/issues/148
func TestConvert_TypelessFieldUsesJSONScalar(t *testing.T) {
	converter := types.NewConverter(types.NewRegistry())

	schema := &spec.Schema{
		SchemaProps: spec.SchemaProps{
			Properties: map[string]spec.Schema{
				"config": {},
			},
		},
	}

	fields, inputFields, err := converter.ConvertFields(schema, map[string]*spec.Schema{}, "TestType")
	if err != nil {
		t.Fatalf("ConvertFields() error = %v", err)
	}

	if got := fields["config"].Type.Name(); got != "JSONString" {
		t.Errorf("output type = %q, want %q", got, "JSONString")
	}
	if got := inputFields["config"].Type.Name(); got != "JSONString" {
		t.Errorf("input type = %q, want %q", got, "JSONString")
	}
}

// TestConvert_NestedTypeNameCollision verifies that a CRD whose nested field
// path would produce a name matching a built-in Kind (e.g., Component + status
// → ComponentStatus) does not collide when the typePrefix is group+version
// qualified. Regression test for https://go.platform-mesh.io/kubernetes-graphql-gateway/issues/222
func TestConvert_NestedTypeNameCollision(t *testing.T) {
	registry := types.NewRegistry()
	converter := types.NewConverter(registry)

	builtinType := graphql.NewObject(graphql.ObjectConfig{
		Name:   "V1ComponentStatus",
		Fields: graphql.Fields{"name": &graphql.Field{Type: graphql.String}},
	})
	builtinInput := graphql.NewInputObject(graphql.InputObjectConfig{
		Name:   "V1ComponentStatus_Input",
		Fields: graphql.InputObjectConfigFieldMap{"name": &graphql.InputObjectFieldConfig{Type: graphql.String}},
	})
	registry.Register("V1ComponentStatus", builtinType, builtinInput)

	schema := &spec.Schema{
		SchemaProps: spec.SchemaProps{
			Properties: map[string]spec.Schema{
				"status": {
					SchemaProps: spec.SchemaProps{
						Type: []string{"object"},
						Properties: map[string]spec.Schema{
							"ready": {SchemaProps: spec.SchemaProps{Type: []string{"boolean"}}},
						},
					},
				},
			},
		},
	}

	fields, inputFields, err := converter.ConvertFields(schema, map[string]*spec.Schema{}, "CustomIoV1Component")
	if err != nil {
		t.Fatalf("ConvertFields() error = %v", err)
	}

	statusField := fields["status"]
	if statusField == nil {
		t.Fatal("expected 'status' field to exist")
	}
	if got := statusField.Type.Name(); got != "CustomIoV1ComponentStatus" {
		t.Errorf("nested output type = %q, want %q", got, "CustomIoV1ComponentStatus")
	}

	statusInput := inputFields["status"]
	if statusInput == nil {
		t.Fatal("expected 'status' input field to exist")
	}
	if got := statusInput.Type.Name(); got != "CustomIoV1ComponentStatus_Input" {
		t.Errorf("nested input type = %q, want %q", got, "CustomIoV1ComponentStatus_Input")
	}
}

// TestConvert_FieldNamedInputNoCollision verifies that a nested object with a
// field literally named "input" does not collide with the parent's _Input type.
// The field "input" produces output type "...SpecTemplatesInput" while the parent
// input type is "...SpecTemplates_Input" — these are distinct.
// Regression test for https://go.platform-mesh.io/kubernetes-graphql-gateway/issues/222
func TestConvert_FieldNamedInputNoCollision(t *testing.T) {
	registry := types.NewRegistry()
	converter := types.NewConverter(registry)

	// Mimics the automaticd.sap/v2 Gomplate CRD structure:
	// spec.templates is an array of objects, each containing a field named "input"
	schema := &spec.Schema{
		SchemaProps: spec.SchemaProps{
			Properties: map[string]spec.Schema{
				"templates": {
					SchemaProps: spec.SchemaProps{
						Type: []string{"array"},
						Items: &spec.SchemaOrArray{
							Schema: &spec.Schema{
								SchemaProps: spec.SchemaProps{
									Type: []string{"object"},
									Properties: map[string]spec.Schema{
										"name": {SchemaProps: spec.SchemaProps{Type: []string{"string"}}},
										"input": {
											SchemaProps: spec.SchemaProps{
												Type: []string{"object"},
												Properties: map[string]spec.Schema{
													"url":  {SchemaProps: spec.SchemaProps{Type: []string{"string"}}},
													"path": {SchemaProps: spec.SchemaProps{Type: []string{"string"}}},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	typePrefix := "AutomaticdSapV2GomplateSpec"
	fields, inputFields, err := converter.ConvertFields(schema, map[string]*spec.Schema{}, typePrefix)
	if err != nil {
		t.Fatalf("ConvertFields() error = %v", err)
	}

	// The templates field should exist
	templatesField := fields["templates"]
	if templatesField == nil {
		t.Fatal("expected 'templates' field to exist")
	}

	// Verify the array item output type is named correctly
	listType, ok := templatesField.Type.(*graphql.List)
	if !ok {
		t.Fatal("expected templates to be a list type")
	}
	itemType, ok := listType.OfType.(*graphql.Object)
	if !ok {
		t.Fatal("expected templates list item to be an object type")
	}
	if got := itemType.Name(); got != "AutomaticdSapV2GomplateSpecTemplates" {
		t.Errorf("templates item output type = %q, want %q", got, "AutomaticdSapV2GomplateSpecTemplates")
	}

	// The "input" field inside templates produces output type "...SpecTemplatesInput"
	inputFieldInTemplates := itemType.Fields()["input"]
	if inputFieldInTemplates == nil {
		t.Fatal("expected 'input' field inside templates item")
	}
	if got := inputFieldInTemplates.Type.Name(); got != "AutomaticdSapV2GomplateSpecTemplatesInput" {
		t.Errorf("nested 'input' field output type = %q, want %q", got, "AutomaticdSapV2GomplateSpecTemplatesInput")
	}

	// The input type for templates array item uses _Input suffix
	templatesInput := inputFields["templates"]
	if templatesInput == nil {
		t.Fatal("expected 'templates' input field to exist")
	}
	inputListType, ok := templatesInput.Type.(*graphql.List)
	if !ok {
		t.Fatal("expected templates input to be a list type")
	}
	inputItemType, ok := inputListType.OfType.(*graphql.InputObject)
	if !ok {
		t.Fatal("expected templates input list item to be an input object type")
	}
	// This is the critical assertion: _Input suffix does NOT collide with the "input" field's type name
	if got := inputItemType.Name(); got != "AutomaticdSapV2GomplateSpecTemplates_Input" {
		t.Errorf("templates item input type = %q, want %q", got, "AutomaticdSapV2GomplateSpecTemplates_Input")
	}

	// Verify both types can coexist (no duplicate name)
	if itemType.Name() == inputItemType.Name() {
		t.Errorf("output type for 'input' field (%q) must not equal parent input type (%q)", inputFieldInTemplates.Type.Name(), inputItemType.Name())
	}
}
