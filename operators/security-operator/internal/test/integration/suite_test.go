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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	platformeshconfig "go.platform-mesh.io/golang-commons/config"
	platformmeshcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/controller"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	multiprovider "sigs.k8s.io/multicluster-runtime/providers/multi"
	"sigs.k8s.io/yaml"

	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/kcp-dev/multicluster-provider/apiexport"
	clusterclient "github.com/kcp-dev/multicluster-provider/client"
	"github.com/kcp-dev/multicluster-provider/envtest"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	"github.com/kcp-dev/sdk/apis/core"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"

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

	//go:embed yaml/apiresourceschema-providerpermissions.providers.platform-mesh.io.yaml
	ProviderPermissionsSchemaYAML []byte

	//go:embed yaml/apiexport-core.platform-mesh.io.yaml
	ApiExportPlatformMeshSystemYAML []byte

	//go:embed yaml/apiexport-providers.platform-mesh.io.yaml
	ApiExportProvidersYAML []byte

	//go:embed yaml/apiexport-fga.platform-mesh.io.yaml
	ApiExportFgaYAML []byte

	//go:embed yaml/apiexport-provider-fga.platform-mesh.io.yaml
	ApiExportProviderFgaYAML []byte

	//go:embed yaml/apibinding-provider-fga-platform-mesh.io.yaml
	ApiBindingProviderFgaPlatformMeshYAML []byte

	//go:embed yaml/apibinding-core-platform-mesh.io.yaml
	ApiBindingCorePlatformMeshYAML []byte

	//go:embed yaml/apibinding-providers-platform-mesh.io.yaml
	ApiBindingProvidersPlatformMeshYAML []byte

	//go:embed yaml/workspace-type-org.yaml
	WorkspaceTypeOrgYAML []byte

	//go:embed yaml/workspace-type-orgs.yaml
	WorkspaceTypeOrgsYAML []byte

	//go:embed yaml/workspace-type-account.yaml
	WorkspaceTypeAccountYAML []byte
)

