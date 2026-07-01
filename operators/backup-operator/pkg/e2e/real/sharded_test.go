//go:build e2e_real

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

package e2e_real_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.platform-mesh.io/backup-operator/pkg/backup"
	"go.platform-mesh.io/backup-operator/pkg/restore"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// discoverShardCount returns the number of non-e2e Etcd CRs in liveShardNS.
// Skips the test if none are found — sharded tests require at least one live
// shard to determine topology (set LIVE_SHARD_NAMESPACE if shards are elsewhere).
func discoverShardCount(ctx context.Context, t *testing.T) int {
	t.Helper()
	var list druidv1alpha1.EtcdList
	require.NoError(t, cl.List(ctx, &list, ctrlruntimeclient.InNamespace(liveShardNS)),
		"listing Etcd CRs in %s", liveShardNS)

	count := 0
	for _, e := range list.Items {
		if !strings.HasPrefix(e.Name, "e2e-") {
			count++
		}
	}
	if count == 0 {
		t.Skipf("no non-e2e Etcd CRs found in %s — sharded tests require a live platform-mesh deployment\n"+
			"  Set LIVE_SHARD_NAMESPACE env var if shards are in a different namespace", liveShardNS)
	}
	return count
}

// TestRealEtcd_Sharded_BackupRestore verifies that a PlatformBackup and PlatformRestore against N synthetic shards (mirroring the live cluster topology) produces per-shard snapshot artefacts in minio and restores every Etcd CR with the correct annotation and Ready=true.
func TestRealEtcd_Sharded_BackupRestore(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	shardCount := discoverShardCount(ctx, t)
	t.Logf("[preflight] %d Etcd CR(s) in %s — creating %d synthetic shards in %s to mirror topology",
		shardCount, liveShardNS, shardCount, e2eNS)

	id := suffix()
	backupName := "e2e-real-backup-sharded-" + id
	restoreName := "e2e-real-restore-sharded-" + id

	shardNames := make([]string, shardCount)
	for i := 0; i < shardCount; i++ {
		shardNames[i] = fmt.Sprintf("e2e-real-shard-%s-%d", id, i)
	}

	for _, name := range shardNames {
		shard := newRealEtcdShard(name)
		require.NoError(t, cl.Create(ctx, shard), "creating shard %s", name)
		capturedName := name
		t.Cleanup(func() {
			stripFinalizersAndDelete(t, &druidv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{Name: capturedName, Namespace: e2eNS},
			})
		})
		t.Logf("[step 1] created synthetic shard %s/%s", e2eNS, name)
	}

	t.Logf("[step 2] waiting for all %d shards to reach Ready=true...", shardCount)
	require.Eventually(t, func() bool {
		ready := 0
		for _, name := range shardNames {
			var etcd druidv1alpha1.Etcd
			if err := cl.Get(ctx, types.NamespacedName{Name: name, Namespace: e2eNS}, &etcd); err != nil {
				t.Logf("[poll] Get shard %s error: %v", name, err)
				continue
			}
			t.Logf("[poll] shard %s ready=%v currentReplicas=%d",
				name, etcd.Status.Ready, etcd.Status.CurrentReplicas)
			if etcd.Status.Ready != nil && *etcd.Status.Ready {
				ready++
			}
		}
		return ready == shardCount
	}, 15*time.Minute, 15*time.Second,
		"not all %d shards became Ready=true within timeout", shardCount)
	t.Logf("[step 2] all %d shards Ready=true", shardCount)

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })
	t.Logf("[step 3] created PlatformBackup %s, waiting for EtcdSnapshotted=True...", backupName)

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			t.Logf("[poll] Get PlatformBackup error: %v", err)
			return false
		}
		cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
		t.Logf("[poll] backup conditions=%+v EtcdSnapshotted=%v", bkp.Status.Conditions, cond)
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
	}, 15*time.Minute, 15*time.Second,
		"EtcdSnapshotted never True for backup %s", backupName)
	t.Logf("[step 4] EtcdSnapshotted=True confirmed")

	require.NotNil(t, bkp.Status.Artefacts.Etcd, "backup.Status.Artefacts.Etcd is nil")
	assert.Len(t, bkp.Status.Artefacts.Etcd.Shards, shardCount,
		"expected %d shard artefacts, got %d: %v",
		shardCount, len(bkp.Status.Artefacts.Etcd.Shards), bkp.Status.Artefacts.Etcd.Shards)

	snapshotKeys := make(map[string]string, shardCount)
	for _, name := range shardNames {
		artefact, ok := bkp.Status.Artefacts.Etcd.Shards[name]
		require.True(t, ok, "no artefact for shard %s", name)
		require.NotEmpty(t, artefact.SnapshotKey, "empty snapshot key for shard %s", name)
		assert.False(t, artefact.SnapshotTime.IsZero(), "zero snapshot time for shard %s", name)
		snapshotKeys[name] = artefact.SnapshotKey
		t.Logf("[step 5] shard %s snapshot key = %q", name, artefact.SnapshotKey)
	}

	for name := range snapshotKeys {
		require.NoError(t,
			VerifyS3SnapshotExists(ctx, cl, e2eNS, name),
			"no Full snapshot found for shard %s in bucket %q", name, minioBucket,
		)
		t.Logf("[step 6] S3 snapshot confirmed for shard %s", name)
	}

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })
	t.Logf("[step 7] created PlatformRestore %s, waiting for EtcdRestored=True...", restoreName)

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			t.Logf("[poll] Get PlatformRestore error: %v", err)
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
		t.Logf("[poll] restore conditions=%+v EtcdRestored=%v", rst.Status.Conditions, cond)
		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, restore.ConditionEtcdRestored)
	}, 30*time.Minute, 15*time.Second,
		"EtcdRestored never True for restore %s", restoreName)
	t.Logf("[step 8] EtcdRestored=True confirmed")

	for _, name := range shardNames {
		var recreated druidv1alpha1.Etcd
		require.NoError(t,
			cl.Get(ctx, types.NamespacedName{Name: name, Namespace: e2eNS}, &recreated),
			"Etcd CR %s not found after restore", name,
		)
		gotKey := recreated.Annotations[restore.AnnotationKeyRestoredFromSnapshot]
		assert.Equal(t, snapshotKeys[name], gotKey,
			"shard %s: annotation %s=%q does not match expected snapshot key %q",
			name, restore.AnnotationKeyRestoredFromSnapshot, gotKey, snapshotKeys[name])
		assert.Equal(t, backup.LabelComponentKCPShard, recreated.Labels[backup.LabelKeyComponent],
			"shard %s: kcp-shard label missing after restore", name)
		t.Logf("[step 9] shard %s restored: annotation=%q", name, gotKey)
	}

	t.Logf("[step 10] waiting for all %d restored shards to reach Ready=true...", shardCount)
	require.Eventually(t, func() bool {
		ready := 0
		for _, name := range shardNames {
			var etcd druidv1alpha1.Etcd
			if err := cl.Get(ctx, types.NamespacedName{Name: name, Namespace: e2eNS}, &etcd); err != nil {
				continue
			}
			t.Logf("[poll] restored shard %s ready=%v currentReplicas=%d",
				name, etcd.Status.Ready, etcd.Status.CurrentReplicas)
			if etcd.Status.Ready != nil && *etcd.Status.Ready {
				ready++
			}
		}
		return ready == shardCount
	}, 15*time.Minute, 15*time.Second,
		"not all %d restored shards became Ready=true within timeout", shardCount)
	t.Logf("[step 10] all %d restored shards Ready=true — sharded round-trip complete", shardCount)
}

