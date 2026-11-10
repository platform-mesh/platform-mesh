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

package security

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmopenfgav1alpha1 "go.platform-mesh.io/apis/openfga/v1alpha1"
	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	openfgaresources "go.platform-mesh.io/apis/static-assets/resources/pm-openfga-operator"
	securityresources "go.platform-mesh.io/apis/static-assets/resources/security-operator"
	platformeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh/tests/framework/fixtures"
	"go.platform-mesh.io/platform-mesh/tests/framework/kcp"
	"go.platform-mesh.io/platform-mesh/tests/framework/manager"
	"go.platform-mesh.io/platform-mesh/tests/framework/scheme"
	"go.platform-mesh.io/pm-openfga-operator/pkg/app"
	"go.platform-mesh.io/pm-openfga-operator/pkg/config"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcptc "github.com/kcp-dev/kcp-testcontainers"
	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/kcp-dev/multicluster-provider/apiexport"
	"github.com/kcp-dev/sdk/apis/core"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	kcptesting "github.com/kcp-dev/sdk/testing"
)

const (
	coreExport            = "core.platform-mesh.io"
	providersExport       = "providers.platform-mesh.io"
	openfgaExport         = "openfga.platform-mesh.io"
	providerOpenfgaExport = "provider-openfga.platform-mesh.io"
)

// IntegrationSuite shares one platform-mesh-system and one model-generator
// manager between the tests, as the operator's former suite did.
type IntegrationSuite struct {
	suite.Suite
	env      *kcptc.Instance
	scheme   *runtime.Scheme
	platform *fixtures.Platform
	// systemClient is the client of :root:platform-mesh-system.
	systemClient ctrlruntimeclient.Client
}

func TestIntegrationSuite(t *testing.T) {
	suite.Run(t, new(IntegrationSuite))
}

func (s *IntegrationSuite) SetupSuite() {
	t := s.T()
	var err error
	s.env, err = kcp.Security(t.Context())
	require.NoError(t, err)
	s.scheme = scheme.New(t, pmcorev1alpha1.AddToScheme, pmopenfgav1alpha1.AddToScheme, pmprovidersv1alpha1.AddToScheme)
	s.platform = fixtures.Install(t, s.env,
		fixtures.WithScheme(s.scheme),
		fixtures.WithResources(securityresources.Resources, "*.yaml"),
		fixtures.WithResources(openfgaresources.Resources, "*.yaml"),
		// The deployed manifests serve AuthorizationModels from an export of
		// their own; apigen puts the whole group into openfga.platform-mesh.io.
		fixtures.WithExport(providerOpenfgaExport, openfgaExport, "authorizationmodels"),
		// platform-mesh-system plays the provider in these tests, so it binds
		// provider-fga and holds the AuthorizationModels.
		fixtures.WithBindings(coreExport, providersExport, providerOpenfgaExport),
		// Orgs hold their Store; in production the security-operator's org
		// initializer (not run here) binds fga.platform-mesh.io for them.
		fixtures.WithDefaultBindings("org", openfgaExport),
		fixtures.WithOrgs(),
	)
	s.systemClient = s.platform.SystemClient

	s.setupControllers(t)
}

func (s *IntegrationSuite) setupControllers(t *testing.T) {
	logcfg := logger.DefaultConfig()
	if os.Getenv("TEST_QUIET") == "1" {
		logcfg.Output = io.Discard
	}
	log, err := logger.New(logcfg)
	require.NoError(t, err, "creating operator logger")

	byExport := manager.Providers(t, s.env, s.platform.System, s.scheme, coreExport, providersExport, openfgaExport, providerOpenfgaExport)
	// The operator finds its clusters by provider name prefix (config.MultiProviderName).
	providers := map[string]*apiexport.Provider{
		config.CoreProviderName:            byExport[coreExport],
		config.ProvidersProviderName:       byExport[providersExport],
		config.OpenfgaProviderName:         byExport[openfgaExport],
		config.ProviderOpenfgaProviderName: byExport[providerOpenfgaExport],
	}
	mgr, _ := manager.New(t, s.env, s.platform.System, s.scheme, providers)

	operatorCfg := &config.Config{
		APIExportEndpointSlices: config.APIExportEndpointSlices{
			CorePlatformMeshIO:      coreExport,
			ProvidersPlatformMeshIO: providersExport,
		},
	}
	require.NoError(t, app.SetupModelGeneratorControllers(mgr, operatorCfg, app.Deps{
		Log:          log,
		CommonConfig: platformeshconfig.NewDefaultConfig(),
		CoreProvider: byExport[coreExport].Provider,
	}))

	manager.Start(t, mgr)
}

func (s *IntegrationSuite) createAccount(t *testing.T, cl ctrlruntimeclient.Client, accountName string, accountType pmcorev1alpha1.AccountType) {
	account := &pmcorev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: accountName},
		Spec:       pmcorev1alpha1.AccountSpec{Type: accountType},
	}
	if err := cl.Create(t.Context(), account); err != nil && !apierrors.IsAlreadyExists(err) {
		s.Require().NoError(err)
	}
	t.Logf("created account '%s' (type: %s)", accountName, accountType)
}

// createTestStore creates a minimal Store, which must exist before an
// AuthorizationModel can bind to it.
func (s *IntegrationSuite) createTestStore(t *testing.T, cl ctrlruntimeclient.Client, storeName string) {
	store := &pmopenfgav1alpha1.Store{ObjectMeta: metav1.ObjectMeta{Name: storeName}}
	if err := cl.Create(t.Context(), store); err != nil && !apierrors.IsAlreadyExists(err) {
		s.Require().NoError(err)
	}
	t.Logf("created store '%s'", storeName)
}

// createAccountInfo creates the AccountInfo of an account workspace.
// orgClusterID is the raw logical cluster ID the org workspace is stored
// under (not its path) - must match wherever createTestStore created the
// Store.
// workspace creates the workspace name of type root:<typeName> under parent
// and returns its path and cluster name.
func (s *IntegrationSuite) workspace(t *testing.T, parent logicalcluster.Path, name, typeName string) (logicalcluster.Path, string) {
	t.Helper()
	path, err := s.env.CreateWorkspace(t.Context(), parent.Join(name).String(),
		kcptc.With(kcptesting.WithType(core.RootCluster.Path(), kcptenancyv1alpha1.WorkspaceTypeName(typeName))), kcptc.WaitForReachable())
	require.NoError(t, err)
	ws := &kcptenancyv1alpha1.Workspace{}
	require.NoError(t, s.platform.Cluster.Cluster(parent).Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: name}, ws))
	return path, ws.Spec.Cluster
}

func (s *IntegrationSuite) createAccountInfo(t *testing.T, cl ctrlruntimeclient.Client, accountName, orgName, orgClusterID string, accountPath, orgPath logicalcluster.Path) {
	accountInfo := &pmcorev1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account"},
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
				Store: pmcorev1alpha1.StoreInfo{Id: "test-store-id"},
			},
		},
	}
	if err := cl.Create(t.Context(), accountInfo); err != nil && !apierrors.IsAlreadyExists(err) {
		s.Require().NoError(err)
	}
	t.Logf("created accountInfo 'account' in %s workspace", accountPath)
}