func init() {
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(kcpapisv1alpha2.AddToScheme(scheme.Scheme))
	utilruntime.Must(kcpcorev1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(kcptenancyv1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(pmcorev1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(pmprovidersv1alpha1.AddToScheme(scheme.Scheme))
}

type IntegrationSuite struct {
	suite.Suite
	env                      *envtest.Sharded
	kcpConfig                *rest.Config
	platformMeshSysPath      logicalcluster.Path
	platformMeshSystemClient ctrlruntimeclient.Client
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

	// Do not use T.Context() because that context would be cancelled before the cleanup
	// functions are executed, which would interfere with a clean test teardown.
	ctx, cancel, _ := platformmeshcontext.StartContext(testLogger, nil, 0)

	// Do not use the Suite's teardown to stop kcp, since this would happen
	// before the test's Cleanup functions are called. Since we use the envtest's
	// workspace fixture, we have to keep kcp alive until that fixture can clean up.
	suite.T().Cleanup(func() {
		cancel(fmt.Errorf("tearing down test suite"))
	})

	suite.env = &envtest.Sharded{
		StartTimeout: 2 * time.Minute,
		StopTimeout:  time.Second * 30,
		WorkDir:      filepath.Join(os.Getenv("COMPONENT_DIRECTORY"), ".test", "IntegrationSuite"),
	}

	err = suite.env.Start(ctx)
	require.NoError(suite.T(), err, "failed to start envtest environment")

	suite.T().Cleanup(func() {
		if err := suite.env.Stop(); err != nil {
			suite.T().Logf("error stopping envtest environment: %v", err)
		}
		suite.T().Log("kcp server has been stopped")
	})

	suite.kcpConfig = suite.env.Config()

	suite.setupPlatformMesh(suite.T())
	suite.setupControllers(defaultCfg, testLogger)
}

func (suite *IntegrationSuite) setupPlatformMesh(t *testing.T) {
	ctx := suite.T().Context()

	var err error
	cli, err := clusterclient.New(suite.kcpConfig, ctrlruntimeclient.Options{})
	suite.Require().NoError(err)

	rootClient := cli.Cluster(core.RootCluster.Path())

	// create :root:platform-mesh-system ws
	_, platformMeshSystemClusterPath := envtest.NewWorkspaceFixture(suite.T(), cli, core.RootCluster.Path(), envtest.WithName("platform-mesh-system"))
	suite.platformMeshSysPath = platformMeshSystemClusterPath
	suite.platformMeshSystemClient = cli.Cluster(platformMeshSystemClusterPath)

	// register api-resource schemas
	schemas := [][]byte{AccountInfoSchemaYAML, AccountSchemaYAML, AuthorizationModelSchemaYAML, StoreSchemaYAML, InviteSchemaYAML, ProviderPermissionsSchemaYAML}
	for _, schemaYAML := range schemas {
		var schema kcpapisv1alpha1.APIResourceSchema
		suite.Require().NoError(yaml.Unmarshal(schemaYAML, &schema))
		err = cli.Cluster(platformMeshSystemClusterPath).Create(ctx, &schema)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			suite.Require().NoError(err)
		}
		suite.T().Logf("created APIResourceSchema: %s", schema.Name)
	}
	suite.Require().NoError(err)

	var apiExport kcpapisv1alpha1.APIExport
	suite.Require().NoError(yaml.Unmarshal(ApiExportPlatformMeshSystemYAML, &apiExport))

	err = cli.Cluster(platformMeshSystemClusterPath).Create(ctx, &apiExport)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		suite.Require().NoError(err)
	}
	t.Log("created APIExport 'core.platform-mesh.io' in platform-mesh-system workspace")

	// Same split as production: stores in fga.platform-mesh.io, authorizationmodels in provider-fga.platform-mesh.io.
	for _, exportYAML := range [][]byte{ApiExportFgaYAML, ApiExportProviderFgaYAML} {
		var splitExport kcpapisv1alpha1.APIExport
		suite.Require().NoError(yaml.Unmarshal(exportYAML, &splitExport))
		err = cli.Cluster(platformMeshSystemClusterPath).Create(ctx, &splitExport)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			suite.Require().NoError(err)
		}
		t.Logf("created APIExport '%s' in platform-mesh-system workspace", splitExport.Name)
	}

	// Create providers.platform-mesh.io APIExport
	var providersApiExport kcpapisv1alpha1.APIExport
	suite.Require().NoError(yaml.Unmarshal(ApiExportProvidersYAML, &providersApiExport))

	err = cli.Cluster(platformMeshSystemClusterPath).Create(ctx, &providersApiExport)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		suite.Require().NoError(err)
	}
	t.Log("created APIExport 'providers.platform-mesh.io' in platform-mesh-system workspace")

	var platformMeshBinding kcpapisv1alpha2.APIBinding
	suite.Require().NoError(yaml.Unmarshal(ApiBindingCorePlatformMeshYAML, &platformMeshBinding))

	err = cli.Cluster(platformMeshSystemClusterPath).Create(ctx, &platformMeshBinding)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		suite.Require().NoError(err)
	}
	t.Log("created APIBinding 'core.platform-mesh.io' in platform-mesh-system workspace")
	suite.Assert().Eventually(func() bool {
		var binding kcpapisv1alpha2.APIBinding
		if err := cli.Cluster(platformMeshSystemClusterPath).Get(ctx, ctrlruntimeclient.ObjectKey{Name: platformMeshBinding.Name}, &binding); err != nil {
			return false
		}
		return binding.Status.Phase == kcpapisv1alpha2.APIBindingPhaseBound
	}, 10*time.Second, 200*time.Millisecond, "APIBinding core.platform-mesh.io should be bound")

	// Create providers.platform-mesh.io APIBinding
	var providersBinding kcpapisv1alpha2.APIBinding
	suite.Require().NoError(yaml.Unmarshal(ApiBindingProvidersPlatformMeshYAML, &providersBinding))

	err = cli.Cluster(platformMeshSystemClusterPath).Create(ctx, &providersBinding)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		suite.Require().NoError(err)
	}
	t.Log("created APIBinding 'providers.platform-mesh.io' in platform-mesh-system workspace")
	suite.Assert().Eventually(func() bool {
		var binding kcpapisv1alpha2.APIBinding
		if err := cli.Cluster(platformMeshSystemClusterPath).Get(ctx, ctrlruntimeclient.ObjectKey{Name: providersBinding.Name}, &binding); err != nil {
			return false
		}
		return binding.Status.Phase == kcpapisv1alpha2.APIBindingPhaseBound
	}, 10*time.Second, 200*time.Millisecond, "APIBinding providers.platform-mesh.io should be bound")

	// platform-mesh-system plays the provider in these tests, so it holds the AuthorizationModels.
	var providerFgaBinding kcpapisv1alpha2.APIBinding
	suite.Require().NoError(yaml.Unmarshal(ApiBindingProviderFgaPlatformMeshYAML, &providerFgaBinding))
	err = cli.Cluster(platformMeshSystemClusterPath).Create(ctx, &providerFgaBinding)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		suite.Require().NoError(err)
	}
	suite.Assert().Eventually(func() bool {
		var binding kcpapisv1alpha2.APIBinding
		if err := cli.Cluster(platformMeshSystemClusterPath).Get(ctx, ctrlruntimeclient.ObjectKey{Name: providerFgaBinding.Name}, &binding); err != nil {
			return false
		}
		return binding.Status.Phase == kcpapisv1alpha2.APIBindingPhaseBound
	}, 10*time.Second, 200*time.Millisecond, "APIBinding provider-fga.platform-mesh.io should be bound")

	// The bind permission check can lag behind the new APIExport when it is on another shard.
	createWorkspaceType := func(wst *kcptenancyv1alpha1.WorkspaceType) {
		suite.Require().Eventually(func() bool {
			err := rootClient.Create(ctx, wst.DeepCopy())
			return err == nil || apierrors.IsAlreadyExists(err)
		}, 30*time.Second, 500*time.Millisecond, "WorkspaceType %s should be created", wst.Name)
	}

	// Create WorkspaceTypes in root workspace
	var orgWorkspaceType kcptenancyv1alpha1.WorkspaceType
	suite.Require().NoError(yaml.Unmarshal(WorkspaceTypeOrgYAML, &orgWorkspaceType))

	createWorkspaceType(&orgWorkspaceType)
	t.Log("created WorkspaceType 'org' in root workspace")

	var orgsWorkspaceType kcptenancyv1alpha1.WorkspaceType
	suite.Require().NoError(yaml.Unmarshal(WorkspaceTypeOrgsYAML, &orgsWorkspaceType))

	createWorkspaceType(&orgsWorkspaceType)
	t.Log("created WorkspaceType 'orgs' in root workspace")

	var accountWorkspaceType kcptenancyv1alpha1.WorkspaceType
	suite.Require().NoError(yaml.Unmarshal(WorkspaceTypeAccountYAML, &accountWorkspaceType))

	createWorkspaceType(&accountWorkspaceType)
	t.Log("created WorkspaceType 'account' in root workspace")

	// create :root:orgs ws
	orgsWs, orgsClusterPath := envtest.NewWorkspaceFixture(suite.T(), cli, core.RootCluster.Path(), envtest.WithName("orgs"), envtest.WithType(core.RootCluster.Path(), "orgs"))
	t.Logf("orgs workspace path (%s), cluster id (%s)", orgsClusterPath, orgsWs.Spec.Cluster)

	var endpointSlice kcpapisv1alpha1.APIExportEndpointSlice
	suite.Assert().Eventually(func() bool {
		err := cli.Cluster(platformMeshSystemClusterPath).Get(ctx, ctrlruntimeclient.ObjectKey{Name: "core.platform-mesh.io"}, &endpointSlice)
		if err != nil {
			return false
		}
		return len(endpointSlice.Status.APIExportEndpoints) > 0 && endpointSlice.Status.APIExportEndpoints[0].URL != ""
	}, 10*time.Second, 200*time.Millisecond, "kcp should automatically create core.platform-mesh.io APIExportEndpointSlice with populated endpoints")

	suite.Require().NotEmpty(endpointSlice.Status.APIExportEndpoints, "APIExportEndpointSlice should have at least one endpoint")
	suite.Require().NotEqual("", endpointSlice.Status.APIExportEndpoints[0].URL, "APIExportEndpointSlice endpoint URL should not be empty")
}

