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

// Package backup_test runs the backup-operator topology projector against a
// kcp workspace (formerly a controller-runtime envtest suite in
// operators/backup-operator/pkg/topology/projector).
package backup_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/backup-operator/pkg/topology/projector"
	"go.platform-mesh.io/tests/framework/kcp"
	"go.platform-mesh.io/tests/framework/scheme"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kcp-dev/multicluster-provider/envtest"
	"github.com/kcp-dev/sdk/apis/core"
)

const (
	testNamespace = "default"
	configMapName = "backup-topology-schemas"
)

// setup returns a client into a fresh workspace.
func setup(t *testing.T) ctrlruntimeclient.Client {
	t.Helper()
	env := kcp.Server(t)
	_, path := env.Workspace(t, core.RootCluster.Path(), envtest.WithNamePrefix("backup"))
	return env.Client(t, path, scheme.New(t))
}

func getConfigMap(t *testing.T, c ctrlruntimeclient.Client) *corev1.ConfigMap {
	t.Helper()
	cm := &corev1.ConfigMap{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: configMapName, Namespace: testNamespace}, cm))
	return cm
}

// EnsureConfigMap creates the ConfigMap when absent.
func TestEnsureConfigMap_Creates(t *testing.T) {
	c := setup(t)

	p := projector.New(c, testNamespace)
	require.NoError(t, p.EnsureConfigMap(t.Context()))

	cm := getConfigMap(t, c)
	assert.Contains(t, cm.Data, "v1alpha1.json")
	assert.NotEmpty(t, cm.Data["v1alpha1.json"])
}

// EnsureConfigMap is idempotent when called twice.
func TestEnsureConfigMap_Idempotent(t *testing.T) {
	c := setup(t)

	p := projector.New(c, testNamespace)
	require.NoError(t, p.EnsureConfigMap(t.Context()))
	require.NoError(t, p.EnsureConfigMap(t.Context()), "second call must not error")

	cm := getConfigMap(t, c)
	assert.Contains(t, cm.Data, "v1alpha1.json")
}

// EnsureConfigMap updates data when called after an external patch.
func TestEnsureConfigMap_Updates(t *testing.T) {
	c := setup(t)

	p := projector.New(c, testNamespace)
	require.NoError(t, p.EnsureConfigMap(t.Context()))

	// Simulate external drift: patch the ConfigMap data to stale content.
	cm := getConfigMap(t, c)
	patch := ctrlruntimeclient.MergeFrom(cm.DeepCopy())
	cm.Data["v1alpha1.json"] = `{"stale": true}`
	require.NoError(t, c.Patch(t.Context(), cm, patch))

	// Re-apply: should restore the correct schema.
	require.NoError(t, p.EnsureConfigMap(t.Context()))

	updated := getConfigMap(t, c)
	assert.NotEqual(t, `{"stale": true}`, updated.Data["v1alpha1.json"])
	assert.Contains(t, updated.Data["v1alpha1.json"], "schemaVersion")
}
