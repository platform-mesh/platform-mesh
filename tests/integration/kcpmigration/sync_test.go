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

package kcpmigration

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	migrationconfig "go.platform-mesh.io/kcp-migration-operator/pkg/config"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kcp-dev/logicalcluster/v3"
)

const (
	// sourceNamespace is where the sync tests create their ConfigMaps; the
	// target workspace is named after it.
	sourceNamespace = "default"
	// syncLabel selects the ConfigMaps of the sync tests, so the sync mode
	// does not also copy the source workspace's kube-root-ca.crt over the
	// target's.
	syncLabel = "kcpmigration.test.platform-mesh.io/sync"

	sourceUIDAnnotation = "migration.platform-mesh.io/source-uid"
)

// syncConfigs is what the sync mode runs with: labelled ConfigMaps from every
// namespace, targeting the workspace below targetParent named after the source
// namespace.
func syncConfigs(targetParent logicalcluster.Path) []*migrationconfig.SyncConfig {
	return []*migrationconfig.SyncConfig{{
		MigrationName: "configmaps",
		Source: migrationconfig.SourceConfig{
			APIVersion:     "v1",
			Kind:           "ConfigMap",
			LabelSelectors: []string{syncLabel + "=true"},
		},
		Target: migrationconfig.TargetConfig{
			WorkspaceExpression: targetParent.String() + ":{{ .Source.metadata.namespace }}",
		},
	}}
}

func newConfigMap(name, value string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: sourceNamespace,
			Labels:    map[string]string{syncLabel: "true"},
		},
		Data: map[string]string{"key": value},
	}
}

// waitForTarget waits until the synced copy of source in the target workspace
// carries value and points back at the source UID.
func (s *IntegrationSuite) waitForTarget(source *corev1.ConfigMap, value string) {
	s.Require().EventuallyWithT(func(c *assert.CollectT) {
		got := &corev1.ConfigMap{}
		require.NoError(c, s.targetClient.Get(s.ctx, ctrlruntimeclient.ObjectKeyFromObject(source), got))
		assert.Equal(c, value, got.Data["key"])
		assert.Equal(c, string(source.UID), got.Annotations[sourceUIDAnnotation])
		assert.Equal(c, "true", got.Labels[syncLabel])
	}, timeout, interval, "ConfigMap %s/%s was not synced to kcp with value %q", source.Namespace, source.Name, value)
}

func (s *IntegrationSuite) TestSyncConfigMapCreate() {
	cm := newConfigMap("sync-test-cm-create", "value")
	s.Require().NoError(s.client.Create(s.ctx, cm))
	s.T().Cleanup(func() { s.NoError(ctrlruntimeclient.IgnoreNotFound(s.client.Delete(s.ctx, cm))) })

	s.waitForTarget(cm, "value")

	// The source must stay untouched by the sync controller.
	got := &corev1.ConfigMap{}
	s.Require().NoError(s.client.Get(s.ctx, ctrlruntimeclient.ObjectKeyFromObject(cm), got))
	s.Equal("value", got.Data["key"])
	s.Equal(cm.ResourceVersion, got.ResourceVersion, "source ConfigMap was modified")
}

func (s *IntegrationSuite) TestSyncConfigMapUpdate() {
	cm := newConfigMap("sync-test-cm-update", "original")
	s.Require().NoError(s.client.Create(s.ctx, cm))
	s.T().Cleanup(func() { s.NoError(ctrlruntimeclient.IgnoreNotFound(s.client.Delete(s.ctx, cm))) })
	s.waitForTarget(cm, "original")

	key := ctrlruntimeclient.ObjectKeyFromObject(cm)
	s.Require().EventuallyWithT(func(c *assert.CollectT) {
		got := &corev1.ConfigMap{}
		require.NoError(c, s.client.Get(s.ctx, key, got))
		got.Data["key"] = "updated"
		require.NoError(c, s.client.Update(s.ctx, got))
	}, timeout, interval, "updating ConfigMap")

	s.Require().EventuallyWithT(func(c *assert.CollectT) {
		got := &corev1.ConfigMap{}
		require.NoError(c, s.client.Get(s.ctx, key, got))
		assert.Equal(c, "updated", got.Data["key"])
	}, timeout, interval)
	s.waitForTarget(cm, "updated")
}

func (s *IntegrationSuite) TestSyncConfigMapDelete() {
	cm := newConfigMap("sync-test-cm-delete", "value")
	s.Require().NoError(s.client.Create(s.ctx, cm))
	s.waitForTarget(cm, "value")
	s.Require().NoError(s.client.Delete(s.ctx, cm))

	s.Require().Eventually(func() bool {
		err := s.client.Get(s.ctx, ctrlruntimeclient.ObjectKeyFromObject(cm), &corev1.ConfigMap{})
		return apierrors.IsNotFound(err)
	}, timeout, interval, "ConfigMap was not deleted")
	// Deletion is not propagated to kcp yet (TODO in the sync controller), so
	// the target copy is deliberately not asserted here.
}
