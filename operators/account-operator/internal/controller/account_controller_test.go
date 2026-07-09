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

package controller_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"go.platform-mesh.io/account-operator/internal/config"
	"go.platform-mesh.io/account-operator/internal/controller"
	"go.platform-mesh.io/account-operator/pkg/subroutines/manageaccountinfo"
	"go.platform-mesh.io/account-operator/pkg/subroutines/workspace"
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	platformmeshconfig "go.platform-mesh.io/golang-commons/config"
	platformmeshcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/logger"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/kcp-dev/logicalcluster/v3"
	mcc "github.com/kcp-dev/multicluster-provider/client"
	mcenvtest "github.com/kcp-dev/multicluster-provider/envtest"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	"github.com/kcp-dev/sdk/apis/core"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"

	_ "embed"
)

const (
	defaultTestTimeout  = 15 * time.Second
	defaultTickInterval = 250 * time.Millisecond
	defaultNamespace    = "default"
)

type AccountTestSuite struct {
	suite.Suite

	env                 *mcenvtest.Environment
	kcpClient           mcc.ClusterClient
	kcpConfig           *rest.Config
	mgr                 mcmanager.Manager
	scheme              *runtime.Scheme
	platformMeshSysPath logicalcluster.Path
	orgsClusterPath     logicalcluster.Path

	rootClient            ctrlruntimeclient.Client
	rootOrgsClient        ctrlruntimeclient.Client
	rootOrgsDefaultClient ctrlruntimeclient.Client

	logger *logger.Logger
	ctx    context.Context
	cancel context.CancelCauseFunc
}

func TestAccountTestSuite(t *testing.T) {
	suite.Run(t, new(AccountTestSuite))
}

func (s *AccountTestSuite) SetupSuite() {
	logConfig := logger.DefaultConfig()
	logConfig.NoJSON = true
	logConfig.Name = "AccountTestSuite"
	logConfig.Level = "debug"

	logger, err := logger.New(logConfig)
	s.Require().NoError(err)
	ctrl.SetLogger(logger.Logr())
	s.logger = logger
	s.ctx, s.cancel, _ = platformmeshcontext.StartContext(logger, nil, 0)

	s.scheme = runtime.NewScheme()
	utilruntime.Must(pmcorev1alpha1.AddToScheme(s.scheme))
	utilruntime.Must(corev1.AddToScheme(s.scheme))
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(s.scheme))
	utilruntime.Must(kcpapisv1alpha2.AddToScheme(s.scheme))
	utilruntime.Must(kcpcorev1alpha1.AddToScheme(s.scheme))
	utilruntime.Must(kcptenancyv1alpha1.AddToScheme(s.scheme))

	s.setupKCP()
	s.setupManager()

	// Setup account reconciler and dependencies
	cfg := config.OperatorConfig{}
	cfg.Subroutines.Workspace.Enabled = true
	cfg.Subroutines.AccountInfo.Enabled = true
	cfg.Subroutines.WorkspaceType.Enabled = true
	cfg.Kcp.ProviderWorkspace = core.RootCluster.Path().String()
	dCfg := &platformmeshconfig.CommonServiceConfig{}
	accountReconciler, err := controller.NewAccountReconciler(logger, s.mgr, cfg)
	s.Require().NoError(err)
	s.Require().NoError(accountReconciler.SetupWithManager(s.mgr, dCfg, logger))
	s.startManager()

	s.setupDefaultOrg()
}

func (s *AccountTestSuite) TearDownSuite() {
	if err := s.env.Stop(); err != nil {
		s.T().Logf("Error stopping kcp environment: %v", err)
	}
	s.cancel(fmt.Errorf("tearing down test suite"))
}

func (s *AccountTestSuite) TestAddingFinalizer() {
	testContext := context.Background()
	accountName := "test-account-finalizer"

	account := &pmcorev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: accountName,
		},
		Spec: pmcorev1alpha1.AccountSpec{
			Type: pmcorev1alpha1.AccountTypeOrg,
		},
	}

	s.Require().NoError(s.rootOrgsClient.Create(testContext, account))

	createdAccount := pmcorev1alpha1.Account{}
	s.Assert().Eventually(func() bool {
		err := s.rootOrgsClient.Get(testContext, types.NamespacedName{Name: accountName, Namespace: defaultNamespace}, &createdAccount)

		return err == nil && len(createdAccount.Finalizers) == 2
	}, defaultTestTimeout*2, defaultTickInterval)

	s.ElementsMatch([]string{"workspacetype.core.platform-mesh.io/finalizer", "account.core.platform-mesh.io/finalizer"}, createdAccount.Finalizers)
}

