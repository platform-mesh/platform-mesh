package controller_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	kcpcorev1alpha "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	apiexport "github.com/kcp-dev/multicluster-provider/apiexport"
	platformmeshconfig "github.com/platform-mesh/golang-commons/config"
	platformmeshcontext "github.com/platform-mesh/golang-commons/context"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/internal/controller"
	"github.com/platform-mesh/account-operator/pkg/subroutines/accountinfo"
	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
	"github.com/platform-mesh/account-operator/pkg/testing/kcpenvtest"
)

const (
	defaultTestTimeout  = 15 * time.Second
	defaultTickInterval = 250 * time.Millisecond
	defaultNamespace    = "default"
)

type AccountTestSuite struct {
	suite.Suite

	kubernetesClient    client.Client
	multiClusterManager mcmanager.Manager
	provider            *apiexport.Provider
	testEnv             *kcpenvtest.Environment
	log                 *logger.Logger
	cancel              context.CancelCauseFunc
	rootConfig          *rest.Config
	scheme              *runtime.Scheme
}

func TestAccountTestSuite(t *testing.T) {
	suite.Run(t, new(AccountTestSuite))
}

func buildOrgsClient(mgr mcmanager.Manager) (client.Client, error) {
	cfg := rest.CopyConfig(mgr.GetLocalManager().GetConfig())

	parsed, err := url.Parse(cfg.Host)
	if err != nil {
		return nil, err
	}

	parsed.Path = "/clusters/root:orgs"

	cfg.Host = parsed.String()

	return client.New(cfg, client.Options{
		Scheme: mgr.GetLocalManager().GetScheme(),
	})
}

func (suite *AccountTestSuite) SetupSuite() {
	logConfig := logger.DefaultConfig()
	logConfig.NoJSON = true
	logConfig.Name = "AccountTestSuite"
	logConfig.Level = "debug"

	log, err := logger.New(logConfig)
	suite.Require().NoError(err)
	suite.log = log

	cfg := config.OperatorConfig{}
	cfg.Subroutines.FGA.Enabled = false
	cfg.Subroutines.Workspace.Enabled = true
	cfg.Subroutines.AccountInfo.Enabled = true
	cfg.Subroutines.WorkspaceType.Enabled = true
	cfg.Kcp.ProviderWorkspace = "root"

	testContext, cancel, _ := platformmeshcontext.StartContext(log, cfg, 2*time.Minute)
	suite.cancel = cancel

	testEnvLogger := log.ComponentLogger("kcpenvtest")

	useExistingCluster := true
	if envValue, err := strconv.ParseBool(os.Getenv("USE_EXISTING_CLUSTER")); err == nil {
		useExistingCluster = envValue
	}

	suite.testEnv = kcpenvtest.NewEnvironment("core.platform-mesh.io", "platform-mesh-system", "../../", "bin", "test/setup", useExistingCluster, testEnvLogger)
	suite.testEnv.ControlPlaneStartTimeout = 2 * time.Minute
	suite.testEnv.ControlPlaneStopTimeout = 30 * time.Second

	k8sCfg, vsURL, err := suite.testEnv.Start()
	if err != nil {
		_ = suite.testEnv.Stop(useExistingCluster)
		suite.T().Skipf("skipping account controller suite: unable to start KCP: %v", err)
	}
	suite.rootConfig = k8sCfg

	suite.scheme = runtime.NewScheme()
	utilruntime.Must(v1alpha1.AddToScheme(suite.scheme))
	utilruntime.Must(v1.AddToScheme(suite.scheme))
	utilruntime.Must(kcpcorev1alpha.AddToScheme(suite.scheme))
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(suite.scheme))

	providerCfg := rest.CopyConfig(suite.rootConfig)
	providerCfg.Host = vsURL

	suite.provider, err = apiexport.New(providerCfg, apiexport.Options{Scheme: suite.scheme})
	suite.Require().NoError(err)

	mcOpts := mcmanager.Options{
		Scheme: suite.scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		BaseContext: func() context.Context { return testContext },
	}

	suite.multiClusterManager, err = mcmanager.New(providerCfg, suite.provider, mcOpts)
	suite.Require().NoError(err)

	orgsClient, err := buildOrgsClient(suite.multiClusterManager)
	suite.Require().NoError(err)

	mockClient := mocks.NewOpenFGAServiceClient(suite.T())
	accountReconciler := controller.NewAccountReconciler(log, suite.multiClusterManager, cfg, orgsClient, mockClient)

	dCfg := &platformmeshconfig.CommonServiceConfig{}
	suite.Require().NoError(accountReconciler.SetupWithManager(suite.multiClusterManager, dCfg, log))

	go suite.startController(testContext)

	// Client targeting orgs workspace for assertions
	testDataConfig := rest.CopyConfig(suite.rootConfig)
	testDataConfig.Host = fmt.Sprintf("%s:%s", suite.rootConfig.Host, "orgs:root-org")

	suite.kubernetesClient, err = client.New(testDataConfig, client.Options{Scheme: suite.scheme})
	suite.Require().NoError(err)

	suite.Require().NoError(suite.testEnv.WaitForWorkspaceWithTimeout(orgsClient, "root-org", testEnvLogger, time.Minute))
}

