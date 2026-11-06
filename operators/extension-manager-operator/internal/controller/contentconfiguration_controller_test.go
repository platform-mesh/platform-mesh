//go:build integration

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

package controller

import (
	"context"
	"crypto/tls"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	resources "go.platform-mesh.io/apis/static-assets/resources/extension-manager-operator"
	"go.platform-mesh.io/apis/ui/v1alpha1"
	"go.platform-mesh.io/extension-manager-operator/internal/config"
	commonTesting "go.platform-mesh.io/extension-manager-operator/pkg/util/testing"
	"go.platform-mesh.io/extension-manager-operator/pkg/validation/validation_test"
	platformmeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/yaml"

	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/kcp-dev/multicluster-provider/apiexport"
	clusterclient "github.com/kcp-dev/multicluster-provider/client"
	"github.com/kcp-dev/multicluster-provider/envtest"
	apisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	"github.com/kcp-dev/sdk/apis/core"
	tenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	topologyv1alpha1 "github.com/kcp-dev/sdk/apis/topology/v1alpha1"
)

const (
	defaultTestTimeout  = 15 * time.Second
	defaultTickInterval = 250 * time.Millisecond
)

type ContentConfigurationTestSuite struct {
	suite.Suite
	cli                clusterclient.ClusterClient
	provider, consumer logicalcluster.Path
	consumerWS         *tenancyv1alpha1.Workspace
}

func init() {
	runtime.Must(v1alpha1.AddToScheme(scheme.Scheme))
	runtime.Must(apisv1alpha1.AddToScheme(scheme.Scheme))
	runtime.Must(tenancyv1alpha1.AddToScheme(scheme.Scheme))
	runtime.Must(topologyv1alpha1.AddToScheme(scheme.Scheme))
}

