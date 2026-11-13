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

package subroutines

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	pmuiv1alpha1 "go.platform-mesh.io/apis/ui/v1alpha1"
	"go.platform-mesh.io/extension-manager-operator/pkg/subroutines/mocks"
	commonTesting "go.platform-mesh.io/extension-manager-operator/pkg/util/testing"
	"go.platform-mesh.io/extension-manager-operator/pkg/validation"
	"go.platform-mesh.io/extension-manager-operator/pkg/validation/validation_test"
	"go.platform-mesh.io/golang-commons/logger"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type ContentConfigurationSubroutineTestSuite struct {
	suite.Suite

	testObj *ContentConfigurationSubroutine

	// mocks
	clientMock *mocks.Client
}

func TestContentConfigurationSubroutineTestSuit(t *testing.T) {
	suite.Run(t, new(ContentConfigurationSubroutineTestSuite))
}

func (suite *ContentConfigurationSubroutineTestSuite) SetupTest() {
	// create new mock
	suite.clientMock = new(mocks.Client)

	// create new test object
	suite.testObj, _ = NewContentConfigurationSubroutine(validation.NewContentConfiguration(), http.DefaultClient, nil, nil)
}

func (suite *ContentConfigurationSubroutineTestSuite) TestCreateAndUpdate_OK() {
	// Given
	contentConfiguration := &pmuiv1alpha1.ContentConfiguration{
		Spec: pmuiv1alpha1.ContentConfigurationSpec{
			InlineConfiguration: &pmuiv1alpha1.InlineConfiguration{
				Content:     validation_test.GetValidYAML(),
				ContentType: "yaml",
			},
		},
	}

	// When
	_, err := suite.testObj.Process(context.Background(), contentConfiguration)

	// Then
	suite.Require().Nil(err)

	equal, cmpErr := commonTesting.CompareJSON(
		validation_test.GetValidJSON(),
		contentConfiguration.Status.ConfigurationResult,
	)
	suite.Require().Nil(cmpErr)
	suite.Require().True(equal)

	// Now lets take the same object and update it
	// Given
	contentConfiguration.Spec.InlineConfiguration.Content = validation_test.GetValidYAMLFixtureButDifferentName()

	// When
	_, err2 := suite.testObj.Process(context.Background(), contentConfiguration)

	// Then
	suite.Require().Nil(err2)
	equal, cmpErr = commonTesting.CompareJSON(validation_test.GetValidJSONButDifferentName(), contentConfiguration.Status.ConfigurationResult)
	suite.Require().Nil(cmpErr)
	suite.Require().True(equal)
}

func (suite *ContentConfigurationSubroutineTestSuite) TestCreateAndUpdate_Error() {
	// Given
	contentConfiguration := &pmuiv1alpha1.ContentConfiguration{
		Spec: pmuiv1alpha1.ContentConfigurationSpec{
			InlineConfiguration: &pmuiv1alpha1.InlineConfiguration{
				Content:     validation_test.GetValidYAML(),
				ContentType: "yaml",
			},
		},
	}

	// When
	_, errCmp := suite.testObj.Process(context.Background(), contentConfiguration)

	// Then
	suite.Require().Nil(errCmp)

	// compare configuration and result YAMLs
	cmp, cmpErr := commonTesting.CompareJSON(validation_test.GetValidJSON(), contentConfiguration.Status.ConfigurationResult)
	suite.Require().Nil(cmpErr)
	suite.Require().True(cmp)

	// Given invalid configuration
	contentConfiguration.Spec.InlineConfiguration.Content = "invalid"

	// When
	_, errProcessInvalidConfig := suite.testObj.Process(context.Background(), contentConfiguration)
	time.Sleep(1 * time.Second)

	// Then
	suite.Require().Nil(errProcessInvalidConfig)
	// result shoundn't change
	equal, cmpErr := commonTesting.CompareJSON(validation_test.GetValidJSON(), contentConfiguration.Status.ConfigurationResult)
	suite.Require().Nil(cmpErr)
	suite.Require().True(equal)
}

func (suite *ContentConfigurationSubroutineTestSuite) TestGetName_OK() {
	// When
	result := suite.testObj.GetName()

	// Then
	suite.Equal(ContentConfigurationSubroutineName, result)
}

