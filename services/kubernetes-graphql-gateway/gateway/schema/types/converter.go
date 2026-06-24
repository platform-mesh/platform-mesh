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

package types

import (
	"github.com/graphql-go/graphql"

	"k8s.io/kube-openapi/pkg/validation/spec"
)

type Converter struct {
	registry *Registry
}

func NewConverter(registry *Registry) *Converter {
	return &Converter{
		registry: registry,
	}
}

func (c *Converter) ConvertFields(resourceScheme *spec.Schema, definitions map[string]*spec.Schema, typePrefix string) (graphql.Fields, graphql.InputObjectConfigFieldMap, error) {
	return c.convertFields(resourceScheme, definitions, typePrefix, []string{})
}

func (c *Converter) convertFields(resourceScheme *spec.Schema, definitions map[string]*spec.Schema, typePrefix string, fieldPath []string) (graphql.Fields, graphql.InputObjectConfigFieldMap, error) {
	fields := graphql.Fields{}
	inputFields := graphql.InputObjectConfigFieldMap{}

	for fieldName, fieldSpec := range resourceScheme.Properties {
		sanitizedFieldName := SanitizeFieldName(fieldName)
		currentFieldPath := append(fieldPath, fieldName)

		fieldType, inputFieldType, err := c.convert(fieldSpec, definitions, typePrefix, currentFieldPath)
		if err != nil {
			return nil, nil, err
		}

		fields[sanitizedFieldName] = &graphql.Field{
			Type:        fieldType,
			Description: fieldSpec.Description,
		}

		inputFields[sanitizedFieldName] = &graphql.InputObjectFieldConfig{
			Type:        inputFieldType,
			Description: fieldSpec.Description,
		}
	}

	return fields, inputFields, nil
}

func (c *Converter) convert(schema spec.Schema, definitions map[string]*spec.Schema, typePrefix string, fieldPath []string) (graphql.Output, graphql.Input, error) {
	if len(schema.Type) == 0 {
		return c.handleRefType(schema, definitions, fieldPath)
	}

	switch schema.Type[0] {
	case "string":
		return graphql.String, graphql.String, nil
	case "integer":
		return graphql.Int, graphql.Int, nil
	case "number":
		return graphql.Float, graphql.Float, nil
	case "boolean":
		return graphql.Boolean, graphql.Boolean, nil
	case "array":
		return c.handleArrayType(schema, definitions, typePrefix, fieldPath)
	case "object":
		return c.handleObjectType(schema, definitions, typePrefix, fieldPath)
	default:
		return JSONStringScalar, JSONStringScalar, nil
	}
}

func (c *Converter) handleRefType(schema spec.Schema, definitions map[string]*spec.Schema, fieldPath []string) (graphql.Output, graphql.Input, error) {
	if len(schema.AllOf) == 0 {
		return JSONStringScalar, JSONStringScalar, nil
	}

	refKey := schema.AllOf[0].Ref.String()

	if c.registry.IsProcessing(refKey) {
		if output, input := c.registry.Get(refKey); output != nil {
			return output, input, nil
		}
		return JSONStringScalar, JSONStringScalar, nil
	}

	refDef, ok := definitions[refKey]
	if !ok {
		return JSONStringScalar, JSONStringScalar, nil
	}

	c.registry.MarkProcessing(refKey)
	defer c.registry.UnmarkProcessing(refKey)

	fieldType, inputFieldType, err := c.convert(*refDef, definitions, refKey, fieldPath)
	if err != nil {
		return nil, nil, err
	}

	objType, objOk := fieldType.(*graphql.Object)
	inputObjType, inputOk := inputFieldType.(*graphql.InputObject)

	if objOk && inputOk {
		c.registry.Register(refKey, objType, inputObjType)
	}

	return fieldType, inputFieldType, nil
}

func (c *Converter) handleArrayType(schema spec.Schema, definitions map[string]*spec.Schema, typePrefix string, fieldPath []string) (graphql.Output, graphql.Input, error) {
	if schema.Items == nil || schema.Items.Schema == nil {
		return graphql.NewList(JSONStringScalar), graphql.NewList(JSONStringScalar), nil
	}

	itemType, inputItemType, err := c.convert(*schema.Items.Schema, definitions, typePrefix, fieldPath)
	if err != nil {
		return nil, nil, err
	}
	return graphql.NewList(itemType), graphql.NewList(inputItemType), nil
}

func (c *Converter) handleObjectType(fieldSpec spec.Schema, definitions map[string]*spec.Schema, typePrefix string, fieldPath []string) (graphql.Output, graphql.Input, error) {
	// NOTE: When a schema has both Properties and x-kubernetes-preserve-unknown-fields: true,
	// only the declared properties are exposed in the GraphQL type. Any additional undeclared
	// fields are silently dropped from responses. Supporting the full preserve-unknown-fields
	// semantics would require a catch-all JSON field or falling back to JSONStringScalar.
	if len(fieldSpec.Properties) > 0 {
		return c.handleNestedObject(fieldSpec, definitions, typePrefix, fieldPath)
	}

	if fieldSpec.AdditionalProperties != nil && fieldSpec.AdditionalProperties.Schema != nil {
		if len(fieldSpec.AdditionalProperties.Schema.Type) == 1 && fieldSpec.AdditionalProperties.Schema.Type[0] == "string" {
			return StringMapScalar, StringMapScalar, nil
		}
	}

	return JSONStringScalar, JSONStringScalar, nil
}

func (c *Converter) handleNestedObject(fieldSpec spec.Schema, definitions map[string]*spec.Schema, typePrefix string, fieldPath []string) (graphql.Output, graphql.Input, error) {
	typeName := GenerateTypeName(typePrefix, fieldPath)

	if output, input := c.registry.Get(typeName); output != nil {
		return output, input, nil
	}

	if c.registry.IsProcessing(typeName) {
		// Circular reference detected - return JSONStringScalar as a fallback to break
		// recursion. This loses the actual type structure. A proper fix would use
		// graphql.FieldsThunk for lazy type resolution, allowing self-referential types.
		return JSONStringScalar, JSONStringScalar, nil
	}

	c.registry.MarkProcessing(typeName)

	nestedFields, nestedInputFields, err := c.convertFields(&fieldSpec, definitions, typeName, []string{})
	if err != nil {
		c.registry.UnmarkProcessing(typeName)
		return nil, nil, err
	}

	newType := graphql.NewObject(graphql.ObjectConfig{
		Name:        SanitizeFieldName(typeName),
		Description: fieldSpec.Description,
		Fields:      nestedFields,
	})

	newInputType := graphql.NewInputObject(graphql.InputObjectConfig{
		Name:        SanitizeFieldName(typeName) + "_Input",
		Description: fieldSpec.Description,
		Fields:      nestedInputFields,
	})

	c.registry.Register(typeName, newType, newInputType)

	return newType, newInputType, nil
}
