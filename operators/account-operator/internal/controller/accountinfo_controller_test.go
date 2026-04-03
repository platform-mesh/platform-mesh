package controller_test

import (
	"context"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	crcluster "sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/platform-mesh/golang-commons/logger"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/internal/controller"
	"github.com/platform-mesh/account-operator/pkg/subroutines/finalizeaccountinfo"
)

type stubManager struct {
	mcmanager.Manager
	cluster crcluster.Cluster
	err     error
}

func (m *stubManager) ClusterFromContext(_ context.Context) (crcluster.Cluster, error) {
	if m.err != nil {
		return nil, m.err
	}

	return m.cluster, nil
}

type stubCluster struct {
	crcluster.Cluster
	client client.Client
}

func (c *stubCluster) GetClient() client.Client {
	return c.client
}

func TestAccountInfoReconciler_ReconcileAddsFinalizerWhenControllerEnabled(
	t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))

	accountInfo := &v1alpha1.AccountInfo{}
	accountInfo.Name = "account-info"

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(accountInfo).
		Build()

	cfg := config.NewOperatorConfig()
	cfg.Controllers.AccountInfo.Enabled = true

	log := testLogger(t)
	mgr := &stubManager{
		cluster: &stubCluster{client: cl},
	}

	reconciler, err := controller.NewAccountInfoReconciler(log, mgr, cfg)
	require.NoError(t, err)

	_, err = reconciler.Reconcile(context.Background(), mcreconcile.Request{
		Request: reconcile.Request{
			NamespacedName: types.NamespacedName{Name: accountInfo.Name},
		},
	})
	require.NoError(t, err)

	updated := &v1alpha1.AccountInfo{}
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: accountInfo.Name}, updated))
	require.Contains(t, updated.Finalizers, finalizeaccountinfo.AccountInfoFinalizer)
}

func TestAccountInfoReconciler_ReconcileSkipsFinalizerWhenControllerDisabled(
	t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))

	accountInfo := &v1alpha1.AccountInfo{}
	accountInfo.Name = "account-info"

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(accountInfo).
		Build()

	cfg := config.NewOperatorConfig()
	cfg.Controllers.AccountInfo.Enabled = false

	log := testLogger(t)
	mgr := &stubManager{
		cluster: &stubCluster{client: cl},
	}

	reconciler, err := controller.NewAccountInfoReconciler(log, mgr, cfg)
	require.NoError(t, err)

	_, err = reconciler.Reconcile(context.Background(), mcreconcile.Request{
		Request: reconcile.Request{
			NamespacedName: types.NamespacedName{Name: accountInfo.Name},
		},
	})
	require.NoError(t, err)

	updated := &v1alpha1.AccountInfo{}
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: accountInfo.Name}, updated))
	require.Empty(t, updated.Finalizers)
}

func testLogger(t *testing.T) *logger.Logger {
	t.Helper()

	logCfg := logger.DefaultConfig()
	logCfg.NoJSON = true
	logCfg.Name = "accountinfo-controller-test"
	logCfg.Level = "debug"

	l, err := logger.New(logCfg)
	require.NoError(t, err)

	return l
}