func (suite *ContentConfigurationSubroutineTestSuite) TestProcessingConfig() {
	remoteURL := "https://this-address-should-be-mocked-by-httpmock"

	tests := []struct {
		name                 string
		spec                 pmuiv1alpha1.ContentConfigurationSpec
		remoteURL            string
		statusCode           int
		expectedErr          string
		expectedConfigResult string
	}{
		{
			name: "InlineConfigYAML_OK",
			spec: pmuiv1alpha1.ContentConfigurationSpec{
				InlineConfiguration: &pmuiv1alpha1.InlineConfiguration{
					Content:     validation_test.GetValidYAML(),
					ContentType: "yaml",
				},
			},
			expectedConfigResult: validation_test.GetValidJSON(),
		},
		{
			name: "InlineConfigYAML_ValidationError",
			spec: pmuiv1alpha1.ContentConfigurationSpec{
				InlineConfiguration: &pmuiv1alpha1.InlineConfiguration{
					Content:     "I am not a valid yaml",
					ContentType: "yaml",
				},
			},
		},
		{
			name: "InlineConfigJSON_OK",
			spec: pmuiv1alpha1.ContentConfigurationSpec{
				InlineConfiguration: &pmuiv1alpha1.InlineConfiguration{
					Content:     validation_test.GetValidJSON(),
					ContentType: "json",
				},
			},
			expectedConfigResult: validation_test.GetValidJSON(),
		},
		{
			name: "InlineConfigJSON_ValidationError",
			spec: pmuiv1alpha1.ContentConfigurationSpec{
				InlineConfiguration: &pmuiv1alpha1.InlineConfiguration{
					Content:     "I am not a valid json",
					ContentType: "json",
				},
			},
		},
		{
			name: "RemoteConfig_OK",
			spec: pmuiv1alpha1.ContentConfigurationSpec{
				RemoteConfiguration: &pmuiv1alpha1.RemoteConfiguration{
					ContentType: "json",
					URL:         remoteURL,
				},
			},
			remoteURL:            remoteURL,
			statusCode:           http.StatusOK,
			expectedConfigResult: validation_test.GetValidJSON(),
		},
		{
			name: "RemoteConfig_http_error",
			spec: pmuiv1alpha1.ContentConfigurationSpec{
				RemoteConfiguration: &pmuiv1alpha1.RemoteConfiguration{
					URL: remoteURL,
				},
			},
			remoteURL:   remoteURL,
			statusCode:  http.StatusInternalServerError,
			expectedErr: "received non-200 status code: 500",
		},
		{
			name:        "NoConfigProvider_Error",
			spec:        pmuiv1alpha1.ContentConfigurationSpec{},
			expectedErr: "no configuration provided",
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			if tt.remoteURL != "" {
				httpmock.Activate()
				defer httpmock.DeactivateAndReset()

				httpmock.RegisterResponder(
					"GET", tt.remoteURL, httpmock.NewStringResponder(tt.statusCode, validation_test.GetValidJSON()),
				)
			}

			// When
			contentConfiguration := pmuiv1alpha1.ContentConfiguration{
				Spec: tt.spec,
			}
			_, err := suite.testObj.Process(context.Background(), &contentConfiguration)

			// Then
			if tt.expectedErr != "" {
				suite.Require().Error(err)
				suite.Require().Equal(tt.expectedErr, err.Error())
			} else {
				suite.Require().NoError(err)
			}

			if tt.expectedConfigResult == "" {
				assert.Equal(suite.T(), "", contentConfiguration.Status.ConfigurationResult)
			} else {
				cmp, cmpErr := commonTesting.CompareJSON(tt.expectedConfigResult, contentConfiguration.Status.ConfigurationResult)
				suite.Require().Nil(cmpErr)
				suite.Require().True(cmp)
			}
		})
	}
}

