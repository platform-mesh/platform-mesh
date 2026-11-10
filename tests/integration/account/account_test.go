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

package account

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/account-operator/pkg/app"
	"go.platform-mesh.io/account-operator/pkg/config"
	"go.platform-mesh.io/account-operator/pkg/subroutines/manageaccountinfo"
	"go.platform-mesh.io/account-operator/pkg/subroutines/workspace"
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	accountresources "go.platform-mesh.io/apis/static-assets/resources/account-operator"
	platformmeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh/tests/framework/fixtures"
	"go.platform-mesh.io/platform-mesh/tests/framework/kcp"
	"go.platform-mesh.io/platform-mesh/tests/framework/manager"
	"go.platform-mesh.io/platform-mesh/tests/framework/scheme"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/yaml"

	kcptc "github.com/kcp-dev/kcp-testcontainers"
	"github.com/kcp-dev/multicluster-provider/apiexport"
	pathaware "github.com/kcp-dev/multicluster-provider/path-aware"
	"github.com/kcp-dev/sdk/apis/core"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"

	_ "embed"
)

const (
	defaultTestTimeout  = 15 * time.Second
	defaultTickInterval = 250 * time.Millisecond
	defaultOrg          = "default"
	coreExport          = "core.platform-mesh.io"
)

// defaultOrgYAML is the "default" org Account the scenarios create their
// accounts in.
//
//go:embed testdata/account-root-org.yaml
var defaultOrgYAML []byte

// env is what the scenarios share: the orgs workspace and the workspace of
// the default org.
type env struct {
	orgs, defaultOrg ctrlruntimeclient.Client
}

func TestAccount(t *testing.T) {
	e := setup(t)

	t.Run("AddingFinalizer", func(t *testing.T) { testAddingFinalizer(t, e) })
	t.Run("WorkspaceCreation", func(t *testing.T) { testWorkspaceCreation(t, e) })
	t.Run("AccountInfoCreationForOrganization", func(t *testing.T) { testAccountInfoCreationForOrganization(t, e) })
	t.Run("WorkspaceFinalizerRemovesWorkspace", func(t *testing.T) { testWorkspaceFinalizerRemovesWorkspace(t, e) })
}

// setup builds the platform-mesh-system tree, runs the account-operator
// against it the way cmd/ does (path-aware provider on
// :root:platform-mesh-system, app.SetupControllers) and creates the default
// org.
func setup(t *testing.T) *env {
	t.Helper()
	kcpEnv, err := kcp.Default(t.Context())
	require.NoError(t, err)
	s := scheme.New(t, pmcorev1alpha1.AddToScheme)

	p := fixtures.Install(t, kcpEnv,
		fixtures.WithScheme(s),
		fixtures.WithResources(accountresources.Resources, "*.yaml"),
		// Bound in platform-mesh-system so kcp serves the export's endpoint
		// slice the operator's provider watches.
		fixtures.WithBindings(coreExport),
		fixtures.WithOrgs(),
	)

	logConfig := logger.DefaultConfig()
	logConfig.NoJSON = true
	logConfig.Name = "account-operator"
	logConfig.Level = "debug"
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	mgr := newManager(t, kcpEnv, p, s, log)

	cfg := config.OperatorConfig{}
	cfg.Subroutines.Workspace.Enabled = true
	cfg.Subroutines.AccountInfo.Enabled = true
	cfg.Subroutines.WorkspaceType.Enabled = true
	cfg.Kcp.ProviderWorkspace = core.RootCluster.Path().String()
	require.NoError(t, app.SetupControllers(mgr, cfg, app.Deps{Log: log, Common: &platformmeshconfig.CommonServiceConfig{}}))
	manager.Start(t, mgr)

	acc := &pmcorev1alpha1.Account{}
	require.NoError(t, yaml.Unmarshal(defaultOrgYAML, acc))
	require.NoError(t, p.OrgsClient.Create(t.Context(), acc))
	// The account controller creates the org's workspace.
	wait.WorkspaceReady(t, p.OrgsClient, defaultOrg)

	defaultOrgClient, err := kcpEnv.Client(p.Orgs.Join(defaultOrg).String(), ctrlruntimeclient.Options{Scheme: s})
	require.NoError(t, err)
	return &env{
		orgs:       p.OrgsClient,
		defaultOrg: defaultOrgClient,
	}
}

// newManager builds the manager as cmd/ does: golang-commons' path-aware
// provider over the core.platform-mesh.io endpoint slice in
// platform-mesh-system, rather than framework/manager's apiexport providers.
func newManager(t *testing.T, kcpEnv *kcptc.Instance, p *fixtures.Platform, s *runtime.Scheme, log *logger.Logger) mcmanager.Manager {
	t.Helper()
	cfg := kcpEnv.RESTConfig(p.System.String())

	providerLog := log.ComponentLogger("apiexport_provider").Logr()
	provider, err := pathaware.New(cfg, coreExport, apiexport.Options{
		Scheme: s,
		Log:    &providerLog,
	})
	require.NoError(t, err)

	mgr, err := mcmanager.New(cfg, provider, mcmanager.Options{
		Scheme:      s,
		Metrics:     metricsserver.Options{BindAddress: "0"},
		BaseContext: t.Context,
	})
	require.NoError(t, err)
	return mgr
}

func testAddingFinalizer(t *testing.T, e *env) {
	ctx := t.Context()
	accountName := "test-account-finalizer"
	account := &pmcorev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: accountName},
		Spec:       pmcorev1alpha1.AccountSpec{Type: pmcorev1alpha1.AccountTypeOrg},
	}
	require.NoError(t, e.orgs.Create(ctx, account))

	created := &pmcorev1alpha1.Account{}
	wait.Until(t, func() bool {
		err := e.orgs.Get(ctx, ctrlruntimeclient.ObjectKey{Name: accountName}, created)
		return err == nil && len(created.Finalizers) == 2
	}, defaultTestTimeout*2, defaultTickInterval, "condition not met")

	assert.ElementsMatch(t, []string{"workspacetype.core.platform-mesh.io/finalizer", "account.core.platform-mesh.io/finalizer"}, created.Finalizers)
}