func (suite *AccountTestSuite) TearDownSuite() {
	if suite.cancel != nil {
		suite.cancel(fmt.Errorf("tearing down test suite"))
	}

	useExistingCluster := true
	if envValue, err := strconv.ParseBool(os.Getenv("USE_EXISTING_CLUSTER")); err == nil {
		useExistingCluster = envValue
	}
	if suite.testEnv != nil {
		_ = suite.testEnv.Stop(useExistingCluster)
	}
}

func (suite *AccountTestSuite) startController(ctx context.Context) {
	go func() {
		_ = suite.provider.Run(ctx, suite.multiClusterManager)
	}()
	suite.Require().NoError(suite.multiClusterManager.Start(ctx))
}

func (suite *AccountTestSuite) TestAddingFinalizer() {
	testContext := context.Background()
	accountName := "test-account-finalizer"

	account := &v1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: accountName,
		},
		Spec: v1alpha1.AccountSpec{
			Type: v1alpha1.AccountTypeAccount,
		},
	}

	suite.Require().NoError(suite.kubernetesClient.Create(testContext, account))

	createdAccount := v1alpha1.Account{}
	suite.Assert().Eventually(func() bool {
		err := suite.kubernetesClient.Get(testContext, types.NamespacedName{Name: accountName, Namespace: defaultNamespace}, &createdAccount)
		return err == nil && len(createdAccount.Finalizers) == 3
	}, defaultTestTimeout, defaultTickInterval)

	suite.ElementsMatch([]string{"workspacetype.core.platform-mesh.io/finalizer", "account.core.platform-mesh.io/finalizer", "account.core.platform-mesh.io/info"}, createdAccount.Finalizers)
}

func (suite *AccountTestSuite) TestWorkspaceCreation() {
	testContext := context.Background()
	accountName := "test-account-ws-creation"
	account := &v1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: accountName}, Spec: v1alpha1.AccountSpec{Type: v1alpha1.AccountTypeAccount}}

	suite.Require().NoError(suite.kubernetesClient.Create(testContext, account))

	createdWorkspace := kcptenancyv1alpha.Workspace{}
	suite.Assert().Eventually(func() bool {
		if err := suite.kubernetesClient.Get(testContext, types.NamespacedName{Name: accountName}, &createdWorkspace); err != nil {
			return false
		}
		return createdWorkspace.Status.Phase == kcpcorev1alpha.LogicalClusterPhaseReady
	}, defaultTestTimeout, defaultTickInterval)

	updatedAccount := &v1alpha1.Account{}
	suite.Assert().Eventually(func() bool {
		if err := suite.kubernetesClient.Get(testContext, types.NamespacedName{Name: accountName}, updatedAccount); err != nil {
			return false
		}
		return meta.IsStatusConditionTrue(updatedAccount.Status.Conditions, "WorkspaceSubroutine_Ready")
	}, defaultTestTimeout, defaultTickInterval)

	suite.verifyWorkspace(testContext, accountName)
	suite.verifyCondition(updatedAccount.Status.Conditions, "WorkspaceSubroutine_Ready", metav1.ConditionTrue, "Complete")
}

