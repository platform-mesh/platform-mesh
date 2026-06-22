//go:build integration

package controller

import (
	"context"
	"crypto/tls"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jarcoal/httpmock"
	"github.com/kcp-dev/multicluster-provider/apiexport"
	platformmeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/yaml"

	"github.com/kcp-dev/logicalcluster/v3"
	clusterclient "github.com/kcp-dev/multicluster-provider/client"
	"github.com/kcp-dev/multicluster-provider/envtest"
	apisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	"github.com/kcp-dev/sdk/apis/core"
	tenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	topologyv1alpha1 "github.com/kcp-dev/sdk/apis/topology/v1alpha1"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"go.platform-mesh.io/apis/ui/v1alpha1"
	"go.platform-mesh.io/extension-manager-operator/internal/config"
	commonTesting "go.platform-mesh.io/extension-manager-operator/pkg/util/testing"
	"go.platform-mesh.io/extension-manager-operator/pkg/validation/validation_test"
)

const (
	defaultTestTimeout  = 15 * time.Second
	defaultTickInterval = 250 * time.Millisecond
)

var (
	env       *envtest.Environment
	kcpConfig *rest.Config
)

type ContentConfigurationTestSuite struct {
	suite.Suite
	cli                clusterclient.ClusterClient
	provider, consumer logicalcluster.Path
	consumerWS         *tenancyv1alpha1.Workspace
	ctx                context.Context
	cancel             context.CancelFunc
	g                  *errgroup.Group
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
	suite.ctx, suite.cancel = context.WithCancel(context.Background())
	// Prevent the metrics listener being created
	metricsserver.DefaultBindAddress = "0"

	env = &envtest.Environment{}
	env.BinaryAssetsDirectory = "../../bin"
	err = os.Setenv("PRESERVE", "true")
	suite.Require().NoError(err, "failed to set PRESERVE environment variable")
	kcpConfig, err = env.Start()
	if err != nil {
		suite.T().Fatalf("envtest failed to start (e.g. missing kcp binary in bin/): %v", err)
	}

	suite.cli, err = clusterclient.New(kcpConfig, client.Options{})
	suite.Require().NoError(err, "failed to create cluster client")
	_, suite.provider = envtest.NewWorkspaceFixture(suite.T(), suite.cli, core.RootCluster.Path(), envtest.WithNamePrefix("provider"))
	suite.consumerWS, suite.consumer = envtest.NewWorkspaceFixture(suite.T(), suite.cli, core.RootCluster.Path(), envtest.WithNamePrefix("consumer"))

	// Prepare apiexports and resource schema
	suite.loadFromFile("../../test/setup/apiresourceschema-providermetadatas.ui.platform-mesh.io.yaml", suite.provider)
	suite.loadFromFile("../../test/setup/apiresourceschema-contentconfigurations.ui.platform-mesh.io.yaml", suite.provider)
	suite.loadFromFile("../../test/setup/apiexport-ui.platform-mesh.io.yaml", suite.provider)

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
	suite.cli.Cluster(suite.provider).Create(suite.ctx, aes) //nolint:errcheck

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
	err = suite.cli.Cluster(suite.consumer).Create(suite.ctx, ab)
	suite.Require().NoError(err, "failed to create APIBinding for ui.platform-mesh.io in consumer workspace")

	suite.Eventually(func() bool {
		getErr := suite.cli.Cluster(suite.consumer).Get(suite.ctx, types.NamespacedName{Name: "ui.platform-mesh.io"}, ab)
		return getErr == nil && ab.Status.Phase == apisv1alpha1.APIBindingPhaseBound
	}, 10*time.Second, 100*time.Millisecond, "APIBinding for ui.platform-mesh.io in consumer workspace did not become ready")

	// lookup api export
	err = suite.cli.Cluster(suite.provider).Get(suite.ctx, types.NamespacedName{Name: "ui.platform-mesh.io"}, aes)
	suite.Require().NoError(err, "failed to get APIExport for ui.platform-mesh.io in provider workspace")

	// Config must point at the provider workspace so discovery can find APIExportEndpointSlice there
	// (same as multicluster-provider e2e: providerConfig.Host += provider.RequestPath()).
	providerConfig := rest.CopyConfig(kcpConfig)
	providerConfig.Host = strings.TrimSuffix(providerConfig.Host, "/") + suite.provider.RequestPath()
	// KCP envtest often fails discovery with "failed to get server groups: unknown" when the client
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
	provider, err := apiexport.New(providerConfig, "ui.platform-mesh.io", apiexport.Options{Scheme: scheme.Scheme})
	suite.Require().NoError(err, "failed to create APIExport provider for ui.platform-mesh.io")

	mgr, err := mcmanager.New(providerConfig, provider, mcmanager.Options{Logger: log.Logr()})
	suite.Require().NoError(err, "failed to create multicluster manager")

	operatorCfg := *config.NewOperatorConfig()
	rec := NewContentConfigurationReconciler(log, mgr, operatorCfg)

	err = rec.SetupWithManager(mgr, &platformmeshconfig.CommonServiceConfig{}, log)
	suite.Require().NoError(err, "failed to setup ContentConfiguration reconciler with manager")

	suite.g, _ = errgroup.WithContext(suite.ctx)
	suite.g.Go(func() error {
		return mgr.Start(suite.ctx)
	})
}

func (suite *ContentConfigurationTestSuite) loadFromFile(filePath string, workspace logicalcluster.Path) {
	data, err := os.ReadFile(filePath)
	require.NoError(suite.T(), err, "failed to read file %s", filePath)

	var u unstructured.Unstructured
	err = yaml.Unmarshal(data, &u.Object)
	require.NoError(suite.T(), err, "failed to unmarshal file %s", filePath)

	err = suite.cli.Cluster(workspace).Create(suite.ctx, &u)
	require.NoError(suite.T(), err, "failed to create resource %s", filePath)
}

func (suite *ContentConfigurationTestSuite) TearDownSuite() {
	suite.cancel()
	suite.g.Wait() //nolint:errcheck
	env.Stop()     //nolint:errcheck
}

func (suite *ContentConfigurationTestSuite) TestProcessContentConfiguration() {

	//Given
	var err error
	testContext := context.Background()
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

			testCtx := context.Background()
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
	testCtx := context.Background()
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
	testCtx := context.Background()
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