func TestService_Do(t *testing.T) {
	log, err := logger.New(logger.DefaultConfig())
	require.NoError(t, err)
	tests := []struct {
		name           string
		url            string
		mockResponse   string
		mockStatusCode int
		mockError      error
		expectedBody   string
		expectError    bool
	}{
		{
			name:           "GET_request_OK",
			url:            "https://example.com/success",
			mockResponse:   `{"message": "success"}`,
			mockStatusCode: http.StatusOK,
			expectedBody:   `{"message": "success"}`,
			expectError:    false,
		},
		{
			name:           "status_code_500_Error",
			url:            "https://example.com/error",
			mockResponse:   `{"message": "error"}`,
			mockStatusCode: http.StatusInternalServerError,
			expectError:    true,
		},
		{
			name:           "status_code_404_Error",
			url:            "https://example.com/error",
			mockResponse:   `{"message": "error"}`,
			mockStatusCode: http.StatusNotFound,
			expectError:    true,
		},
		{
			name:        "network_Error",
			url:         "https://example.com/network-error",
			mockError:   errors.New("network error"),
			expectError: true,
		},
		{
			name:        "invalidurl",
			url:         "://invalid-url",
			mockError:   errors.New("network error"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpmock.Activate()
			defer httpmock.DeactivateAndReset()

			if tt.mockError != nil {
				httpmock.RegisterResponder(http.MethodGet, tt.url,
					httpmock.NewErrorResponder(tt.mockError))
			} else {
				httpmock.RegisterResponder(http.MethodGet, tt.url,
					httpmock.NewStringResponder(tt.mockStatusCode, tt.mockResponse))
			}

			r, err := NewContentConfigurationSubroutine(validation.NewContentConfiguration(), http.DefaultClient, nil, nil)
			require.NoError(t, err)

			var body []byte
			body, err = r.getRemoteConfig(tt.url, log)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedBody, string(body))
			}
		})
	}
}

func (suite *ContentConfigurationSubroutineTestSuite) Test_IncompatibleSchemaUpdate() {
	// Given
	contentConfiguration := &pmuiv1alpha1.ContentConfiguration{
		Spec: pmuiv1alpha1.ContentConfigurationSpec{
			InlineConfiguration: &pmuiv1alpha1.InlineConfiguration{
				Content:     validation_test.GetValidYAML(),
				ContentType: "yaml",
			},
		},
		Status: pmuiv1alpha1.ContentConfigurationStatus{
			Conditions: []metav1.Condition{
				{
					Type:    "Ready",
					Status:  "True",
					Message: "The resource is ready",
					Reason:  "Complete",
				},
				{
					Message: "The subroutine is complete",
					Reason:  "Complete",
					Status:  "True",
					Type:    "ContentConfigurationSubroutine_Ready",
				},
			},
			ConfigurationResult: validation_test.GetValidJSON(),
		},
	}

	// Simulate incompatible schema update
	contentConfiguration.Spec.InlineConfiguration.Content = validation_test.GetValidIncompatibleYAML()

	// When
	_, err := suite.testObj.Process(context.Background(), contentConfiguration)
	// Then: should keep previously valid and currently invalid result
	suite.Require().Nil(err)

	cmp, cmpErr := commonTesting.CompareJSON(validation_test.GetValidJSON(), contentConfiguration.Status.ConfigurationResult)
	suite.Require().Nil(cmpErr)
	suite.Require().True(cmp)
	suite.Require().True(
		getCondition(contentConfiguration.Status.Conditions, ValidationConditionType).Status == metav1.ConditionFalse,
	)
	suite.Require().Equal(
		"ValidationFailed", getCondition(contentConfiguration.Status.Conditions, ValidationConditionType).Reason,
	)

	// make it valid and check if condition is removed
	contentConfiguration.Spec.InlineConfiguration.Content = validation_test.GetValidYAML()

	// When
	_, err = suite.testObj.Process(context.Background(), contentConfiguration)
	suite.Require().Nil(err)

	cmp, cmpErr = commonTesting.CompareJSON(validation_test.GetValidJSON(), contentConfiguration.Status.ConfigurationResult)
	suite.Require().NoError(cmpErr)
	suite.Require().True(cmp)

	suite.Require().Equal(
		"ValidationSucceeded", getCondition(contentConfiguration.Status.Conditions, ValidationConditionType).Reason,
	)
}

func getCondition(conditions []metav1.Condition, conditionType string) metav1.Condition { //nolint:unparam
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition
		}
	}
	return metav1.Condition{}
}

// validJSONWithEntityType returns a valid CC JSON that references the given entityType
// in both nodeDefaults and a single node.
func validJSONWithEntityType(entityType string) string {
	return `{
		"name": "test-cc",
		"luigiConfigFragment": {
			"data": {
				"nodeDefaults": {
					"entityType": "` + entityType + `"
				},
				"nodes": [
					{
						"entityType": "` + entityType + `",
						"pathSegment": "home",
						"label": "Home"
					}
				]
			}
		}
	}`
}

