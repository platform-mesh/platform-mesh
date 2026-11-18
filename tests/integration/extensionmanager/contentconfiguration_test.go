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

package extensionmanager

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/suite"

	extensionmanagerresources "go.platform-mesh.io/apis/static-assets/resources/extension-manager-operator"
	pmuiv1alpha1 "go.platform-mesh.io/apis/ui/v1alpha1"
	"go.platform-mesh.io/extension-manager-operator/pkg/app"
	"go.platform-mesh.io/extension-manager-operator/pkg/config"
	commontesting "go.platform-mesh.io/extension-manager-operator/pkg/util/testing"
	"go.platform-mesh.io/extension-manager-operator/pkg/validation/validation_test"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh/tests/framework/fixtures"
	"go.platform-mesh.io/platform-mesh/tests/framework/kcp"
	"go.platform-mesh.io/platform-mesh/tests/framework/manager"
	"go.platform-mesh.io/platform-mesh/tests/framework/scheme"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcptc "github.com/kcp-dev/kcp-testcontainers"
	"github.com/kcp-dev/logicalcluster/v3"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	"github.com/kcp-dev/sdk/apis/core"
)

const (
	defaultTestTimeout  = 15 * time.Second
	defaultTickInterval = 250 * time.Millisecond

	uiExport = "ui.platform-mesh.io"
)

// ContentConfigurationTestSuite binds ui.platform-mesh.io, exported from a
// provider workspace, into a consumer workspace and runs the operator's
// controllers over the export's virtual workspace.
type ContentConfigurationTestSuite struct {
	suite.Suite
	provider, consumer logicalcluster.Path
	consumerClient     ctrlruntimeclient.Client
}

func TestContentConfigurationSuite(t *testing.T) {
	suite.Run(t, new(ContentConfigurationTestSuite))
}

func (suite *ContentConfigurationTestSuite) SetupSuite() {
	t := suite.T()
	env, err := kcp.Default(t.Context())
	suite.Require().NoError(err)
	s := scheme.New(t, pmuiv1alpha1.AddToScheme)

	logConfig := logger.DefaultConfig()
	logConfig.NoJSON = true
	logConfig.Name = "ContentConfigurationControllerTestSuite"
	log, err := logger.New(logConfig)
	suite.Require().NoError(err, "failed to create logger")

	suite.provider, err = env.CreateWorkspace(t.Context(), core.RootCluster.Path().Join("provider-").String(), kcptc.WaitForReachable())
	suite.Require().NoError(err)
	suite.consumer, err = env.CreateWorkspace(t.Context(), core.RootCluster.Path().Join("consumer-").String(), kcptc.WaitForReachable())
	suite.Require().NoError(err)
	providerClient, err := env.Client(suite.provider.String(), ctrlruntimeclient.Options{Scheme: s})
	suite.Require().NoError(err)
	suite.consumerClient, err = env.Client(suite.consumer.String(), ctrlruntimeclient.Options{Scheme: s})
	suite.Require().NoError(err)

	// The operator's export shares the identity of core.platform-mesh.io, which
	// in a deployment lives next to it in platform-mesh-system; this provider
	// workspace has no such export, so it gets the identity secret kcp would
	// have created for one.
	key := make([]byte, 32)
	_, err = rand.Read(key)
	suite.Require().NoError(err)
	fixtures.Apply(t, providerClient,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kcp-system"}},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: "kcp-system", Name: "core.platform-mesh.io"},
			StringData: map[string]string{kcpapisv1alpha1.SecretKeyAPIExportIdentity: hex.EncodeToString(key)},
		},
	)

	// The APIResourceSchemas and the APIExport the operator ships.
	fixtures.ApplyFS(t, providerClient, extensionmanagerresources.Resources, "*.yaml")

	fixtures.Apply(t, providerClient, &kcpapisv1alpha1.APIExportEndpointSlice{
		ObjectMeta: metav1.ObjectMeta{Name: uiExport},
		Spec: kcpapisv1alpha1.APIExportEndpointSliceSpec{
			APIExport: kcpapisv1alpha1.ExportBindingReference{Name: uiExport, Path: suite.provider.String()},
		},
	})

	// The export can take a moment to become visible to the consumer's shard.
	binding := &kcpapisv1alpha1.APIBinding{
		ObjectMeta: metav1.ObjectMeta{Name: uiExport},
		Spec: kcpapisv1alpha1.APIBindingSpec{
			Reference: kcpapisv1alpha1.BindingReference{
				Export: &kcpapisv1alpha1.ExportBindingReference{Name: uiExport, Path: suite.provider.String()},
			},
		},
	}
	wait.Eventually(t, func() (bool, string) {
		if err := suite.consumerClient.Create(t.Context(), binding.DeepCopy()); err != nil && !apierrors.IsAlreadyExists(err) {
			return false, err.Error()
		}
		return true, ""
	}, defaultTestTimeout, defaultTickInterval, "creating APIBinding %s in the consumer workspace", uiExport)
	wait.APIBindingBound(t, suite.consumerClient, uiExport)
	wait.EndpointSliceServed(t, providerClient, uiExport)

	// The provider drops a virtual workspace URL whose first watch fails and only retries it when
	// the endpoint slice changes again, so kcp must already answer discovery on every endpoint
	// before the provider starts. Right after creation the slice can list a URL that still 404s.
	// The provider's wildcard cache talks to <url>/clusters/*, and the bare URL never serves
	// discovery, so probe the same path.
	wait.Eventually(t, func() (bool, string) {
		slice := &kcpapisv1alpha1.APIExportEndpointSlice{}
		if err := providerClient.Get(t.Context(), types.NamespacedName{Name: uiExport}, slice); err != nil {
			return false, err.Error()
		}
		for _, endpoint := range slice.Status.APIExportEndpoints {
			cfg := env.RESTConfig(suite.provider.String())
			cfg.Host = strings.TrimSuffix(endpoint.URL, "/") + "/clusters/*"
			dc, err := discovery.NewDiscoveryClientForConfig(cfg)
			if err != nil {
				return false, err.Error()
			}
			if _, err := dc.ServerGroups(); err != nil {
				return false, fmt.Sprintf("discovery on %s: %v", cfg.Host, err)
			}
		}
		return true, ""
	}, 4*defaultTestTimeout, defaultTickInterval, "APIExport virtual workspace for %s should serve discovery", uiExport)

	providers := manager.Providers(t, env, suite.provider, s, uiExport)
	mgr, _ := manager.New(t, env, suite.provider, s, providers)

	// Wire the controllers exactly as the operator binary does. HTTPClient stays nil
	// (http.DefaultClient) so httpmock, which swaps http.DefaultTransport, intercepts the
	// remote configuration fetches.
	err = app.SetupControllers(mgr, &config.Config{Operator: config.NewOperatorConfig()}, app.Deps{Log: log})
	suite.Require().NoError(err, "failed to setup controllers with manager")

	manager.Start(t, mgr)
}

