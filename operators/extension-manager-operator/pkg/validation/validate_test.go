package validation

import (
	"encoding/json"
	"log"
	"testing"

	"github.com/hashicorp/go-multierror"
	"github.com/stretchr/testify/assert"

	commonTesting "go.platform-mesh.io/extension-manager-operator/pkg/util/testing"
	"go.platform-mesh.io/extension-manager-operator/pkg/validation/validation_test"
)

func TestValidate(t *testing.T) {
	testCases := []struct {
		name        string
		input       string
		contentType string
		schema      []byte
		expected    string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid_JSON",
			input:       validation_test.GetValidJSON(),
			contentType: "Json",
			expected:    validation_test.GetValidJSON(),
			expectError: false,
		},
		{
			name:        "invalid_JSON_empty_input_ERROR",
			input:       `{"name": "overview",`,
			contentType: "JSON",
			expected:    "",
			expectError: true,
			errorMsg:    "unexpected EOF",
		},
		{
			name:        "valid_YAML",
			input:       validation_test.GetValidYAML(),
			contentType: "yaml",
			expected:    validation_test.GetValidJSON(),
			expectError: false,
		},
		{
			name:        "unmarshalling_YAML_ERROR",
			input:       "!2",
			contentType: "Yaml",
			expected:    "",
			expectError: true,
			errorMsg:    "error unmarshalling YAML: yaml:",
		},
		{
			name:        "the_document_is_not_valid_ERROR",
			input:       `2!`,
			contentType: "YAML",
			expected:    "",
			expectError: true,
			errorMsg: "error unmarshalling YAML: yaml: unmarshal errors:\n  line 1: " +
				"cannot unmarshal !!str `2!` into map[string]interface {}",
		},
		{
			name:        "unsupported_content_type_ERROR",
			input:       validation_test.GetValidJSON(),
			contentType: "xml",
			expected:    "",
			expectError: true,
			errorMsg:    "no validator found for content type",
		},
		{
			name:        "empty_input_ERROR",
			input:       "",
			contentType: "json",
			expected:    "",
			expectError: true,
			errorMsg:    "empty input provided",
		},
		{
			name:        "validating_with_incorrect_schema_ERROR",
			schema:      []byte("123"),
			contentType: "json",
			input:       validation_test.GetValidJSON(),
			expected:    "",
			expectError: true,
			errorMsg:    "error validating JSON data",
		},
		{
			name:        "unmarshal_string_into_Go_struct_ERROR",
			schema:      getJSONSchemaFixture(),
			input:       validation_test.GetInvalidTypeYAML(),
			contentType: "yaml",
			expected:    "",
			expectError: true,
			errorMsg:    "The document is not valid:",
		},
		{
			name:        "valid_JSON_empty_locale",
			input:       validation_test.GetValidJSONWithEmptyLocale(),
			contentType: "json",
			expected:    validation_test.GetValidJSONWithEmptyLocale(),
			expectError: false,
		},
		{
			name:        "test_luigiConfigFragment",
			input:       validation_test.GetluigiConfigFragment(),
			contentType: "json",
			expected:    validation_test.GetluigiConfigFragment(),
			expectError: false,
		},
		{
			name:        "test_luigiConfigFragment",
			input:       validation_test.GetValidYaml_targetAppConfig_viewGroup(),
			contentType: "yaml",
			expected:    validation_test.GetValidYaml_targetAppConfig_viewGroup(),
			expectError: false,
			schema:      nil,
		},
		{
			name:        "test_node_category_string",
			input:       validation_test.GetValidYAML_node_category_string(),
			contentType: "yaml",
			expected:    validation_test.GetValidJSON_node_category_string(),
			expectError: false,
			schema:      nil,
		},
		{
			name:        "test_node_category_object",
			input:       validation_test.GetValidYAML_node_category_object(),
			contentType: "yaml",
			expected:    validation_test.GetValidJSON_node_category_object(),
			expectError: false,
			schema:      nil,
		},
		{
			name:        "test_node_category_invalidobject",
			input:       validation_test.GetInalidYAML_node_category_object(),
			contentType: "yaml",
			expected:    "",
			expectError: true,
			schema:      nil,
		},
		{
			name:        "test_luigiConfigFragment",
			input:       validation_test.GetValidYaml_targetAppConfig_viewGroup2(),
			contentType: "yaml",
			expected:    validation_test.GetValidYaml_targetAppConfig_viewGroup2(),
			expectError: false,
			schema:      nil,
		},
		{
			name:        "extension-manager-ui1",
			input:       validation_test.GetValidJSON_extension_manager_ui1(),
			contentType: "json",
			expected:    validation_test.GetValidJSON_extension_manager_ui1(),
			expectError: false,
			schema:      nil,
		},
		{
			name:        "extension-manager-ui2",
			input:       validation_test.GetValidJSON_extension_manager_ui2(),
			contentType: "json",
			expected:    validation_test.GetValidJSON_extension_manager_ui2(),
			expectError: false,
			schema:      nil,
		},
		{
			name:        "github-ui1",
			input:       validation_test.GetValidJSON_github_ui1(),
			contentType: "json",
			expected:    validation_test.GetValidJSON_github_ui1(),
			expectError: false,
			schema:      nil,
		},
		{
			name:        "github-wc1",
			input:       validation_test.GetValidJSON_github_wc(),
			contentType: "json",
			expected:    validation_test.GetValidJSON_github_wc(),
			expectError: false,
			schema:      nil,
		},
		{
			name:        "iam-ui1",
			input:       validation_test.GetValidJSON_iam_ui(),
			contentType: "json",
			expected:    validation_test.GetValidJSON_iam_ui(),
			expectError: false,
			schema:      nil,
		},
		{
			name:        "learnings",
			input:       validation_test.GetValidJSON_learnings(),
			contentType: "json",
			expected:    validation_test.GetValidJSON_learnings(),
			expectError: false,
			schema:      nil,
		},
		{
			name:        "organization-ui",
			input:       validation_test.GetValidJSON_organization_ui(),
			contentType: "json",
			expected:    validation_test.GetValidJSON_organization_ui(),
			expectError: false,
			schema:      nil,
		},
		{
			name:        "organization-ui2",
			input:       validation_test.GetValidJSON_organization_ui2(),
			contentType: "json",
			expected:    validation_test.GetValidJSON_organization_ui2(),
			expectError: false,
			schema:      nil,
		},
		{
			name:        "search-ui",
			input:       validation_test.GetValidJSON_search_ui(),
			contentType: "json",
			expected:    validation_test.GetValidJSON_search_ui(),
			expectError: false,
			schema:      nil,
		},
		{
			name:        "metadata-registry-wc",
			input:       validation_test.GetValidJSON_metadata_registry_wc(),
			contentType: "json",
			expected:    validation_test.GetValidJSON_metadata_registry_wc(),
			expectError: false,
			schema:      nil,
		},
		{
			name:        "review-extension",
			input:       validation_test.GetValidJSON_review_extension(),
			contentType: "json",
			expected:    validation_test.GetValidJSON_review_extension(),
			expectError: false,
			schema:      nil,
		},
		{
			name:        "tech-docs",
			input:       validation_test.GetValidJSON_tech_docs(),
			contentType: "json",
			expected:    validation_test.GetValidJSON_tech_docs(),
			expectError: false,
			schema:      nil,
		},
		{
			name:        "url",
			input:       validation_test.GetValidJSON_url(),
			contentType: "json",
			expected:    validation_test.GetValidJSON_url(),
			expectError: false,
			schema:      nil,
		},
		{
			name:        "home-ui",
			input:       validation_test.GetValidJSON_home_ui(),
			contentType: "json",
			expected:    validation_test.GetValidJSON_home_ui(),
			expectError: false,
			schema:      nil,
		},
	}

	cC := NewContentConfiguration()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.schema != nil {
				cC.WithSchema(tc.schema) // nolint: errcheck
			}
			result, err := cC.Validate([]byte(tc.input), tc.contentType)

			if tc.expectError {
				assert.GreaterOrEqual(t, err.Len(), 1)
				assert.Equal(t, tc.expected, result)
				assert.Contains(t, err.Error(), tc.errorMsg)
			} else {
				assert.Equal(t, err.Len(), 0)
				equal, err := commonTesting.CompareJSON(tc.expected, result)
				assert.NoError(t, err)
				assert.True(t, equal)
			}
		})
	}
}