// validJSONDefiningEntityType returns a valid CC JSON that defines a new entity type
// via defineEntity under a "global" parent.
func validJSONDefiningEntityType(defineEntityId string) string {
	return `{
		"name": "definer-cc",
		"luigiConfigFragment": {
			"data": {
				"nodes": [
					{
						"entityType": "global",
						"pathSegment": "root",
						"label": "Root",
						"defineEntity": {
							"id": "` + defineEntityId + `",
							"contextKey": "` + defineEntityId + `Id"
						},
						"children": []
					}
				]
			}
		}
	}`
}

func newFakeReader(objects ...ctrlruntimeclient.Object) ctrlruntimeclient.Reader {
	scheme := runtime.NewScheme()
	err := pmuiv1alpha1.AddToScheme(scheme)
	if err != nil {
		panic(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func TestProcess_EntityTypeValidation(t *testing.T) {
	tests := []struct {
		name                   string
		existingCCs            []ctrlruntimeclient.Object
		inlineContent          string
		registry               *validation.EntityTypeRegistry
		k8sReader              ctrlruntimeclient.Reader
		expectOperatorError    bool
		expectValidCondition   string
		expectValidReason      string
		expectConfigResult     bool
		expectRegistryContains []string
	}{
		{
			name: "registry init succeeds and populates from existing CCs",
			existingCCs: []ctrlruntimeclient.Object{
				&pmuiv1alpha1.ContentConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: "existing-cc"},
					Status: pmuiv1alpha1.ContentConfigurationStatus{
						ConfigurationResult: validJSONDefiningEntityType("project"),
					},
				},
			},
			inlineContent:          validJSONWithEntityType("project"),
			registry:               validation.NewEntityTypeRegistry(),
			expectValidCondition:   ConditionStatusTrue,
			expectValidReason:      ValidationConditionReasonSuccess,
			expectConfigResult:     true,
			expectRegistryContains: []string{"global", "project"},
		},
		{
			name:                   "registry init with empty CC list succeeds",
			existingCCs:            []ctrlruntimeclient.Object{},
			inlineContent:          validJSONWithEntityType("global"),
			registry:               validation.NewEntityTypeRegistry(),
			expectValidCondition:   ConditionStatusTrue,
			expectValidReason:      ValidationConditionReasonSuccess,
			expectConfigResult:     true,
			expectRegistryContains: []string{"global"},
		},
		{
			name:                 "entity type validation failure sets Valid=False condition",
			existingCCs:          []ctrlruntimeclient.Object{},
			inlineContent:        validJSONWithEntityType("nonexistent-type"),
			registry:             validation.NewEntityTypeRegistry(),
			expectValidCondition: ConditionStatusFalse,
			expectValidReason:    ValidationConditionReasonFailed,
			expectConfigResult:   false,
		},
		{
			name: "entity type validation success updates registry",
			existingCCs: []ctrlruntimeclient.Object{
				&pmuiv1alpha1.ContentConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: "definer"},
					Status: pmuiv1alpha1.ContentConfigurationStatus{
						ConfigurationResult: validJSONDefiningEntityType("mytype"),
					},
				},
			},
			inlineContent:          validJSONWithEntityType("mytype"),
			registry:               validation.NewEntityTypeRegistry(),
			expectValidCondition:   ConditionStatusTrue,
			expectValidReason:      ValidationConditionReasonSuccess,
			expectConfigResult:     true,
			expectRegistryContains: []string{"global", "mytype"},
		},
		{
			name: "initEntityTypeRegistry skips CC with empty ConfigurationResult",
			existingCCs: []ctrlruntimeclient.Object{
				&pmuiv1alpha1.ContentConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: "empty-result"},
					Status:     pmuiv1alpha1.ContentConfigurationStatus{ConfigurationResult: ""},
				},
			},
			inlineContent:          validJSONWithEntityType("global"),
			registry:               validation.NewEntityTypeRegistry(),
			expectValidCondition:   ConditionStatusTrue,
			expectValidReason:      ValidationConditionReasonSuccess,
			expectConfigResult:     true,
			expectRegistryContains: []string{"global"},
		},
		{
			name: "initEntityTypeRegistry skips CC with unparsable ConfigurationResult",
			existingCCs: []ctrlruntimeclient.Object{
				&pmuiv1alpha1.ContentConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: "bad-json"},
					Status: pmuiv1alpha1.ContentConfigurationStatus{
						ConfigurationResult: "{{not valid json at all",
					},
				},
			},
			inlineContent:          validJSONWithEntityType("global"),
			registry:               validation.NewEntityTypeRegistry(),
			expectValidCondition:   ConditionStatusTrue,
			expectValidReason:      ValidationConditionReasonSuccess,
			expectConfigResult:     true,
			expectRegistryContains: []string{"global"},
		},
		{
			name: "initEntityTypeRegistry skips unparsable but loads valid CCs",
			existingCCs: []ctrlruntimeclient.Object{
				&pmuiv1alpha1.ContentConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: "bad"},
					Status: pmuiv1alpha1.ContentConfigurationStatus{
						ConfigurationResult: "not json",
					},
				},
				&pmuiv1alpha1.ContentConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: "good"},
					Status: pmuiv1alpha1.ContentConfigurationStatus{
						ConfigurationResult: validJSONDefiningEntityType("team"),
					},
				},
			},
			inlineContent:          validJSONWithEntityType("team"),
			registry:               validation.NewEntityTypeRegistry(),
			expectValidCondition:   ConditionStatusTrue,
			expectValidReason:      ValidationConditionReasonSuccess,
			expectConfigResult:     true,
			expectRegistryContains: []string{"global", "team"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reader ctrlruntimeclient.Reader
			if tt.k8sReader != nil {
				reader = tt.k8sReader
			} else if tt.existingCCs != nil {
				reader = newFakeReader(tt.existingCCs...)
			}

			sub, err := NewContentConfigurationSubroutine(
				validation.NewContentConfiguration(),
				http.DefaultClient,
				reader,
				tt.registry,
			)
			require.NoError(t, err)

			cc := &pmuiv1alpha1.ContentConfiguration{
				Spec: pmuiv1alpha1.ContentConfigurationSpec{
					InlineConfiguration: &pmuiv1alpha1.InlineConfiguration{
						Content:     tt.inlineContent,
						ContentType: "json",
					},
				},
			}

			_, err = sub.Process(context.Background(), cc)

			if tt.expectOperatorError {
				require.NotNil(t, err, "expected an OperatorError but got nil")
				return
			}

			require.Nil(t, err, "unexpected OperatorError: %v", err)

			cond := getCondition(cc.Status.Conditions, ValidationConditionType)
			assert.Equal(t, tt.expectValidCondition, string(cond.Status), "unexpected Valid condition status")
			assert.Equal(t, tt.expectValidReason, cond.Reason, "unexpected Valid condition reason")

			if tt.expectConfigResult {
				assert.NotEmpty(t, cc.Status.ConfigurationResult, "expected ConfigurationResult to be set")
			} else {
				assert.Empty(t, cc.Status.ConfigurationResult, "expected ConfigurationResult to be empty")
			}

			if tt.registry != nil && len(tt.expectRegistryContains) > 0 {
				known := tt.registry.KnownTypes()
				for _, et := range tt.expectRegistryContains {
					assert.True(t, known[et], "expected registry to contain entity type %q", et)
				}
			}
		})
	}
}

