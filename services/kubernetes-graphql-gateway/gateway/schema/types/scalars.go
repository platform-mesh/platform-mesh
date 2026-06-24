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
	"encoding/json"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
)

// JSONStringScalar is a GraphQL scalar for JSON-serialized string representation of any object.
var JSONStringScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "JSONString",
	Description: "A JSON-serialized string representation of any object.",
	Serialize: func(value any) any {
		// Convert the value to JSON string
		jsonBytes, err := json.Marshal(value)
		if err != nil {
			// Fallback to empty JSON object if marshaling fails
			return "{}"
		}
		return string(jsonBytes)
	},
	ParseValue: func(value any) any {
		if str, ok := value.(string); ok {
			var result any
			err := json.Unmarshal([]byte(str), &result)
			if err != nil {
				return nil // Invalid JSON
			}
			return result
		}
		return nil
	},
	ParseLiteral: func(valueAST ast.Value) any {
		if value, ok := valueAST.(*ast.StringValue); ok {
			var result any
			err := json.Unmarshal([]byte(value.Value), &result)
			if err != nil {
				return nil // Invalid JSON
			}
			return result
		}
		return nil
	},
})

// StringMapScalar is a GraphQL scalar for map[string]string input types.
var StringMapScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "StringMap_Input",
	Description: "Input type for a map from strings to strings.",
	Serialize: func(value any) any {
		return value
	},
	ParseValue: func(value any) any {
		switch val := value.(type) {
		case map[string]any, map[string]string:
			return val
		default:
			// Added this to handle GraphQL variables
			if arr, ok := value.([]any); ok {
				result := make(map[string]string)
				for _, item := range arr {
					if obj, ok := item.(map[string]any); ok {
						if key, keyOk := obj["key"].(string); keyOk {
							val, _ := obj["value"].(string)
							result[key] = val
						}
					}
				}
				return result
			}
			return nil // to tell GraphQL that the value is invalid
		}
	},
	ParseLiteral: func(valueAST ast.Value) any {
		switch value := valueAST.(type) {
		case *ast.ListValue:
			result := make(map[string]string)
			for _, item := range value.Values {
				obj, ok := item.(*ast.ObjectValue)
				if !ok {
					return nil
				}

				var key, val string
				for _, field := range obj.Fields {
					switch field.Name.Value {
					case "key":
						if k, ok := field.Value.GetValue().(string); ok {
							key = k
						}
					case "value":
						if v, ok := field.Value.GetValue().(string); ok {
							val = v
						}
					}
				}
				if key != "" {
					result[key] = val
				}
			}

			return result
		case *ast.ObjectValue:
			result := map[string]string{}
			for _, field := range value.Fields {
				if strValue, ok := field.Value.GetValue().(string); ok {
					result[field.Name.Value] = strValue
				}
			}
			return result
		default:
			return nil // to tell GraphQL that the value is invalid
		}
	},
})