func testWorkspaceCreation(t *testing.T, e *env) {
	ctx := t.Context()
	accountName := "test-account-ws-creation"
	account := &pmcorev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: accountName},
		Spec:       pmcorev1alpha1.AccountSpec{Type: pmcorev1alpha1.AccountTypeAccount},
	}
	require.NoError(t, e.defaultOrg.Create(ctx, account))

	wait.Until(t, func() bool {
		ws := &kcptenancyv1alpha1.Workspace{}
		if err := e.defaultOrg.Get(ctx, ctrlruntimeclient.ObjectKey{Name: accountName}, ws); err != nil {
			return false
		}
		return ws.Status.Phase == kcpcorev1alpha1.LogicalClusterPhaseReady
	}, defaultTestTimeout, defaultTickInterval, "condition not met")

	updated := &pmcorev1alpha1.Account{}
	wait.Until(t, func() bool {
		if err := e.defaultOrg.Get(ctx, ctrlruntimeclient.ObjectKey{Name: accountName}, updated); err != nil {
			return false
		}
		return meta.IsStatusConditionTrue(updated.Status.Conditions, workspace.WorkspaceSubroutineName)
	}, defaultTestTimeout, defaultTickInterval, "condition not met")

	ws := &kcptenancyv1alpha1.Workspace{}
	require.NoError(t, e.defaultOrg.Get(ctx, ctrlruntimeclient.ObjectKey{Name: accountName}, ws))
	assert.Equal(t, accountName, ws.Name)
	require.NotNil(t, ws.Spec.Type)
	assert.Equal(t, kcptenancyv1alpha1.WorkspaceTypeName(fmt.Sprintf("%s-%s", defaultOrg, pmcorev1alpha1.AccountTypeAccount)), ws.Spec.Type.Name)

	assert.True(t, meta.IsStatusConditionPresentAndEqual(updated.Status.Conditions, workspace.WorkspaceSubroutineName, metav1.ConditionTrue))
	cond := meta.FindStatusCondition(updated.Status.Conditions, workspace.WorkspaceSubroutineName)
	require.NotNil(t, cond)
	assert.Equal(t, "Complete", cond.Reason)
}

func testAccountInfoCreationForOrganization(t *testing.T, e *env) {
	ctx := t.Context()
	accountName := "test-org-account"
	account := &pmcorev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: accountName},
		Spec:       pmcorev1alpha1.AccountSpec{Type: pmcorev1alpha1.AccountTypeOrg},
	}
	require.NoError(t, e.orgs.Create(ctx, account))

	wait.Until(t, func() bool {
		created := &pmcorev1alpha1.Account{}
		if err := e.orgs.Get(ctx, ctrlruntimeclient.ObjectKey{Name: accountName}, created); err != nil {
			return false
		}
		return meta.IsStatusConditionTrue(created.Status.Conditions, manageaccountinfo.ManageAccountInfoSubroutineName)
	}, defaultTestTimeout, defaultTickInterval, "condition not met")

	// As in the original suite, this reads the AccountInfo of the default
	// org's workspace.
	wait.Until(t, func() bool {
		info := &pmcorev1alpha1.AccountInfo{}
		if err := e.defaultOrg.Get(ctx, ctrlruntimeclient.ObjectKey{Name: manageaccountinfo.DefaultAccountInfoName}, info); err != nil {
			return false
		}
		return info.Spec.Account.Type == pmcorev1alpha1.AccountTypeOrg
	}, defaultTestTimeout, defaultTickInterval, "condition not met")
}

func testWorkspaceFinalizerRemovesWorkspace(t *testing.T, e *env) {
	ctx := t.Context()
	accountName := "test-workspace-finalizer"
	account := &pmcorev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: accountName},
		Spec:       pmcorev1alpha1.AccountSpec{Type: pmcorev1alpha1.AccountTypeAccount},
	}
	require.NoError(t, e.defaultOrg.Create(ctx, account))

	wait.Eventually(t, func() (bool, string) {
		err := e.defaultOrg.Get(ctx, ctrlruntimeclient.ObjectKey{Name: accountName}, &kcptenancyv1alpha1.Workspace{})
		if err != nil {
			return false, err.Error()
		}
		return true, ""
	}, defaultTestTimeout, defaultTickInterval, "Workspace %s should be created", accountName)

	wait.Eventually(t, func() (bool, string) {
		created := &pmcorev1alpha1.Account{}
		if err := e.defaultOrg.Get(ctx, ctrlruntimeclient.ObjectKey{Name: accountName}, created); err != nil {
			return false, err.Error()
		}
		return meta.IsStatusConditionPresentAndEqual(created.GetConditions(), "Ready", metav1.ConditionTrue), "Account not Ready"
	}, defaultTestTimeout, defaultTickInterval, "Account %s should be Ready", accountName)

	require.NoError(t, e.defaultOrg.Delete(ctx, account))

	wait.Eventually(t, func() (bool, string) {
		err := e.defaultOrg.Get(ctx, ctrlruntimeclient.ObjectKey{Name: accountName}, &kcptenancyv1alpha1.Workspace{})
		switch {
		case apierrors.IsNotFound(err):
			return true, ""
		case err != nil:
			return false, err.Error()
		default:
			return false, "Workspace still exists"
		}
	}, defaultTestTimeout, defaultTickInterval, "Workspace %s should be deleted", accountName)
}