func TestProcess_RegistryInitOnlyRunsOnce(t *testing.T) {
	reader := newFakeReader(
		&pmuiv1alpha1.ContentConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: "seed"},
			Status: pmuiv1alpha1.ContentConfigurationStatus{
				ConfigurationResult: validJSONDefiningEntityType("project"),
			},
		},
	)

	registry := validation.NewEntityTypeRegistry()
	sub, err := NewContentConfigurationSubroutine(
		validation.NewContentConfiguration(),
		http.DefaultClient,
		reader,
		registry,
	)
	require.NoError(t, err)

	cc1 := &pmuiv1alpha1.ContentConfiguration{
		Spec: pmuiv1alpha1.ContentConfigurationSpec{
			InlineConfiguration: &pmuiv1alpha1.InlineConfiguration{
				Content:     validJSONWithEntityType("global"),
				ContentType: "json",
			},
		},
	}

	_, err = sub.Process(context.Background(), cc1)
	require.Nil(t, err)
	assert.True(t, registry.KnownTypes()["project"], "registry should contain 'project' after init")

	cc2 := &pmuiv1alpha1.ContentConfiguration{
		Spec: pmuiv1alpha1.ContentConfigurationSpec{
			InlineConfiguration: &pmuiv1alpha1.InlineConfiguration{
				Content:     validJSONWithEntityType("global"),
				ContentType: "json",
			},
		},
	}
	_, err = sub.Process(context.Background(), cc2)
	require.Nil(t, err)

	assert.True(t, registry.KnownTypes()["project"])
}