func (suite *ContentConfigurationTestSuite) SetupSuite() {
	logConfig := logger.DefaultConfig()
	logConfig.NoJSON = true
	logConfig.Name = "ContentConfigurationControllerTestSuite"
	log, err := logger.New(logConfig)
	ctrl.SetLogger(log.Logr())
	suite.Require().NoError(err, "failed to create logger %v", err)

	// Do not use T.Context() because that context would be cancelled before the cleanup
	// functions are executed, which would interfere with a clean test teardown.
	ctx, cancel := context.WithCancel(context.Background())
	suite.T().Cleanup(cancel)

	// Prevent the metrics listener being created
	metricsserver.DefaultBindAddress = "0"

	env := &envtest.Sharded{
		StartTimeout: 2 * time.Minute,
		StopTimeout:  time.Second * 30,
		WorkDir:      filepath.Join(os.Getenv("COMPONENT_DIRECTORY"), ".test", "ContentConfigurationTestSuite"),
	}

	err = os.Setenv("PRESERVE", "true")
	suite.Require().NoError(err, "failed to set PRESERVE environment variable")

	err = env.Start(ctx)
	if err != nil {
		suite.T().Fatalf("envtest failed to start (e.g. missing kcp binaries in bin/): %v", err)
	}

	suite.T().Cleanup(func() {
		if err := env.Stop(); err != nil {
			suite.T().Logf("Error stopping kcp environment: %v", err)
		}
	})

	kcpConfig := env.Config()

	suite.cli, err = clusterclient.New(kcpConfig, client.Options{})
	suite.Require().NoError(err, "failed to create cluster client")
	_, suite.provider = envtest.NewWorkspaceFixture(suite.T(), suite.cli, core.RootCluster.Path(), envtest.WithNamePrefix("provider"))
	suite.consumerWS, suite.consumer = envtest.NewWorkspaceFixture(suite.T(), suite.cli, core.RootCluster.Path(), envtest.WithNamePrefix("consumer"))

	// Prepare apiexports and resource schema
	suite.loadFromFile(ctx, "apiresourceschema-providermetadatas.ui.platform-mesh.io.yaml", suite.provider)
	suite.loadFromFile(ctx, "apiresourceschema-contentconfigurations.ui.platform-mesh.io.yaml", suite.provider)
	suite.loadFromFile(ctx, "apiexport-ui.platform-mesh.io.yaml", suite.provider)

	// Create apiexportendpointslice
	aes := &apisv1alpha1.APIExportEndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ui.platform-mesh.io",
		},
		Spec: apisv1alpha1.APIExportEndpointSliceSpec{
			APIExport: apisv1alpha1.ExportBindingReference{
				Name: "ui.platform-mesh.io",
				Path: suite.provider.String(),
			},
		},
	}
	suite.cli.Cluster(suite.provider).Create(ctx, aes) //nolint:errcheck

	ab := &apisv1alpha1.APIBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ui.platform-mesh.io",
		},
		Spec: apisv1alpha1.APIBindingSpec{
			Reference: apisv1alpha1.BindingReference{
				Export: &apisv1alpha1.ExportBindingReference{
					Name: "ui.platform-mesh.io",
					Path: suite.provider.String(),
				},
			},
		},
	}
	var createErr error
	suite.Eventually(func() bool {
		createErr = suite.cli.Cluster(suite.consumer).Create(ctx, ab.DeepCopy())
		if apierrors.IsAlreadyExists(createErr) {
			createErr = nil
		}
		return createErr == nil
	}, defaultTestTimeout, defaultTickInterval)
	suite.Require().NoError(createErr, "failed to create APIBinding for ui.platform-mesh.io in consumer workspace")

	suite.Eventually(func() bool {
		getErr := suite.cli.Cluster(suite.consumer).Get(ctx, types.NamespacedName{Name: "ui.platform-mesh.io"}, ab)
		return getErr == nil && ab.Status.Phase == apisv1alpha1.APIBindingPhaseBound
	}, 10*time.Second, 100*time.Millisecond, "APIBinding for ui.platform-mesh.io in consumer workspace did not become ready")

	// lookup api export
	err = suite.cli.Cluster(suite.provider).Get(ctx, types.NamespacedName{Name: "ui.platform-mesh.io"}, aes)
	suite.Require().NoError(err, "failed to get APIExport for ui.platform-mesh.io in provider workspace")

	// Config must point at the provider workspace so discovery can find APIExportEndpointSlice there
	// (same as multicluster-provider e2e: providerConfig.Host += provider.RequestPath()).
	providerConfig := rest.CopyConfig(kcpConfig)
	providerConfig.Host = strings.TrimSuffix(providerConfig.Host, "/") + suite.provider.RequestPath()
	// kcp envtest often fails discovery with "failed to get server groups: unknown" when the client
	// uses HTTP/2 (default). Force HTTP/1.1 so the cache's discovery client uses it (same as operator).
	providerConfig.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		if tr, ok := rt.(*http.Transport); ok {
			tr = tr.Clone()
			if tr.TLSClientConfig == nil {
				tr.TLSClientConfig = &tls.Config{}
			} else {
				tr.TLSClientConfig = tr.TLSClientConfig.Clone()
			}
			tr.TLSClientConfig.NextProtos = []string{"http/1.1"}
			return tr
		}
		return rt
	})
	// The provider drops a virtual workspace URL whose first watch fails and only retries it when
	// the endpoint slice changes again, so kcp must already answer discovery on every endpoint
	// before the provider starts. Right after creation the slice can list a URL that still 404s.
	// The provider's wildcard cache talks to <url>/clusters/*, and the bare URL never serves
	// discovery, so probe the same path.
	suite.Eventually(func() bool {
		if err := suite.cli.Cluster(suite.provider).Get(ctx, types.NamespacedName{Name: "ui.platform-mesh.io"}, aes); err != nil {
			return false
		}
		if len(aes.Status.APIExportEndpoints) == 0 {
			return false
		}
		for _, endpoint := range aes.Status.APIExportEndpoints {
			endpointConfig := rest.CopyConfig(providerConfig)
			endpointConfig.Host = strings.TrimSuffix(endpoint.URL, "/") + "/clusters/*"
			dc, err := discovery.NewDiscoveryClientForConfig(endpointConfig)
			if err != nil {
				return false
			}
			if _, err := dc.ServerGroups(); err != nil {
				return false
			}
		}
		return true
	}, 4*defaultTestTimeout, defaultTickInterval, "APIExport virtual workspace for ui.platform-mesh.io did not start serving discovery")

	provider, err := apiexport.New(providerConfig, "ui.platform-mesh.io", apiexport.Options{Scheme: scheme.Scheme})
	suite.Require().NoError(err, "failed to create APIExport provider for ui.platform-mesh.io")

	mgr, err := mcmanager.New(providerConfig, provider, mcmanager.Options{Logger: log.Logr()})
	suite.Require().NoError(err, "failed to create multicluster manager")

	operatorCfg := *config.NewOperatorConfig()
	rec := NewContentConfigurationReconciler(log, mgr, operatorCfg)

	err = rec.SetupWithManager(mgr, &platformmeshconfig.CommonServiceConfig{}, log)
	suite.Require().NoError(err, "failed to setup ContentConfiguration reconciler with manager")

	go mgr.Start(ctx)
}

