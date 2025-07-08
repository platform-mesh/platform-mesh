package controller_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	kcpcorev1alpha "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	platformmeshconfig "github.com/platform-mesh/golang-commons/config"
	platformmeshcontext "github.com/platform-mesh/golang-commons/context"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/kcp"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/internal/controller"
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

	kubernetesClient  client.Client
	kubernetesManager ctrl.Manager
	testEnv           *kcpenvtest.Environment
	log               *logger.Logger
	cancel            context.CancelCauseFunc
	rootConfig        *rest.Config
	scheme            *runtime.Scheme
}

func (suite *AccountTestSuite) SetupSuite() {
	logConfig := logger.DefaultConfig()
	logConfig.NoJSON = true
	logConfig.Name = "AccountTestSuite"
	logConfig.Level = "debug"

	log, err := logger.New(logConfig)
	suite.Require().NoError(err)
	suite.log = log
	ctrl.SetLogger(log.Logr())

	cfg := config.OperatorConfig{}
	cfg.Subroutines.FGA.Enabled = false
	cfg.Subroutines.Workspace.Enabled = true
	cfg.Subroutines.AccountInfo.Enabled = true
	cfg.Kcp.ProviderWorkspace = "root"
	suite.Require().NoError(err)

	testContext, cancel, _ := platformmeshcontext.StartContext(log, cfg, 1*time.Minute)
	suite.cancel = cancel

	testEnvLogger := log.ComponentLogger("kcpenvtest")

	useExistingCluster := true
	if envValue, err := strconv.ParseBool(os.Getenv("USE_EXISTING_CLUSTER")); err != nil {
		useExistingCluster = envValue
	}
	suite.testEnv = kcpenvtest.NewEnvironment("core.platform-mesh.io", "platform-mesh-system", "../../", "bin", "test/setup", useExistingCluster, testEnvLogger)
	k8sCfg, vsUrl, err := suite.testEnv.Start()
	if err != nil {
		stopErr := suite.testEnv.Stop(useExistingCluster)
		suite.Require().NoError(stopErr)
	}
	suite.Require().NoError(err)
	suite.Require().NotNil(k8sCfg)
	suite.Require().NotEmpty(vsUrl)
	suite.rootConfig = k8sCfg

	suite.scheme = runtime.NewScheme()
	utilruntime.Must(v1alpha1.AddToScheme(suite.scheme))
	utilruntime.Must(v1.AddToScheme(suite.scheme))
	utilruntime.Must(kcpcorev1alpha.AddToScheme(suite.scheme))
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(suite.scheme))

	managerCfg := rest.CopyConfig(suite.rootConfig)
	managerCfg.Host = vsUrl

	testDataConfig := rest.CopyConfig(suite.rootConfig)
	testDataConfig.Host = fmt.Sprintf("%s:%s", suite.rootConfig.Host, "orgs:root-org")

	// +kubebuilder:scaffold:scheme
	suite.kubernetesClient, err = client.New(testDataConfig, client.Options{
		Scheme: suite.scheme,
	})
	suite.Require().NoError(err)

	suite.kubernetesManager, err = kcp.NewClusterAwareManager(managerCfg, ctrl.Options{
		Scheme:      suite.scheme,
		Logger:      log.Logr(),
		BaseContext: func() context.Context { return testContext },
	})
	suite.Require().NoError(err)

	mockClient := mocks.NewOpenFGAServiceClient(suite.T())
	accountReconciler := controller.NewAccountReconciler(log, suite.kubernetesManager, cfg, mockClient)
	dCfg := &platformmeshconfig.CommonServiceConfig{}
	err = accountReconciler.SetupWithManager(suite.kubernetesManager, dCfg, log)
	suite.Require().NoError(err)

	go suite.startController(testContext)
}

func (suite *AccountTestSuite) TearDownSuite() {
	suite.cancel(fmt.Errorf("tearing down test suite"))
	useExistingCluster := true
	if envValue, err := strconv.ParseBool(os.Getenv("USE_EXISTING_CLUSTER")); err != nil {
		useExistingCluster = envValue
	}
	err := suite.testEnv.Stop(useExistingCluster)
	suite.Nil(err)
}

func (suite *AccountTestSuite) startController(ctx context.Context) {
	err := suite.kubernetesManager.Start(ctx)
	suite.Require().NoError(err)
}

func (suite *AccountTestSuite) TestAddingFinalizer() {
	// Given
	testContext := context.Background()
	accountName := "test-account-finalizer"

	account := &v1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: accountName,
		},
		Spec: v1alpha1.AccountSpec{
			Type: v1alpha1.AccountTypeAccount,
		}}

	// When
	err := suite.kubernetesClient.Create(testContext, account)
	suite.Nil(err)

	// Then
	createdAccount := v1alpha1.Account{}
	suite.Assert().Eventually(func() bool {
		err := suite.kubernetesClient.Get(testContext, types.NamespacedName{
			Name:      accountName,
			Namespace: defaultNamespace,
		}, &createdAccount)
		return err == nil && createdAccount.Finalizers != nil
	}, defaultTestTimeout, defaultTickInterval)

	suite.Equal([]string{"account.core.platform-mesh.io/finalizer", "account.core.platform-mesh.io/info"}, createdAccount.Finalizers)
}