func TestNew_RegistryWithoutReaderReturnsError(t *testing.T) {
	registry := validation.NewEntityTypeRegistry()
	_, err := NewContentConfigurationSubroutine(
		validation.NewContentConfiguration(),
		http.DefaultClient,
		nil,
		registry,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "k8s reader")
}

func TestProcess_NilRegistrySkipsEntityTypeValidation(t *testing.T) {
	sub, err := NewContentConfigurationSubroutine(
		validation.NewContentConfiguration(),
		http.DefaultClient,
		nil,
		nil,
	)
	require.NoError(t, err)

	cc := &pmuiv1alpha1.ContentConfiguration{
		Spec: pmuiv1alpha1.ContentConfigurationSpec{
			InlineConfiguration: &pmuiv1alpha1.InlineConfiguration{
				Content:     validJSONWithEntityType("nonexistent"),
				ContentType: "json",
			},
		},
	}

	_, err = sub.Process(context.Background(), cc)
	require.Nil(t, err)

	cond := getCondition(cc.Status.Conditions, ValidationConditionType)
	assert.Equal(t, string(ConditionStatusTrue), string(cond.Status))
	assert.NotEmpty(t, cc.Status.ConfigurationResult)
}

func TestProcess_EntityTypeValidationFailure_PreservesExistingConfigResult(t *testing.T) {
	reader := newFakeReader()
	registry := validation.NewEntityTypeRegistry()

	sub, err := NewContentConfigurationSubroutine(
		validation.NewContentConfiguration(),
		http.DefaultClient,
		reader,
		registry,
	)
	require.NoError(t, err)

	existingResult := validJSONWithEntityType("global")
	cc := &pmuiv1alpha1.ContentConfiguration{
		Spec: pmuiv1alpha1.ContentConfigurationSpec{
			InlineConfiguration: &pmuiv1alpha1.InlineConfiguration{
				Content:     validJSONWithEntityType("unknown-type"),
				ContentType: "json",
			},
		},
		Status: pmuiv1alpha1.ContentConfigurationStatus{
			ConfigurationResult: existingResult,
		},
	}

	_, err = sub.Process(context.Background(), cc)
	require.Nil(t, err)

	assert.Equal(t, existingResult, cc.Status.ConfigurationResult)

	cond := getCondition(cc.Status.Conditions, ValidationConditionType)
	assert.Equal(t, string(ConditionStatusFalse), string(cond.Status))
	assert.Equal(t, ValidationConditionReasonFailed, cond.Reason)
	assert.Contains(t, cond.Message, "unknown-type")
}

func TestProcess_ValidCC_UpdatesRegistryWithDefinedEntityTypes(t *testing.T) {
	reader := newFakeReader()
	registry := validation.NewEntityTypeRegistry()

	sub, err := NewContentConfigurationSubroutine(
		validation.NewContentConfiguration(),
		http.DefaultClient,
		reader,
		registry,
	)
	require.NoError(t, err)

	cc := &pmuiv1alpha1.ContentConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "definer", UID: "uid-definer"},
		Spec: pmuiv1alpha1.ContentConfigurationSpec{
			InlineConfiguration: &pmuiv1alpha1.InlineConfiguration{
				Content:     validJSONDefiningEntityType("newentity"),
				ContentType: "json",
			},
		},
	}

	_, err = sub.Process(context.Background(), cc)
	require.Nil(t, err)

	cond := getCondition(cc.Status.Conditions, ValidationConditionType)
	assert.Equal(t, string(ConditionStatusTrue), string(cond.Status))

	known := registry.KnownTypes()
	assert.True(t, known["newentity"], "registry should contain 'newentity' after processing CC that defines it")
	assert.True(t, known["global"], "registry should always contain 'global'")
}

func TestProcess_IdempotentRegistryLoad(t *testing.T) {
	reader := newFakeReader()
	registry := validation.NewEntityTypeRegistry()

	sub, err := NewContentConfigurationSubroutine(
		validation.NewContentConfiguration(),
		http.DefaultClient,
		reader,
		registry,
	)
	require.NoError(t, err)

	cc := &pmuiv1alpha1.ContentConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cc", UID: "uid-test"},
		Spec: pmuiv1alpha1.ContentConfigurationSpec{
			InlineConfiguration: &pmuiv1alpha1.InlineConfiguration{
				Content:     validJSONDefiningEntityType("project"),
				ContentType: "json",
			},
		},
	}

	_, err = sub.Process(context.Background(), cc)
	require.Nil(t, err)
	_, err = sub.Process(context.Background(), cc)
	require.Nil(t, err)

	registry.RemoveOwner("uid-test")
	assert.False(t, registry.KnownTypes()["project"], "project should be gone after single RemoveOwner")
}

