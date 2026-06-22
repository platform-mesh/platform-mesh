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

package test

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	platformeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/controller"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/yaml"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/kcp-dev/multicluster-provider/apiexport"
	clusterclient "github.com/kcp-dev/multicluster-provider/client"
	"github.com/kcp-dev/multicluster-provider/envtest"
	apisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	apisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	"github.com/kcp-dev/sdk/apis/core"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	tenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"

	_ "embed"
)

var (
	//go:embed yaml/apiresourceschema-accountinfos.core.platform-mesh.io.yaml
	AccountInfoSchemaYAML []byte

	//go:embed yaml/apiresourceschema-accounts.core.platform-mesh.io.yaml
	AccountSchemaYAML []byte

	//go:embed yaml/apiresourceschema-authorizationmodels.core.platform-mesh.io.yaml
	AuthorizationModelSchemaYAML []byte

	//go:embed yaml/apiresourceschema-stores.core.platform-mesh.io.yaml
	StoreSchemaYAML []byte

	//go:embed yaml/apiresourceschema-invites.core.platform-mesh.io.yaml
	InviteSchemaYAML []byte

	//go:embed yaml/apiexport-core.platform-mesh.io.yaml
	ApiExportPlatformMeshSystemYAML []byte

	//go:embed yaml/apibinding-core-platform-mesh.io.yaml
	ApiBindingCorePlatformMeshYAML []byte

	//go:embed yaml/workspace-type-org.yaml
	WorkspaceTypeOrgYAML []byte

	//go:embed yaml/workspace-type-orgs.yaml
	WorkspaceTypeOrgsYAML []byte

	//go:embed yaml/workspace-type-account.yaml
	WorkspaceTypeAccountYAML []byte
)