func (s *AccountTestSuite) TestWorkspaceCreation() {
	testContext := context.Background()
	accountName := "test-account-ws-creation"
	account := &pmcorev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: accountName}, Spec: pmcorev1alpha1.AccountSpec{Type: pmcorev1alpha1.AccountTypeAccount}}

	s.Require().NoError(s.rootOrgsDefaultClient.Create(testContext, account))

	createdWorkspace := kcptenancyv1alpha1.Workspace{}
	s.Assert().Eventually(func() bool {
		if err := s.rootOrgsDefaultClient.Get(testContext, types.NamespacedName{Name: accountName}, &createdWorkspace); err != nil {
			return false
		}
		return createdWorkspace.Status.Phase == kcpcorev1alpha1.LogicalClusterPhaseReady
	}, defaultTestTimeout, defaultTickInterval)

	updatedAccount := &pmcorev1alpha1.Account{}
	s.Assert().Eventually(func() bool {
		if err := s.rootOrgsDefaultClient.Get(testContext, types.NamespacedName{Name: accountName}, updatedAccount); err != nil {
			return false
		}
		return meta.IsStatusConditionTrue(updatedAccount.Status.Conditions, workspace.WorkspaceSubroutineName)
	}, defaultTestTimeout, defaultTickInterval)

	s.verifyWorkspace(testContext, "default", accountName)
	s.verifyCondition(updatedAccount.Status.Conditions, workspace.WorkspaceSubroutineName, metav1.ConditionTrue, "Complete")
}

func (s *AccountTestSuite) TestAccountInfoCreationForOrganization() {
	testContext := context.Background()
	accountName := "test-org-account"
	account := &pmcorev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: accountName}, Spec: pmcorev1alpha1.AccountSpec{Type: pmcorev1alpha1.AccountTypeOrg}}

	s.Require().NoError(s.rootOrgsClient.Create(testContext, account))

	createdAccount := &pmcorev1alpha1.Account{}
	s.Assert().Eventually(func() bool {
		if err := s.rootOrgsClient.Get(testContext, types.NamespacedName{Name: accountName}, createdAccount); err != nil {
			return false
		}
		return meta.IsStatusConditionTrue(createdAccount.Status.Conditions, manageaccountinfo.ManageAccountInfoSubroutineName)
	}, defaultTestTimeout, defaultTickInterval)

	accountInfo := &pmcorev1alpha1.AccountInfo{}
	s.Assert().Eventually(func() bool {
		if err := s.rootOrgsDefaultClient.Get(testContext, ctrlruntimeclient.ObjectKey{Name: manageaccountinfo.DefaultAccountInfoName}, accountInfo); err != nil {
			return false
		}
		return accountInfo.Spec.Account.Type == pmcorev1alpha1.AccountTypeOrg
	}, defaultTestTimeout, defaultTickInterval)
}

func (s *AccountTestSuite) TestWorkspaceFinalizerRemovesWorkspace() {
	accountName := "test-workspace-finalizer"
	account := &pmcorev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: accountName}, Spec: pmcorev1alpha1.AccountSpec{Type: pmcorev1alpha1.AccountTypeAccount}}

	s.Require().NoError(s.rootOrgsDefaultClient.Create(s.ctx, account))

	s.Assert().Eventually(func() bool {
		createdWorkspace := kcptenancyv1alpha1.Workspace{}
		if err := s.rootOrgsDefaultClient.Get(s.ctx, types.NamespacedName{Name: accountName}, &createdWorkspace); err != nil && !apierrors.IsNotFound(err) {
			s.logger.Err(err).Msg("Waiting for Workspace to be created")
			return false
		} else if apierrors.IsNotFound(err) {
			s.logger.Info().Msg("Waiting for Workspace to be created")
			return false
		}

		return true
	}, defaultTestTimeout, defaultTickInterval)

	s.Assert().Eventually(func() bool {
		createdAccount := pmcorev1alpha1.Account{}
		if err := s.rootOrgsDefaultClient.Get(s.ctx, types.NamespacedName{Name: accountName}, &createdAccount); err != nil {
			s.logger.Err(err).Msg("Waiting for Account to be ready")
			return false
		}

		return meta.IsStatusConditionPresentAndEqual(createdAccount.GetConditions(), "Ready", metav1.ConditionTrue)
	}, defaultTestTimeout, defaultTickInterval)

	s.Require().NoError(s.rootOrgsDefaultClient.Delete(s.ctx, account))

	s.Assert().Eventually(func() bool {
		if err := s.rootOrgsDefaultClient.Get(s.ctx, types.NamespacedName{Name: accountName}, &kcptenancyv1alpha1.Workspace{}); err != nil && !apierrors.IsNotFound(err) {
			s.logger.Err(err).Msg("Waiting for Workspace to be deleted")
			return false
		} else if err == nil {
			s.logger.Info().Msg("Waiting for Workspace to be deleted")
			return false
		}

		return true
	}, defaultTestTimeout, defaultTickInterval)
}

func (s *AccountTestSuite) verifyWorkspace(ctx context.Context, orgName, accountName string) {
	workspace := &kcptenancyv1alpha1.Workspace{}
	s.Require().NoError(s.rootOrgsDefaultClient.Get(ctx, types.NamespacedName{Name: accountName}, workspace))
	s.Equal(accountName, workspace.Name)
	s.NotNil(workspace.Spec.Type)
	expectedType := kcptenancyv1alpha1.WorkspaceTypeName(fmt.Sprintf("%s-%s", orgName, pmcorev1alpha1.AccountTypeAccount))
	s.Equal(expectedType, workspace.Spec.Type.Name)
}

func (s *AccountTestSuite) verifyCondition(conditions []metav1.Condition, conditionType string, status metav1.ConditionStatus, reason string) {
	s.Assert().True(meta.IsStatusConditionPresentAndEqual(conditions, conditionType, status))
	condition := meta.FindStatusCondition(conditions, conditionType)
	s.Require().NotNil(condition)
	s.Equal(reason, condition.Reason)
}