func (suite *AccountTestSuite) TestAccountInfoCreationForOrganization() {
	testContext := context.Background()
	accountName := "test-org-account"
	account := &v1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: accountName}, Spec: v1alpha1.AccountSpec{Type: v1alpha1.AccountTypeOrg}}

	suite.Require().NoError(suite.kubernetesClient.Create(testContext, account))

	createdAccount := &v1alpha1.Account{}
	suite.Assert().Eventually(func() bool {
		if err := suite.kubernetesClient.Get(testContext, types.NamespacedName{Name: accountName}, createdAccount); err != nil {
			return false
		}
		return meta.IsStatusConditionTrue(createdAccount.Status.Conditions, "AccountInfoSubroutine_Ready")
	}, defaultTestTimeout, defaultTickInterval)

	accountInfo := &v1alpha1.AccountInfo{}
	suite.Assert().Eventually(func() bool {
		if err := suite.kubernetesClient.Get(testContext, client.ObjectKey{Name: accountinfo.DefaultAccountInfoName}, accountInfo); err != nil {
			return false
		}
		return accountInfo.Spec.Account.Type == v1alpha1.AccountTypeOrg
	}, defaultTestTimeout, defaultTickInterval)
}

func (suite *AccountTestSuite) TestWorkspaceFinalizerRemovesWorkspace() {
	testContext := context.Background()
	accountName := "test-workspace-finalizer"
	account := &v1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: accountName}, Spec: v1alpha1.AccountSpec{Type: v1alpha1.AccountTypeAccount}}

	suite.Require().NoError(suite.kubernetesClient.Create(testContext, account))

	createdWorkspace := kcptenancyv1alpha.Workspace{}
	suite.Assert().Eventually(func() bool {
		return suite.kubernetesClient.Get(testContext, types.NamespacedName{Name: accountName}, &createdWorkspace) == nil
	}, defaultTestTimeout, defaultTickInterval)

	suite.Require().NoError(suite.kubernetesClient.Delete(testContext, account))

	suite.Assert().Eventually(func() bool {
		err := suite.kubernetesClient.Get(testContext, types.NamespacedName{Name: accountName}, &kcptenancyv1alpha.Workspace{})
		return kerrors.IsNotFound(err)
	}, defaultTestTimeout, defaultTickInterval)
}

func (suite *AccountTestSuite) verifyWorkspace(ctx context.Context, accountName string) {
	workspace := &kcptenancyv1alpha.Workspace{}
	suite.Require().NoError(suite.kubernetesClient.Get(ctx, types.NamespacedName{Name: accountName}, workspace))
	suite.Equal(accountName, workspace.Name)
	suite.NotNil(workspace.Spec.Type)
	expectedType := kcptenancyv1alpha.WorkspaceTypeName(fmt.Sprintf("%s-acc", accountName))
	suite.Equal(expectedType, workspace.Spec.Type.Name)
}

func (suite *AccountTestSuite) verifyCondition(conditions []metav1.Condition, conditionType string, status metav1.ConditionStatus, reason string) {
	suite.Assert().True(meta.IsStatusConditionPresentAndEqual(conditions, conditionType, status))
	condition := meta.FindStatusCondition(conditions, conditionType)
	suite.Require().NotNil(condition)
	suite.Equal(reason, condition.Reason)
}