func (suite *IntegrationSuite) setupControllers(defaultCfg *platformeshconfig.CommonServiceConfig, testLogger *logger.Logger) {
	ctx := suite.T().Context()

	pmSystemConfig, err := suite.getPlatformMeshSystemConfig(suite.kcpConfig)
	suite.Require().NoError(err)

	coreProvider, err := apiexport.New(pmSystemConfig, "core.platform-mesh.io", apiexport.Options{Scheme: scheme.Scheme})
	suite.Require().NoError(err)

	providersProvider, err := apiexport.New(pmSystemConfig, "providers.platform-mesh.io", apiexport.Options{Scheme: scheme.Scheme})
	suite.Require().NoError(err)

	fgaProvider, err := apiexport.New(pmSystemConfig, "fga.platform-mesh.io", apiexport.Options{Scheme: scheme.Scheme})
	suite.Require().NoError(err)

	providerFgaProvider, err := apiexport.New(pmSystemConfig, "provider-fga.platform-mesh.io", apiexport.Options{Scheme: scheme.Scheme})
	suite.Require().NoError(err)

	multiProv := multiprovider.New(multiprovider.Options{})
	err = multiProv.AddProvider(config.CoreProviderName, coreProvider)
	suite.Require().NoError(err)
	err = multiProv.AddProvider(config.ProvidersProviderName, providersProvider)
	suite.Require().NoError(err)
	err = multiProv.AddProvider(config.FgaProviderName, fgaProvider)
	suite.Require().NoError(err)
	err = multiProv.AddProvider(config.ProviderFgaProviderName, providerFgaProvider)
	suite.Require().NoError(err)

	mgr, err := mcmanager.New(pmSystemConfig, multiProv, mcmanager.Options{
		Scheme: scheme.Scheme,
	})
	suite.Require().NoError(err)

	operatorCfg := &config.Config{
		APIExportEndpointSlices: config.APIExportEndpointSlices{
			CorePlatformMeshIO:      "core.platform-mesh.io",
			ProvidersPlatformMeshIO: "providers.platform-mesh.io",
		},
	}

	providerLister := iclient.NewProviderLister(coreProvider.Provider)

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

func (suite *IntegrationSuite) createAccount(ctx context.Context, client ctrlruntimeclient.Client, accountName string, accountType pmcorev1alpha1.AccountType, t *testing.T) {
	account := &pmcorev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: accountName,
		},
		Spec: pmcorev1alpha1.AccountSpec{
			Type: accountType,
		},
	}
	err := client.Create(ctx, account)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		suite.Require().NoError(err)
	}
	t.Logf("created account '%s' (type: %s)", accountName, accountType)
}