func (suite *AccountTestSuite) TestWorkspaceCreation() {
	// Given
	var err error
	testContext := context.Background()
	accountName := "test-account-ws-creation"
	account := &v1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: accountName,
		},
		Spec: v1alpha1.AccountSpec{
			Type: v1alpha1.AccountTypeAccount,
		}}

	// When
	err = suite.kubernetesClient.Create(testContext, account)
	suite.Require().NoError(err)

	// Then

	// Wait for workspace creation and ready
	createdWorkspace := kcptenancyv1alpha.Workspace{}
	suite.Assert().Eventually(func() bool {
		err := suite.kubernetesClient.Get(testContext, types.NamespacedName{
			Name: accountName,
		}, &createdWorkspace)
		return err == nil && createdWorkspace.Status.Phase == kcpcorev1alpha.LogicalClusterPhaseReady
	}, defaultTestTimeout, defaultTickInterval)

	// Wait for conditions update on account
	updatedAccount := &v1alpha1.Account{}
	suite.Assert().Eventually(func() bool {
		err := suite.kubernetesClient.Get(testContext, types.NamespacedName{
			Name: accountName,
		}, updatedAccount)
		return err == nil && meta.IsStatusConditionTrue(updatedAccount.Status.Conditions, "WorkspaceSubroutine_Ready")
	}, defaultTestTimeout, defaultTickInterval)

	// Verify workspace and account conditions
	suite.verifyWorkspace(testContext, accountName)
	suite.verifyCondition(updatedAccount.Status.Conditions, "WorkspaceSubroutine_Ready", metav1.ConditionTrue, "Complete")
}

func (suite *AccountTestSuite) TestAccountInfoCreationForOrganization() {
	testContext := context.Background()

	// Then
	accountInfo := v1alpha1.AccountInfo{}
	suite.Assert().Eventually(func() bool {
		err := suite.kubernetesClient.Get(testContext, types.NamespacedName{
			Name: "account",
		}, &accountInfo)
		return err == nil
	}, defaultTestTimeout, defaultTickInterval)

	// Test if Workspace exists
	suite.NotNil(accountInfo.Spec.ClusterInfo.CA)
	suite.Equal("root-org", accountInfo.Spec.Account.Name)
	suite.NotNil(accountInfo.Spec.Account.URL)
	suite.Equal("root:orgs:root-org", accountInfo.Spec.Account.Path)
	suite.Equal("root-org", accountInfo.Spec.Organization.Name)
	suite.Equal("root-org", accountInfo.Spec.Organization.Name)
	suite.NotNil(accountInfo.Spec.Organization.URL)
	suite.Equal("root:orgs:root-org", accountInfo.Spec.Organization.Path)
	suite.Nil(accountInfo.Spec.ParentAccount)
}

func (suite *AccountTestSuite) TestAccountInfoCreationForAccount() {
	var err error
	testContext := context.Background()
	accountName := "test-account-account-info-creation1"
	account := &v1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: accountName,
		},
		Spec: v1alpha1.AccountSpec{
			Type: v1alpha1.AccountTypeAccount,
		}}

	// When
	err = suite.kubernetesClient.Create(testContext, account)
	suite.Require().NoError(err)

	// Then
	// Wait for Account to be ready
	updatedAccount := &v1alpha1.Account{}
	suite.Assert().Eventually(func() bool {
		err := suite.kubernetesClient.Get(testContext, types.NamespacedName{
			Name: accountName,
		}, updatedAccount)
		cond := meta.FindStatusCondition(updatedAccount.Status.Conditions, "Ready")
		return err == nil && cond != nil && cond.Status == metav1.ConditionTrue
	}, defaultTestTimeout, defaultTickInterval)

	// Retrieve account info from workspace
	testDataConfig := rest.CopyConfig(suite.rootConfig)
	testDataConfig.Host = fmt.Sprintf("%s:%s", suite.rootConfig.Host, "orgs:root-org:test-account-account-info-creation1")
	testClient, err := client.New(testDataConfig, client.Options{
		Scheme: suite.scheme,
	})
	suite.Require().NoError(err)

	accountInfo := v1alpha1.AccountInfo{}
	suite.Assert().Eventually(func() bool {
		err := testClient.Get(testContext, types.NamespacedName{
			Name: "account",
		}, &accountInfo)
		return err == nil
	}, defaultTestTimeout, defaultTickInterval)

	// Test if Workspace exists
	suite.NotNil(accountInfo.Spec.ClusterInfo.CA)
	// Account
	suite.Equal("test-account-account-info-creation1", accountInfo.Spec.Account.Name)
	suite.NotNil(accountInfo.Spec.Account.URL)
	suite.Equal("root:orgs:root-org:test-account-account-info-creation1", accountInfo.Spec.Account.Path)
	// Organization
	suite.Equal("root-org", accountInfo.Spec.Organization.Name)
	suite.Equal("root-org", accountInfo.Spec.Organization.Name)
	suite.NotNil(accountInfo.Spec.Organization.URL)
	// Parent Account
	suite.Require().NotNil(accountInfo.Spec.ParentAccount)
	suite.Equal("root:orgs:root-org", accountInfo.Spec.ParentAccount.Path)
	suite.Equal("root-org", accountInfo.Spec.ParentAccount.Name)
	suite.NotNil(accountInfo.Spec.ParentAccount.URL)

}

func (suite *AccountTestSuite) verifyWorkspace(ctx context.Context, name string) {

	suite.Require().NotNil(name, "failed to verify namespace name")
	ns := &kcptenancyv1alpha.Workspace{}
	err := suite.kubernetesClient.Get(ctx, types.NamespacedName{Name: name}, ns)
	suite.Nil(err)

	suite.Assert().Len(ns.GetOwnerReferences(), 1, "failed to verify owner reference on workspace")
}

func (suite *AccountTestSuite) verifyCondition(conditions []metav1.Condition, conditionType string, status metav1.ConditionStatus, reason string) {
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

func TestAccountTestSuite(t *testing.T) {
	suite.Run(t, new(AccountTestSuite))
}