func init() {
	utilruntime.Must(apisv1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(kcpcorev1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(tenancyv1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(corev1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(apisv1alpha2.AddToScheme(scheme.Scheme))
}

type IntegrationSuite struct {
	suite.Suite
	env                          *envtest.Environment
	kcpConfig                    *rest.Config
	apiExportEndpointSliceConfig *rest.Config
	platformMeshSysPath          logicalcluster.Path
	platformMeshSystemClient     client.Client
}

func TestIntegrationSuite(t *testing.T) {
	suite.Run(t, new(IntegrationSuite))
}

func (suite *IntegrationSuite) SetupSuite() {
	defaultCfg := platformeshconfig.NewDefaultConfig()

	logcfg := logger.DefaultConfig()
	logcfg.Output = io.Discard

	testLogger, err := logger.New(logcfg)
	require.NoError(suite.T(), err, "failed to create test logger")
	ctrl.SetLogger(testLogger.Logr())

	suite.env = &envtest.Environment{}

	suite.kcpConfig, err = suite.env.Start()
	require.NoError(suite.T(), err, "failed to start envtest environment")

	suite.T().Cleanup(func() {
		if err := suite.env.Stop(); err != nil {
			suite.T().Logf("error stopping envtest environment: %v", err)
		}
		suite.T().Log("kcp server has been stopped")
	})

	suite.setupPlatformMesh(suite.T())
	suite.setupControllers(defaultCfg, testLogger)
}

func (suite *IntegrationSuite) setupPlatformMesh(t *testing.T) {
	ctx := suite.T().Context()

	var err error
	cli, err := clusterclient.New(suite.kcpConfig, client.Options{})
	suite.Require().NoError(err)

	rootClient := cli.Cluster(core.RootCluster.Path())

	// create :root:platform-mesh-system ws
	_, platformMeshSystemClusterPath := envtest.NewWorkspaceFixture(suite.T(), cli, core.RootCluster.Path(), envtest.WithName("platform-mesh-system"))
	suite.platformMeshSysPath = platformMeshSystemClusterPath
	suite.platformMeshSystemClient = cli.Cluster(platformMeshSystemClusterPath)

	// register api-resource schemas
	schemas := [][]byte{AccountInfoSchemaYAML, AccountSchemaYAML, AuthorizationModelSchemaYAML, StoreSchemaYAML, InviteSchemaYAML}
	for _, schemaYAML := range schemas {
		var schema apisv1alpha1.APIResourceSchema
		suite.Require().NoError(yaml.Unmarshal(schemaYAML, &schema))
		err = cli.Cluster(platformMeshSystemClusterPath).Create(ctx, &schema)
		if err != nil && !kerrors.IsAlreadyExists(err) {
			suite.Require().NoError(err)
		}
		suite.T().Logf("created APIResourceSchema: %s", schema.Name)
	}
	suite.Require().NoError(err)

	var apiExport apisv1alpha1.APIExport
	suite.Require().NoError(yaml.Unmarshal(ApiExportPlatformMeshSystemYAML, &apiExport))

	err = cli.Cluster(platformMeshSystemClusterPath).Create(ctx, &apiExport)
	if err != nil && !kerrors.IsAlreadyExists(err) {
		suite.Require().NoError(err)
	}

	var platformMeshBinding apisv1alpha2.APIBinding
	suite.Require().NoError(yaml.Unmarshal(ApiBindingCorePlatformMeshYAML, &platformMeshBinding))

	err = cli.Cluster(platformMeshSystemClusterPath).Create(ctx, &platformMeshBinding)
	if err != nil && !kerrors.IsAlreadyExists(err) {
		suite.Require().NoError(err)
	}
	t.Log("created APIBinding 'core.platform-mesh.io' in platform-mesh-system workspace")
	suite.Assert().Eventually(func() bool {
		var binding apisv1alpha2.APIBinding
		if err := cli.Cluster(platformMeshSystemClusterPath).Get(ctx, client.ObjectKey{Name: platformMeshBinding.Name}, &binding); err != nil {
			return false
		}
		return binding.Status.Phase == apisv1alpha2.APIBindingPhaseBound
	}, 10*time.Second, 200*time.Millisecond, "APIBinding core.platform-mesh.io should be bound")

	// Create WorkspaceTypes in root workspace
	var orgWorkspaceType tenancyv1alpha1.WorkspaceType
	suite.Require().NoError(yaml.Unmarshal(WorkspaceTypeOrgYAML, &orgWorkspaceType))

	err = rootClient.Create(ctx, &orgWorkspaceType)
	if err != nil && !kerrors.IsAlreadyExists(err) {
		suite.Require().NoError(err)
	}
	t.Log("created WorkspaceType 'org' in root workspace")

	var orgsWorkspaceType tenancyv1alpha1.WorkspaceType
	suite.Require().NoError(yaml.Unmarshal(WorkspaceTypeOrgsYAML, &orgsWorkspaceType))

	err = rootClient.Create(ctx, &orgsWorkspaceType)
	if err != nil && !kerrors.IsAlreadyExists(err) {
		suite.Require().NoError(err)
	}
	t.Log("created WorkspaceType 'orgs' in root workspace")

	var accountWorkspaceType tenancyv1alpha1.WorkspaceType
	suite.Require().NoError(yaml.Unmarshal(WorkspaceTypeAccountYAML, &accountWorkspaceType))

	err = rootClient.Create(ctx, &accountWorkspaceType)
	if err != nil && !kerrors.IsAlreadyExists(err) {
		suite.Require().NoError(err)
	}
	t.Log("created WorkspaceType 'account' in root workspace")

	// create :root:orgs ws
	orgsWs, orgsClusterPath := envtest.NewWorkspaceFixture(suite.T(), cli, core.RootCluster.Path(), envtest.WithName("orgs"), envtest.WithType(core.RootCluster.Path(), "orgs"))
	t.Logf("orgs workspace path (%s), cluster id (%s)", orgsClusterPath, orgsWs.Spec.Cluster)

	var endpointSlice apisv1alpha1.APIExportEndpointSlice
	suite.Assert().Eventually(func() bool {
		err := cli.Cluster(platformMeshSystemClusterPath).Get(ctx, client.ObjectKey{Name: "core.platform-mesh.io"}, &endpointSlice)
		if err != nil {
			return false
		}
		return len(endpointSlice.Status.APIExportEndpoints) > 0 && endpointSlice.Status.APIExportEndpoints[0].URL != ""
	}, 10*time.Second, 200*time.Millisecond, "KCP should automatically create APIExportEndpointSlice with populated endpoints")

	suite.Require().NotEmpty(endpointSlice.Status.APIExportEndpoints, "APIExportEndpointSlice should have at least one endpoint")
	suite.Require().NotEqual("", endpointSlice.Status.APIExportEndpoints[0].URL, "APIExportEndpointSlice endpoint URL should not be empty")

	// set up config for virtual workspace
	cfg := rest.CopyConfig(suite.kcpConfig)
	cfg.Host = endpointSlice.Status.APIExportEndpoints[0].URL
	suite.apiExportEndpointSliceConfig = cfg
	t.Logf("created apiExportEndpointSliceConfig with host: %s", suite.apiExportEndpointSliceConfig.Host)
}

func (suite *IntegrationSuite) setupControllers(defaultCfg *platformeshconfig.CommonServiceConfig, testLogger *logger.Logger) {
	ctx := suite.T().Context()

	providerConfig, err := suite.getPlatformMeshSystemConfig(suite.apiExportEndpointSliceConfig)
	suite.Require().NoError(err)

	provider, err := apiexport.New(providerConfig, "core.platform-mesh.io", apiexport.Options{Scheme: scheme.Scheme})
	suite.Require().NoError(err)

	mgr, err := mcmanager.New(providerConfig, provider, mcmanager.Options{
		Scheme: scheme.Scheme,
	})
	suite.Require().NoError(err)

	operatorCfg := &config.Config{
		APIExportEndpointSlices: config.APIExportEndpointSlices{
			CorePlatformMeshIO: "core.platform-mesh.io",
		},
	}

	providerLister := iclient.NewProviderLister(provider.Provider)

	err = controller.NewAPIBindingReconciler(testLogger, mgr, providerLister, operatorCfg).SetupWithManager(mgr, defaultCfg)
	suite.Require().NoError(err)

	managerCtx, cancel := context.WithCancel(ctx)
	go func() {
		if err := mgr.Start(managerCtx); err != nil {
			suite.T().Logf("controller manager exited with error: %v", err)
		}
	}()

	suite.T().Cleanup(func() {
		cancel()
	})
}

func (suite *IntegrationSuite) createAccount(ctx context.Context, client client.Client, accountName string, accountType corev1alpha1.AccountType, t *testing.T) {
	account := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: accountName,
		},
		Spec: corev1alpha1.AccountSpec{
			Type: accountType,
		},
	}
	err := client.Create(ctx, account)
	if err != nil && !kerrors.IsAlreadyExists(err) {
		suite.Require().NoError(err)
	}
	t.Logf("created account '%s' (type: %s)", accountName, accountType)
}

func (suite *IntegrationSuite) createAccountInfo(ctx context.Context, accountClient client.Client, accountName, orgName string, accountPath, orgPath logicalcluster.Path, t *testing.T) {
	accountInfo := &corev1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{
			Name: "account",
		},
		Spec: corev1alpha1.AccountInfoSpec{
			Organization: corev1alpha1.AccountLocation{
				Name:               orgName,
				GeneratedClusterId: orgPath.String(),
				OriginClusterId:    orgPath.String(),
				Path:               orgPath.String(),
				Type:               corev1alpha1.AccountTypeOrg,
			},
			Account: corev1alpha1.AccountLocation{
				Name:               accountName,
				GeneratedClusterId: accountPath.String(),
				OriginClusterId:    accountPath.String(),
				Path:               accountPath.String(),
				Type:               corev1alpha1.AccountTypeAccount,
			},
			FGA: corev1alpha1.FGAInfo{
				Store: corev1alpha1.StoreInfo{
					Id: "test-store-id",
				},
			},
		},
	}
	err := accountClient.Create(ctx, accountInfo)
	if err != nil && !kerrors.IsAlreadyExists(err) {
		suite.Require().NoError(err)
	}
	t.Logf("created accountInfo 'account' in %s workspace", accountPath)
}

func (suite *IntegrationSuite) getPlatformMeshSystemConfig(cfg *rest.Config) (*rest.Config, error) {
	providerConfig := rest.CopyConfig(cfg)

	parsed, err := url.Parse(providerConfig.Host)
	if err != nil {
		return nil, fmt.Errorf("unable to parse URL: %w", err)
	}

	parsed.Path, err = url.JoinPath("clusters", suite.platformMeshSysPath.String())
	if err != nil {
		return nil, fmt.Errorf("failed to join path")
	}
	providerConfig.Host = parsed.String()

	return providerConfig, nil
}