// createTestStore creates a minimal Store, which must exist before an
// AuthorizationModel can bind to it.
func (suite *IntegrationSuite) createTestStore(ctx context.Context, storeClusterClient ctrlruntimeclient.Client, storeName string, t *testing.T) {
	store := &pmcorev1alpha1.Store{
		ObjectMeta: metav1.ObjectMeta{
			Name: storeName,
		},
	}
	err := storeClusterClient.Create(ctx, store)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		suite.Require().NoError(err)
	}
	t.Logf("created store '%s'", storeName)
}

// orgClusterID is the raw logical cluster ID the org workspace is stored
// under (not its path) - must match wherever createTestStore created the
// Store.
func (suite *IntegrationSuite) createAccountInfo(ctx context.Context, accountClient ctrlruntimeclient.Client, accountName, orgName, orgClusterID string, accountPath, orgPath logicalcluster.Path, t *testing.T) {
	accountInfo := &pmcorev1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{
			Name: "account",
		},
		Spec: pmcorev1alpha1.AccountInfoSpec{
			Organization: pmcorev1alpha1.AccountLocation{
				Name:               orgName,
				GeneratedClusterId: orgPath.String(),
				OriginClusterId:    orgClusterID,
				Path:               orgPath.String(),
				Type:               pmcorev1alpha1.AccountTypeOrg,
			},
			Account: pmcorev1alpha1.AccountLocation{
				Name:               accountName,
				GeneratedClusterId: accountPath.String(),
				OriginClusterId:    accountPath.String(),
				Path:               accountPath.String(),
				Type:               pmcorev1alpha1.AccountTypeAccount,
			},
			FGA: pmcorev1alpha1.FGAInfo{
				Store: pmcorev1alpha1.StoreInfo{
					Id: "test-store-id",
				},
			},
		},
	}
	err := accountClient.Create(ctx, accountInfo)
	if err != nil && !apierrors.IsAlreadyExists(err) {
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