// TestRealEtcd_Sharded_ContentIntegrity verifies that after a multi-shard backup→restore cycle, each shard's unique pre-backup key is still readable, catching cross-shard data corruption in the concurrent restore path.
func TestRealEtcd_Sharded_ContentIntegrity(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	shardCount := discoverShardCount(ctx, t)
	t.Logf("[preflight] %d live Etcd CR(s) in %s — creating %d synthetic shards", shardCount, liveShardNS, shardCount)

	id := suffix()
	backupName := "e2e-real-backup-integrity-sharded-" + id
	restoreName := "e2e-real-restore-integrity-sharded-" + id

	shardNames := make([]string, shardCount)
	for i := 0; i < shardCount; i++ {
		shardNames[i] = fmt.Sprintf("e2e-real-shard-integrity-%s-%d", id, i)
	}

	for _, name := range shardNames {
		shard := newRealEtcdShard(name)
		require.NoError(t, cl.Create(ctx, shard), "creating shard %s", name)
		capturedName := name
		t.Cleanup(func() {
			stripFinalizersAndDelete(t, &druidv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{Name: capturedName, Namespace: e2eNS},
			})
		})
	}

	t.Logf("[step 1] waiting for all %d shards Ready=true...", shardCount)
	require.Eventually(t, func() bool {
		ready := 0
		for _, name := range shardNames {
			var e druidv1alpha1.Etcd
			if err := cl.Get(ctx, types.NamespacedName{Name: name, Namespace: e2eNS}, &e); err != nil {
				continue
			}
			if e.Status.Ready != nil && *e.Status.Ready {
				ready++
			}
		}
		t.Logf("[poll] %d/%d shards ready", ready, shardCount)
		return ready == shardCount
	}, 15*time.Minute, 15*time.Second, "not all shards became ready")
	t.Logf("[step 1] all %d shards Ready=true", shardCount)

	const keyPrefix = "/e2e/integrity-sharded"
	shardValues := make(map[string]string, shardCount)
	for i, name := range shardNames {
		shardValues[name] = fmt.Sprintf("shard-%d-%s", i, id)
		etcdEndpoint := fmt.Sprintf("http://%s-client.%s.svc:2379", name, e2eNS)
		require.NoError(t,
			runEtcdctlPod(ctx, t, fmt.Sprintf("put-%s-%d", id, i), []string{
				"etcdctl", "--endpoints=" + etcdEndpoint,
				"put", keyPrefix, shardValues[name],
			}),
			"etcdctl put failed for shard %s", name,
		)
		t.Logf("[step 2] shard %s: wrote %s=%s", name, keyPrefix, shardValues[name])
	}

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	waitForBackupComplete(t, ctx, bkp)
	t.Logf("[step 3] backup %s complete", backupName)

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	waitForRestoreComplete(t, ctx, rst)
	t.Logf("[step 4] EtcdRestored=True")

	require.Eventually(t, func() bool {
		ready := 0
		for _, name := range shardNames {
			var e druidv1alpha1.Etcd
			if err := cl.Get(ctx, types.NamespacedName{Name: name, Namespace: e2eNS}, &e); err != nil {
				continue
			}
			if e.Status.Ready != nil && *e.Status.Ready {
				ready++
			}
		}
		t.Logf("[poll] %d/%d restored shards ready", ready, shardCount)
		return ready == shardCount
	}, 15*time.Minute, 15*time.Second, "not all restored shards became ready")
	t.Logf("[step 5] all %d restored shards Ready=true", shardCount)

	for i, name := range shardNames {
		etcdEndpoint := fmt.Sprintf("http://%s-client.%s.svc:2379", name, e2eNS)
		out, err := runEtcdctlPodOutput(ctx, t, fmt.Sprintf("get-%s-%d", id, i), []string{
			"etcdctl", "--endpoints=" + etcdEndpoint,
			"get", "--print-value-only", keyPrefix,
		})
		require.NoError(t, err, "etcdctl get failed for shard %s", name)
		assert.Contains(t, out, shardValues[name],
			"shard %s: key %s value mismatch after restore — want %q got %q",
			name, keyPrefix, shardValues[name], out)
		t.Logf("[step 6] shard %s: %s=%q confirmed after restore", name, keyPrefix, shardValues[name])
	}
	t.Logf("[step 6] all %d shards passed content integrity check", shardCount)
}