// loadFromFile creates the manifest at filePath in the embedded config/resources in workspace.
func (suite *ContentConfigurationTestSuite) loadFromFile(ctx context.Context, filePath string, workspace logicalcluster.Path) {
	data, err := fs.ReadFile(resources.Resources, filePath)
	require.NoError(suite.T(), err, "failed to read file %s", filePath)

	var u unstructured.Unstructured
	err = yaml.Unmarshal(data, &u.Object)
	require.NoError(suite.T(), err, "failed to unmarshal file %s", filePath)

	err = suite.cli.Cluster(workspace).Create(ctx, &u)
	require.NoError(suite.T(), err, "failed to create resource %s", filePath)
}

func (suite *ContentConfigurationTestSuite) TestProcessContentConfiguration() {
	//Given
	var err error
	testContext := suite.T().Context()
	name := "example-content-configuration"
	cc := &v1alpha1.ContentConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1alpha1.ContentConfigurationSpec{
			InlineConfiguration: &v1alpha1.InlineConfiguration{
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
	err = suite.cli.Cluster(suite.consumer).Create(testContext, cc)
	suite.Require().NoError(err)

	// Wait for workspace creation and ready
	updatedCC := &v1alpha1.ContentConfiguration{}
	suite.Assert().Eventually(func() bool {
		err := suite.cli.Cluster(suite.consumer).Get(testContext, types.NamespacedName{Name: name}, updatedCC)
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
		spec           v1alpha1.ContentConfigurationSpec
		expectedResult string
	}{
		{
			name:         "TestInlineContentConfiguration",
			instanceName: "inline",
			spec: v1alpha1.ContentConfigurationSpec{
				InlineConfiguration: &v1alpha1.InlineConfiguration{
					ContentType: "yaml",
					Content:     validation_test.GetValidYAML(),
				},
			},
			expectedResult: validation_test.GetValidJSON(),
		},
		{
			name:         "TestBothInlineAndRemoteConfiguration",
			instanceName: "inline-and-remote",
			spec: v1alpha1.ContentConfigurationSpec{
				InlineConfiguration: &v1alpha1.InlineConfiguration{
					ContentType: "yaml",
					Content:     validation_test.GetValidYAML(),
				},
				RemoteConfiguration: &v1alpha1.RemoteConfiguration{
					URL:         "this-url-should-not-be-used",
					ContentType: "yaml",
				},
			},
			expectedResult: validation_test.GetValidJSON(),
		},
		{
			name:         "TestRemoteContentConfiguration",
			instanceName: "remote",
			spec: v1alpha1.ContentConfigurationSpec{
				RemoteConfiguration: &v1alpha1.RemoteConfiguration{
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
			instance := &v1alpha1.ContentConfiguration{
				ObjectMeta: metav1.ObjectMeta{Name: tc.instanceName},
				Spec:       tc.spec,
			}
			err := suite.cli.Cluster(suite.consumer).Create(testCtx, instance)
			suite.Require().NoError(err)

			createdInstance := v1alpha1.ContentConfiguration{}
			suite.Assert().Eventually(
				func() bool {
					err := suite.cli.Cluster(suite.consumer).Get(testCtx, types.NamespacedName{
						Name: tc.instanceName,
					}, &createdInstance)
					equal, cErr := commonTesting.CompareJSON(tc.expectedResult,
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
	contentConfiguration := &v1alpha1.ContentConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "extension-manager"},
		Spec: v1alpha1.ContentConfigurationSpec{
			RemoteConfiguration: &v1alpha1.RemoteConfiguration{
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
	err := suite.cli.Cluster(suite.consumer).Create(testCtx, contentConfiguration)
	suite.Require().NoError(err)

	// Then
	createdInstance := v1alpha1.ContentConfiguration{}
	suite.Assert().Eventually(
		func() bool {
			err := suite.cli.Cluster(suite.consumer).Get(testCtx, types.NamespacedName{
				Name: contentConfiguration.Name,
			}, &createdInstance)
			equal, cErr := commonTesting.CompareJSON(validation_test.GetValidJSON(),
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
	err = suite.cli.Cluster(suite.consumer).Update(testCtx, &createdInstance)
	suite.Require().NoError(err)

	// Then
	updatedInstance := v1alpha1.ContentConfiguration{}
	suite.Assert().Eventually(
		func() bool {
			err := suite.cli.Cluster(suite.consumer).Get(testCtx, types.NamespacedName{
				Name: contentConfiguration.Name,
			}, &updatedInstance)
			equal, cerr := commonTesting.CompareJSON(validation_test.GetValidJSONButDifferentName(),
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
	err = suite.cli.Cluster(suite.consumer).Update(testCtx, &updatedInstance)
	suite.Require().NoError(err)

	// Then
	updatedInstanceSameURL := v1alpha1.ContentConfiguration{}
	suite.Assert().Eventually(
		func() bool {
			err := suite.cli.Cluster(suite.consumer).Get(testCtx, types.NamespacedName{
				Name: contentConfiguration.Name,
			}, &updatedInstanceSameURL)
			return err == nil &&
				updatedInstanceSameURL.Status.ObservedGeneration == updatedInstanceSameURL.Generation
		},
		defaultTestTimeout, defaultTickInterval,
	)
	equal, err := commonTesting.CompareJSON(validation_test.GetValidJSON(),
		updatedInstanceSameURL.Status.ConfigurationResult)
	suite.NoError(err)
	suite.True(equal)
}

func (suite *ContentConfigurationTestSuite) TestContentConfigurationCreationCRInternalURL() {
	remoteURL := "https://this-address-should-be-mocked-by-httpmock"
	internalURL := "http://internal-url"
	testCtx := suite.T().Context()
	contentConfiguration := &v1alpha1.ContentConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "extension-manager-internal"},
		Spec: v1alpha1.ContentConfigurationSpec{
			RemoteConfiguration: &v1alpha1.RemoteConfiguration{
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

	err := suite.cli.Cluster(suite.consumer).Create(testCtx, contentConfiguration)
	suite.Require().NoError(err)

	createdInstance := v1alpha1.ContentConfiguration{}
	suite.Assert().Eventually(
		func() bool {
			err := suite.cli.Cluster(suite.consumer).Get(testCtx, types.NamespacedName{
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
	err = suite.cli.Cluster(suite.consumer).Get(testCtx, types.NamespacedName{
		Name: contentConfiguration.Name,
	}, &createdInstance)
	suite.Require().NoError(err)
	err = suite.cli.Cluster(suite.consumer).Update(testCtx, &createdInstance)
	suite.Require().NoError(err)

	time.Sleep(1 * time.Second)

	updatedInstance := v1alpha1.ContentConfiguration{}
	suite.Assert().Eventually(
		func() bool {
			err := suite.cli.Cluster(suite.consumer).Get(testCtx, types.NamespacedName{
				Name: contentConfiguration.Name,
			}, &updatedInstance)
			equal, cErr := commonTesting.CompareJSON(validation_test.GetValidJSON(),
				updatedInstance.Status.ConfigurationResult)
			return err == nil && cErr == nil && equal
		},
		defaultTestTimeout, defaultTickInterval,
	)
}

func TestContentConfigurationSuite(t *testing.T) {
	suite.Run(t, new(ContentConfigurationTestSuite))
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