func Test_validateSchema(t *testing.T) {
	type ContentConfigurationTypeMock struct {
		Name    interface{} `json:"name"`
		Surname interface{} `json:"surname"`
	}

	schema := getJSONSchemaFixture()

	tests := []struct {
		name                  string
		input                 interface{}
		expectedErrorMessages []string
	}{
		{
			name: "Invalid_Type",
			input: ContentConfigurationTypeMock{
				Name: 1, // wrong type
			},
			expectedErrorMessages: []string{
				"The document is not valid:\n%s",
				"luigiConfigFragment is required",
				"(root): Additional property surname is not allowed",
				"field 'name' is invalid, got '%!s(<nil>)', expected 'string'",
			},
		},
		{
			name: "Invalid_JSON",
			input: ContentConfigurationTypeMock{
				Name:    "John",
				Surname: make(chan int), // invalid type for JSON marshaling
			},
			expectedErrorMessages: []string{
				"error validating JSON data",
				"EOF",
			},
		},
		{
			name: "luigiConfigFragment_is_required",
			input: []byte(`{
				"name": "overview"
			}`),
			expectedErrorMessages: []string{
				"The document is not valid:\n%s",
				"field '(root)' is invalid, got '%!s(<nil>)', expected 'object'",
			},
		},
		{
			name: "name_is_required",
			input: ContentConfiguration{
				LuigiConfigFragment: LuigiConfigFragment{
					Data: LuigiConfigData{
						Nodes: []Node{
							{
								EntityType: "global",
							},
						},
					},
				},
			},
			expectedErrorMessages: []string{
				"The document is not valid:\n%s",
				"(root): Must validate one and only one schema (oneOf)",
				"name is required",
			},
		},
		{
			name: "nodes_is_required",
			input: ContentConfiguration{
				Name:                "overview",
				LuigiConfigFragment: LuigiConfigFragment{},
			},
			expectedErrorMessages: []string{
				"The document is not valid:\n%s",
				"luigiConfigFragment.data: Must validate one and only one schema (oneOf)",
				"nodes is required",
			},
		},
		{
			name: "textDictionary_is_required",
			input: ContentConfiguration{
				Name: "overview",
				LuigiConfigFragment: LuigiConfigFragment{
					Data: LuigiConfigData{
						Nodes: []Node{
							{
								EntityType: "global",
							},
						},
						Texts: []Text{{
							Locale: "de",
						}},
					},
				},
			},
			expectedErrorMessages: []string{
				"The document is not valid:\n%s",
				"field 'luigiConfigFragment.data.texts.0.textDictionary' is invalid, got '%!s(<nil>)', expected 'object'",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			byteArray, _ := json.Marshal(tt.input)

			merr := validateSchemaBytes(schema, byteArray)
			assert.Error(t, merr)
			assert.True(t, allErrorsContained(*merr, tt.expectedErrorMessages))
			assert.Equal(t, len(tt.expectedErrorMessages), merr.Len())
		})
	}
}

func getJSONSchemaFixture() []byte {
	schemaFilePath := "./schema/schema_autogen.json"
	schemaJSON, err := loadSchemaJSONFromFile(schemaFilePath)
	if err != nil {
		log.Fatalf("failed to load schema JSON from file: %v", err)
	}

	return schemaJSON
}

func TestWithSchema(t *testing.T) {
	cC := NewContentConfiguration()
	empty := ""
	err := cC.WithSchema([]byte(empty))
	assert.Error(t, err)
}

func allErrorsContained(merr multierror.Error, expectedErrors []string) bool {
	for _, err := range merr.Errors {
		found := false
		errStr := ""
		expectedError := ""
		for _, expectedError = range expectedErrors {
			errStr = err.Error()
			if errStr == expectedError {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