func TestFinalize_RemovesFromRegistry(t *testing.T) {
	reader := newFakeReader()
	registry := validation.NewEntityTypeRegistry()

	sub, err := NewContentConfigurationSubroutine(
		validation.NewContentConfiguration(),
		http.DefaultClient,
		reader,
		registry,
	)
	require.NoError(t, err)

	cc := &pmuiv1alpha1.ContentConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "to-delete", UID: "uid-delete"},
		Spec: pmuiv1alpha1.ContentConfigurationSpec{
			InlineConfiguration: &pmuiv1alpha1.InlineConfiguration{
				Content:     validJSONDefiningEntityType("ephemeral"),
				ContentType: "json",
			},
		},
	}

	_, err = sub.Process(context.Background(), cc)
	require.Nil(t, err)
	assert.True(t, registry.KnownTypes()["ephemeral"])

	result, err := sub.Finalize(context.Background(), cc)
	require.Nil(t, err)
	assert.True(t, result.IsContinue())
	assert.False(t, registry.KnownTypes()["ephemeral"], "entity type should be removed after Finalize")
}

func TestFinalize_NilRegistry_NoOp(t *testing.T) {
	sub, err := NewContentConfigurationSubroutine(
		validation.NewContentConfiguration(),
		http.DefaultClient,
		nil,
		nil,
	)
	require.NoError(t, err)

	cc := &pmuiv1alpha1.ContentConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "test", UID: "uid-test"},
	}

	result, err := sub.Finalize(context.Background(), cc)
	require.Nil(t, err)
	assert.True(t, result.IsContinue())
}

func TestFinalizers_ReturnsFinalizerWhenRegistryEnabled(t *testing.T) {
	registry := validation.NewEntityTypeRegistry()
	sub, err := NewContentConfigurationSubroutine(
		validation.NewContentConfiguration(),
		http.DefaultClient,
		newFakeReader(),
		registry,
	)
	require.NoError(t, err)

	finalizers := sub.Finalizers(nil)
	require.Len(t, finalizers, 1)
	assert.Equal(t, "extension-manager.platform-mesh.io/entity-type-registry", finalizers[0])
}

func TestFinalizers_ReturnsNilWhenRegistryDisabled(t *testing.T) {
	sub, err := NewContentConfigurationSubroutine(
		validation.NewContentConfiguration(),
		http.DefaultClient,
		nil,
		nil,
	)
	require.NoError(t, err)

	finalizers := sub.Finalizers(nil)
	assert.Nil(t, finalizers)
}

func TestProcess_SelfReferencingCC_DefinesAndUsesOwnEntityType(t *testing.T) {
	reader := newFakeReader()
	registry := validation.NewEntityTypeRegistry()

	sub, err := NewContentConfigurationSubroutine(
		validation.NewContentConfiguration(),
		http.DefaultClient,
		reader,
		registry,
	)
	require.NoError(t, err)

	selfReferencingJSON := `{
		"name": "self-ref-cc",
		"luigiConfigFragment": {
			"data": {
				"nodes": [
					{
						"entityType": "global",
						"pathSegment": "root",
						"defineEntity": {
							"id": "project",
							"contextKey": "projectId"
						},
						"children": [
							{
								"entityType": "project",
								"pathSegment": "overview",
								"label": "Overview"
							}
						]
					}
				]
			}
		}
	}`

	cc := &pmuiv1alpha1.ContentConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "self-ref", UID: "uid-self-ref"},
		Spec: pmuiv1alpha1.ContentConfigurationSpec{
			InlineConfiguration: &pmuiv1alpha1.InlineConfiguration{
				Content:     selfReferencingJSON,
				ContentType: "json",
			},
		},
	}

	_, err = sub.Process(context.Background(), cc)
	require.Nil(t, err)

	cond := getCondition(cc.Status.Conditions, ValidationConditionType)
	assert.Equal(t, string(ConditionStatusTrue), string(cond.Status),
		"a CC that defines and references its own entity type should pass validation")
	assert.NotEmpty(t, cc.Status.ConfigurationResult)
}