func (suite *ContentConfigurationTestSuite) TestProcessContentConfiguration() {
	//Given
	var err error
	testContext := suite.T().Context()
	name := "example-content-configuration"
	cc := &pmuiv1alpha1.ContentConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: pmuiv1alpha1.ContentConfigurationSpec{
			InlineConfiguration: &pmuiv1alpha1.InlineConfiguration{
				ContentType: "json",
				Content: `{
								"name": "overview",
								"creationTimestamp": "2022-05-17T11:37:17Z",
								"luigiConfigFragment": {
								  "data": {
									"nodes": [
									  {
										"entityType": "global",
										"pathSegment": "home",
										"hideFromNav": false,
										"order": 1,
										"label": "Home",
										"icon": "account",
										"defineEntity": {
										  "id": "main"
										},
										"children": [
										  {
											"pathSegment": "overview",
											"label": "Overview",
											"icon": "home",
											"defineEntity": {
											  "id": "overview"
											},
											"compound": {
											  "renderer": {
												"use": "grid",
												"config": {
												  "columns": "1fr 1fr 1fr 1fr"
												}
											  }
											}
										  }
										]
									  }
									]
								  }
								}
							  }`,
			},
		},
	}

	// When
	err = suite.consumerClient.Create(testContext, cc)
	suite.Require().NoError(err)

	// Wait for workspace creation and ready
	updatedCC := &pmuiv1alpha1.ContentConfiguration{}
	suite.Assert().Eventually(func() bool {
		err := suite.consumerClient.Get(testContext, types.NamespacedName{Name: name}, updatedCC)
		readyCondition := meta.FindStatusCondition(updatedCC.Status.Conditions, "Ready")
		return err == nil && readyCondition != nil && readyCondition.Status == metav1.ConditionTrue
	}, defaultTestTimeout, defaultTickInterval)

	suite.verifyCondition(updatedCC.Status.Conditions, "Ready", metav1.ConditionTrue, "Complete")
	suite.verifyCondition(updatedCC.Status.Conditions, "Valid", metav1.ConditionTrue, "ValidationSucceeded")
}

