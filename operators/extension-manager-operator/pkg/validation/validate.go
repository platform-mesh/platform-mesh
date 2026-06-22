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

package validation

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"

	"github.com/pkg/errors"
)

var (
	ErrorEmptyInput       = errors.New("empty input provided")
	ErrorNoValidator      = errors.New("no validator found for content type")
	ErrorMarshalJSON      = errors.New("error marshaling input to JSON")
	ErrorValidatingJSON   = errors.New("error validating JSON data")
	ErrorDocumentInvalid  = errors.New("The document is not valid:\n%s")
	ErrorRequiredField    = errors.New("field '%s' is required")
	ErrorInvalidFieldType = errors.New("field '%s' is invalid, got '%s', expected '%s'")
)

type contentConfiguration struct {
	schema []byte
}

//go:embed schema/schema_autogen.json
var schemaJSON []byte

func NewContentConfiguration() ExtensionConfiguration {
	return &contentConfiguration{
		schema: schemaJSON,
	}
}

func (cC *contentConfiguration) WithSchema(schema []byte) error {
	if len(schema) == 0 {
		return ErrorEmptyInput
	}
	cC.schema = schema
	return nil
}

// Validate does the validation against strict schema
// and includes any optional field that is not defined there:
// Steps:
// 1. Gets raw JSON or YAML data from the input
// 2. In case of YAML it converts data to raw JSON
// 3. Passes the raw JSON to the schema validator
// 4. In case of success returns the original raw JSON
func (cC *contentConfiguration) Validate(input []byte, contentType string) (string, *multierror.Error) {
	if len(input) == 0 {
		return "", multierror.Append(nil, ErrorEmptyInput)
	}

	var rawJSON []byte
	var err error
	switch contentTypeLower := strings.ToLower(contentType); contentTypeLower {
	case "json":
		rawJSON = input
	case "yaml":
		rawJSON, err = convertYAMLToJSON(input)
		if err != nil {
			return "", &multierror.Error{Errors: []error{err}}
		}
	default:
		return "", &multierror.Error{Errors: []error{ErrorNoValidator}}
	}
	merr := validateSchemaBytes(cC.schema, rawJSON)
	if merr.Len() == 0 {
		return string(rawJSON), merr
	} else {
		return "", merr
	}
}

func validateSchemaBytes(schema []byte, input []byte) *multierror.Error {
	schemaLoader := gojsonschema.NewBytesLoader(schema)
	documentLoader := gojsonschema.NewBytesLoader(input)
	merrs := &multierror.Error{}

	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		merrs = multierror.Append(merrs, err)
		return multierror.Append(merrs, ErrorValidatingJSON)
	}

	if !result.Valid() {
		for _, desc := range result.Errors() {
			switch desc.Type() {
			case "required":
				merrs = multierror.Append(merrs, errors.New(desc.Description()))

			case "invalid_type":
				errStr := fmt.Sprintf(
					ErrorInvalidFieldType.Error(),
					desc.Field(),
					desc.Details()["type"],
					desc.Details()["expected"])
				merrs = multierror.Append(merrs, errors.New(errStr))
			default:
				merrs = multierror.Append(merrs, errors.New(desc.String()))
			}
		}
		return multierror.Append(merrs, errors.New(ErrorDocumentInvalid.Error()))
	}

	return merrs
}

// ConvertYAMLToJSON converts a YAML byte array to a JSON byte array
func convertYAMLToJSON(yamlData []byte) ([]byte, error) {
	// Unmarshal YAML into a map
	var data map[string]interface{}
	err := yaml.Unmarshal(yamlData, &data)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling YAML: %w", err)
	}

	// Marshal the map into JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("error marshalling JSON: %w", err)
	}

	return jsonData, nil
}
