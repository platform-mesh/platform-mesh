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
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	crcluster "sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/stretchr/testify/require"
	"go.platform-mesh.io/golang-commons/logger"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"go.platform-mesh.io/account-operator/internal/config"
	"go.platform-mesh.io/account-operator/internal/controller"
	"go.platform-mesh.io/account-operator/pkg/subroutines/finalizeaccountinfo"
	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
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
	require.NoError(t, corev1alpha1.AddToScheme(scheme))

	accountInfo := &corev1alpha1.AccountInfo{}
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

	updated := &corev1alpha1.AccountInfo{}
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: accountInfo.Name}, updated))
	require.Contains(t, updated.Finalizers, finalizeaccountinfo.AccountInfoFinalizer)
}

func TestAccountInfoReconciler_ReconcileSkipsFinalizerWhenControllerDisabled(
	t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1alpha1.AddToScheme(scheme))

	accountInfo := &corev1alpha1.AccountInfo{}
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

	updated := &corev1alpha1.AccountInfo{}
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