func (suite *ContentConfigurationTestSuite) TestContentConfigurationCRCreation() {
	remoteURL := "https://this-address-should-be-mocked-by-httpmock"
	testCases := []struct {
		name           string
		instanceName   string
		spec           pmuiv1alpha1.ContentConfigurationSpec
		expectedResult string
	}{
		{
			name:         "TestInlineContentConfiguration",
			instanceName: "inline",
			spec: pmuiv1alpha1.ContentConfigurationSpec{
				InlineConfiguration: &pmuiv1alpha1.InlineConfiguration{
					ContentType: "yaml",
					Content:     validation_test.GetValidYAML(),
				},
			},
			expectedResult: validation_test.GetValidJSON(),
		},
		{
			name:         "TestBothInlineAndRemoteConfiguration",
			instanceName: "inline-and-remote",
			spec: pmuiv1alpha1.ContentConfigurationSpec{
				InlineConfiguration: &pmuiv1alpha1.InlineConfiguration{
					ContentType: "yaml",
					Content:     validation_test.GetValidYAML(),
				},
				RemoteConfiguration: &pmuiv1alpha1.RemoteConfiguration{
					URL:         "this-url-should-not-be-used",
					ContentType: "yaml",
				},
			},
			expectedResult: validation_test.GetValidJSON(),
		},
		{
			name:         "TestRemoteContentConfiguration",
			instanceName: "remote",
			spec: pmuiv1alpha1.ContentConfigurationSpec{
				RemoteConfiguration: &pmuiv1alpha1.RemoteConfiguration{
					ContentType: "json",
					URL:         remoteURL,
				},
			},
			expectedResult: validation_test.GetValidJSON(),
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			httpmock.Activate()
			defer httpmock.DeactivateAndReset()
			httpmock.RegisterResponder(
				"GET", remoteURL, httpmock.NewStringResponder(200, validation_test.GetValidJSON()),
			)

			testCtx := suite.T().Context()
			instance := &pmuiv1alpha1.ContentConfiguration{
				ObjectMeta: metav1.ObjectMeta{Name: tc.instanceName},
				Spec:       tc.spec,
			}
			err := suite.consumerClient.Create(testCtx, instance)
			suite.Require().NoError(err)

			createdInstance := pmuiv1alpha1.ContentConfiguration{}
			suite.Assert().Eventually(
				func() bool {
					err := suite.consumerClient.Get(testCtx, types.NamespacedName{
						Name: tc.instanceName,
					}, &createdInstance)
					equal, cErr := commontesting.CompareJSON(tc.expectedResult,
						createdInstance.Status.ConfigurationResult)
					return err == nil && cErr == nil && equal
				},
				defaultTestTimeout, defaultTickInterval,
			)
		})
	}
}

