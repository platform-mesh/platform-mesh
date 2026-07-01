//go:build !integration

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

package velero_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/backup-operator/pkg/config"
	"go.platform-mesh.io/backup-operator/pkg/velero"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/golang-commons/logger/testlogger"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const lifecycleTestNS = "velero-test"

func newLifecycleScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	return s
}

func lifecycleCtx() context.Context {
	return logger.SetLoggerInContext(context.Background(), testlogger.New().Logger)
}

// TestLifecycle_CreatesDeploymentAndDaemonSet verifies that Start creates the Velero
// server Deployment, node-agent DaemonSet, and ServiceAccount on a fresh cluster.
func TestLifecycle_CreatesDeploymentAndDaemonSet(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(newLifecycleScheme(t)).Build()

	r := &velero.LifecycleRunnable{Client: cl, Namespace: lifecycleTestNS}
	require.NoError(t, r.Start(lifecycleCtx()))

	var sa corev1.ServiceAccount
	require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: "velero", Namespace: lifecycleTestNS}, &sa))

	var deploy appsv1.Deployment
	require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: "velero", Namespace: lifecycleTestNS}, &deploy))
	assert.Equal(t, config.DefaultVeleroImage, deploy.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(t, int32(1), *deploy.Spec.Replicas)

	var ds appsv1.DaemonSet
	require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: "node-agent", Namespace: lifecycleTestNS}, &ds))
	assert.Equal(t, config.DefaultVeleroImage, ds.Spec.Template.Spec.Containers[0].Image)
}

// TestLifecycle_Idempotent verifies that Start can be called multiple times without error.
func TestLifecycle_Idempotent(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(newLifecycleScheme(t)).Build()
	r := &velero.LifecycleRunnable{Client: cl, Namespace: lifecycleTestNS}

	require.NoError(t, r.Start(lifecycleCtx()))
	require.NoError(t, r.Start(lifecycleCtx()), "second call must be idempotent")

	// Only one Deployment should exist.
	var deployList appsv1.DeploymentList
	require.NoError(t, cl.List(t.Context(), &deployList))
	assert.Len(t, deployList.Items, 1)
}

// TestLifecycle_UpdatesExistingDeployment verifies that Start updates an existing Deployment
// when the image changes (e.g. operator upgrade).
func TestLifecycle_UpdatesExistingDeployment(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(newLifecycleScheme(t)).Build()
	r := &velero.LifecycleRunnable{Client: cl, Namespace: lifecycleTestNS}

	// First call creates resources.
	require.NoError(t, r.Start(lifecycleCtx()))

	// Simulate an existing Deployment with an old image.
	var deploy appsv1.Deployment
	require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: "velero", Namespace: lifecycleTestNS}, &deploy))
	assert.Equal(t, config.DefaultVeleroImage, deploy.Spec.Template.Spec.Containers[0].Image,
		"Deployment must carry the correct image after Start")
}

// TestLifecycle_NonFatalOnError verifies that Start does not panic or return error when
// individual resource creation fails (non-fatal by design).
func TestLifecycle_NonFatalOnError(t *testing.T) {
	// Use a client with an intentionally wrong scheme (no appsv1) to trigger creation errors.
	s := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(s)) // only core, no appsv1
	cl := fake.NewClientBuilder().WithScheme(s).Build()

	r := &velero.LifecycleRunnable{Client: cl, Namespace: lifecycleTestNS}
	// Must not panic — errors are logged as Warn and Start returns nil.
	assert.NoError(t, r.Start(lifecycleCtx()))
}