func (suite *ContentConfigurationTestSuite) TestUpdateReconcileCR() {
	remoteURL := "https://this-address-should-be-mocked-by-httpmock"

	// Given
	testCtx := suite.T().Context()
	contentConfiguration := &pmuiv1alpha1.ContentConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "extension-manager"},
		Spec: pmuiv1alpha1.ContentConfigurationSpec{
			RemoteConfiguration: &pmuiv1alpha1.RemoteConfiguration{
				ContentType: "json",
				URL:         remoteURL,
			},
		},
	}

	// setup mocks
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	httpmock.RegisterResponder(
		"GET", remoteURL, httpmock.NewStringResponder(200, validation_test.GetValidJSON()),
	)

	// When
	err := suite.consumerClient.Create(testCtx, contentConfiguration)
	suite.Require().NoError(err)

	// Then
	createdInstance := pmuiv1alpha1.ContentConfiguration{}
	suite.Assert().Eventually(
		func() bool {
			err := suite.consumerClient.Get(testCtx, types.NamespacedName{
				Name: contentConfiguration.Name,
			}, &createdInstance)
			equal, cErr := commontesting.CompareJSON(validation_test.GetValidJSON(),
				createdInstance.Status.ConfigurationResult)
			return err == nil && cErr == nil && equal
		},
		defaultTestTimeout, defaultTickInterval,
	)

	// Update ContentConfiguration and check for 2nd reconcile
	// Given
	remoteURL = "https://new.url"
	createdInstance.Spec.RemoteConfiguration.URL = remoteURL
	httpmock.RegisterResponder(
		"GET", remoteURL, httpmock.NewStringResponder(200, validation_test.GetValidJSONButDifferentName()),
	)

	// When
	err = suite.consumerClient.Update(testCtx, &createdInstance)
	suite.Require().NoError(err)

	// Then
	updatedInstance := pmuiv1alpha1.ContentConfiguration{}
	suite.Assert().Eventually(
		func() bool {
			err := suite.consumerClient.Get(testCtx, types.NamespacedName{
				Name: contentConfiguration.Name,
			}, &updatedInstance)
			equal, cerr := commontesting.CompareJSON(validation_test.GetValidJSONButDifferentName(),
				updatedInstance.Status.ConfigurationResult)
			return err == nil && cerr == nil && equal
		},
		defaultTestTimeout, defaultTickInterval,
	)

	// 3rd reconcile: the same URL but it returns a different content; changed labels
	// Given
	remoteURL = "https://new.url2"
	updatedInstance.Spec.RemoteConfiguration.URL = remoteURL
	httpmock.Reset()
	httpmock.RegisterResponder(
		"GET", remoteURL, httpmock.NewStringResponder(200, validation_test.GetValidJSON()),
	)

	// When
	err = suite.consumerClient.Update(testCtx, &updatedInstance)
	suite.Require().NoError(err)

	// Then
	updatedInstanceSameURL := pmuiv1alpha1.ContentConfiguration{}
	suite.Assert().Eventually(
		func() bool {
			err := suite.consumerClient.Get(testCtx, types.NamespacedName{
				Name: contentConfiguration.Name,
			}, &updatedInstanceSameURL)
			return err == nil &&
				updatedInstanceSameURL.Status.ObservedGeneration == updatedInstanceSameURL.Generation
		},
		defaultTestTimeout, defaultTickInterval,
	)
	equal, err := commontesting.CompareJSON(validation_test.GetValidJSON(),
		updatedInstanceSameURL.Status.ConfigurationResult)
	suite.NoError(err)
	suite.True(equal)
}

func (suite *ContentConfigurationTestSuite) TestContentConfigurationCreationCRInternalURL() {
	remoteURL := "https://this-address-should-be-mocked-by-httpmock"
	internalURL := "http://internal-url"
	testCtx := suite.T().Context()
	contentConfiguration := &pmuiv1alpha1.ContentConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "extension-manager-internal"},
		Spec: pmuiv1alpha1.ContentConfigurationSpec{
			RemoteConfiguration: &pmuiv1alpha1.RemoteConfiguration{
				ContentType: "json",
				InternalUrl: internalURL,
				URL:         remoteURL,
			},
		},
	}

	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	httpmock.RegisterResponder(
		"GET", remoteURL, httpmock.NewStringResponder(200, validation_test.GetValidJSON()),
	)

	err := suite.consumerClient.Create(testCtx, contentConfiguration)
	suite.Require().NoError(err)

	createdInstance := pmuiv1alpha1.ContentConfiguration{}
	suite.Assert().Eventually(
		func() bool {
			err := suite.consumerClient.Get(testCtx, types.NamespacedName{
				Name: contentConfiguration.Name,
			}, &createdInstance)
			return err == nil && createdInstance.Status.ConfigurationResult == ""
		},
		defaultTestTimeout, defaultTickInterval,
	)

	httpmock.RegisterResponder(
		"GET", internalURL, httpmock.NewStringResponder(200, validation_test.GetValidJSON()),
	)

	time.Sleep(2 * time.Second)
	err = suite.consumerClient.Get(testCtx, types.NamespacedName{
		Name: contentConfiguration.Name,
	}, &createdInstance)
	suite.Require().NoError(err)
	err = suite.consumerClient.Update(testCtx, &createdInstance)
	suite.Require().NoError(err)

	time.Sleep(1 * time.Second)

	updatedInstance := pmuiv1alpha1.ContentConfiguration{}
	suite.Assert().Eventually(
		func() bool {
			err := suite.consumerClient.Get(testCtx, types.NamespacedName{
				Name: contentConfiguration.Name,
			}, &updatedInstance)
			equal, cErr := commontesting.CompareJSON(validation_test.GetValidJSON(),
				updatedInstance.Status.ConfigurationResult)
			return err == nil && cErr == nil && equal
		},
		defaultTestTimeout, defaultTickInterval,
	)
}

func (suite *ContentConfigurationTestSuite) verifyCondition(conditions []metav1.Condition, conditionType string, status metav1.ConditionStatus, reason string) {
	condition := getCondition(conditions, conditionType)
	suite.Require().NotNil(condition)
	suite.Equal(status, condition.Status)
	suite.Equal(reason, condition.Reason)
}

func getCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return &condition
		}
	}
	return nil
}
