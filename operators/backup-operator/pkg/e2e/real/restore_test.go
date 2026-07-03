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
	"testing"
	"time"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	backupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"
	"go.platform-mesh.io/backup-operator/pkg/restore"
	"go.platform-mesh.io/backup-operator/pkg/topology"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// TestRealEtcd_Restore_SingleShard verifies the full backup→restore round-trip against a real etcd cluster: the operator deletes and recreates the Etcd CR with the correct restore annotation, and the restored cluster reaches Ready=true.
func TestRealEtcd_Restore_SingleShard(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-real-shard-" + id
	backupName := "e2e-real-backup-" + id
	restoreName := "e2e-real-restore-" + id

	const preKey = "/e2e/pre-backup"
	const postKey = "/e2e/post-backup"
	preValue := "pre-" + id
	postValue := "post-" + id

	shard := newRealEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })
	t.Logf("[step 1] created real Etcd CR %s/%s", e2eNS, shardName)

	t.Logf("[step 2] waiting for Etcd.Status.Ready=true...")
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: shardName, Namespace: e2eNS}, shard); err != nil {
			return false
		}
		t.Logf("[poll] Etcd ready=%v currentReplicas=%d", shard.Status.Ready, shard.Status.CurrentReplicas)
		return shard.Status.Ready != nil && *shard.Status.Ready
	}, 10*time.Minute, 15*time.Second, "Etcd CR %s never became ready", shardName)
	t.Logf("[step 2] Etcd.Status.Ready=true")

	etcdEndpoint := fmt.Sprintf("http://%s-client.%s.svc:2379", shardName, e2eNS)
	require.NoError(t, runEtcdctlPod(ctx, t, "put-pre-"+id, []string{
		"etcdctl", "--endpoints=" + etcdEndpoint, "put", preKey, preValue,
	}), "etcdctl put pre-backup key failed")
	t.Logf("[step 3] wrote pre-backup key %s=%s", preKey, preValue)

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })
	t.Logf("[step 4] created PlatformBackup %s, waiting for EtcdSnapshotted=True...", backupName)

	waitForBackupComplete(t, ctx, bkp)

	require.NotNil(t, bkp.Status.Artefacts.Etcd)
	assert.Len(t, bkp.Status.Artefacts.Etcd.Shards, 1,
		"expected exactly 1 shard in backup artefacts, got %d: %v",
		len(bkp.Status.Artefacts.Etcd.Shards), bkp.Status.Artefacts.Etcd.Shards)
	shardArtefact, ok := bkp.Status.Artefacts.Etcd.Shards[shardName]
	require.True(t, ok, "no artefact for shard %s", shardName)
	require.NotEmpty(t, shardArtefact.SnapshotKey)
	t.Logf("[step 5] backup complete: snapshot key = %q", shardArtefact.SnapshotKey)

	require.NoError(t, runEtcdctlPod(ctx, t, "put-post-"+id, []string{
		"etcdctl", "--endpoints=" + etcdEndpoint, "put", postKey, postValue,
	}), "etcdctl put post-backup key failed")
	t.Logf("[step 6] wrote post-backup key %s=%s (must be absent after restore)", postKey, postValue)

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })
	t.Logf("[step 7] created PlatformRestore %s, waiting for EtcdRestored=True...", restoreName)

	waitForRestoreComplete(t, ctx, rst)
	t.Logf("[step 8] EtcdRestored=True confirmed")

	var recreated druidv1alpha1.Etcd
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: shardName, Namespace: e2eNS}, &recreated),
		"Etcd CR %s not found after restore", shardName)
	gotKey := recreated.Annotations[restore.AnnotationKeyRestoredFromSnapshot]
	assert.Equal(t, shardArtefact.SnapshotKey, gotKey, "restore annotation does not match snapshot key")
	assert.Equal(t, backup.LabelComponentKCPShard, recreated.Labels[backup.LabelKeyComponent], "kcp-shard label missing")

	waitForShardReady(t, ctx, &recreated)
	t.Logf("[step 9] restored Etcd.Status.Ready=true")

	preOut, err := runEtcdctlPodOutput(ctx, t, "get-pre-"+id, []string{
		"etcdctl", "--endpoints=" + etcdEndpoint, "get", "--print-value-only", preKey,
	})
	require.NoError(t, err)
	require.Contains(t, preOut, preValue, "pre-backup key must survive restore; got %q", preOut)
	t.Logf("[step 10] pre-backup key present ✓")

	postOut, err := runEtcdctlPodOutput(ctx, t, "get-post-"+id, []string{
		"etcdctl", "--endpoints=" + etcdEndpoint, "get", "--print-value-only", postKey,
	})
	require.NoError(t, err)
	require.NotContains(t, postOut, postValue,
		"post-backup key must be absent — etcdbr reused the stale PVC instead of replaying S3")
	t.Logf("[step 11] post-backup key absent ✓ — S3 snapshot replay confirmed")
}

// TestRealEtcd_Restore_SlowReady verifies that EtcdRestored=True is only set after the recreated Etcd CR reaches Status.Ready=true, confirming the operator waits for the full etcdbr startup before declaring success.
func TestRealEtcd_Restore_SlowReady(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-real-shard-slowready-" + id
	backupName := "e2e-real-backup-slowready-" + id
	restoreName := "e2e-real-restore-slowready-" + id

	const preKey = "/e2e/pre-slow"
	const postKey = "/e2e/post-slow"
	preValue := "pre-slow-" + id
	postValue := "post-slow-" + id

	shard := newRealEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	t.Logf("[step 1] waiting for shard %s Ready=true...", shardName)
	waitForShardReady(t, ctx, shard)
	t.Logf("[step 1] shard Ready=true")

	etcdEndpoint := fmt.Sprintf("http://%s-client.%s.svc:2379", shardName, e2eNS)
	require.NoError(t, runEtcdctlPod(ctx, t, "put-pre-slow-"+id, []string{
		"etcdctl", "--endpoints=" + etcdEndpoint, "put", preKey, preValue,
	}), "etcdctl put pre-backup key failed")
	t.Logf("[step 2] wrote pre-backup key %s=%s", preKey, preValue)

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	waitForBackupComplete(t, ctx, bkp)
	t.Logf("[step 3] backup complete")

	require.NoError(t, runEtcdctlPod(ctx, t, "put-post-slow-"+id, []string{
		"etcdctl", "--endpoints=" + etcdEndpoint, "put", postKey, postValue,
	}), "etcdctl put post-backup key failed")
	t.Logf("[step 4] wrote post-backup key %s=%s (must be absent after restore)", postKey, postValue)

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })
	t.Logf("[step 5] created PlatformRestore %s", restoreName)

	restoreStart := time.Now()
	waitForRestoreComplete(t, ctx, rst)
	restoreDuration := time.Since(restoreStart)
	t.Logf("[step 6] EtcdRestored=True after %s", restoreDuration.Round(time.Second))

	var recreated druidv1alpha1.Etcd
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: shardName, Namespace: e2eNS}, &recreated))
	require.NotNil(t, recreated.Status.Ready, "restored Etcd CR has nil Status.Ready")
	assert.True(t, *recreated.Status.Ready,
		"EtcdRestored=True was set but Etcd CR is not Ready — operator did not wait for readiness")
	t.Logf("[step 7] restored Etcd Ready=true (restore took %s)", restoreDuration.Round(time.Second))

	preOut, err := runEtcdctlPodOutput(ctx, t, "get-pre-slow-"+id, []string{
		"etcdctl", "--endpoints=" + etcdEndpoint, "get", "--print-value-only", preKey,
	})
	require.NoError(t, err)
	require.Contains(t, preOut, preValue, "pre-backup key must survive restore; got %q", preOut)
	t.Logf("[step 8] pre-backup key present ✓")

	postOut, err := runEtcdctlPodOutput(ctx, t, "get-post-slow-"+id, []string{
		"etcdctl", "--endpoints=" + etcdEndpoint, "get", "--print-value-only", postKey,
	})
	require.NoError(t, err)
	require.NotContains(t, postOut, postValue,
		"post-backup key must be absent — etcdbr reused the stale PVC instead of replaying S3")
	t.Logf("[step 9] post-backup key absent ✓ — S3 snapshot replay confirmed despite slow ready")
}

// TestRealEtcd_Restore_SourceBackupNotFound verifies that a PlatformRestore referencing a non-existent PlatformBackup results in TopologyValidated with Reason=Stopped, triggering a requeue rather than a permanent failure.
func TestRealEtcd_Restore_SourceBackupNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	restoreName := "e2e-real-restore-nobackup-" + suffix()
	nonexistentBackup := "nonexistent-backup-" + suffix()
	rst := newPlatformRestore(restoreName, nonexistentBackup)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })
	t.Logf("[step 1] created PlatformRestore %s referencing nonexistent backup %s", restoreName, nonexistentBackup)

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
		t.Logf("[poll] conditions=%+v EtcdRestored=%v", rst.Status.Conditions, cond)
		return cond != nil
	}, 2*time.Minute, 5*time.Second, "operator never set any condition on restore with missing backup")

	topoCond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
	require.NotNil(t, topoCond)
	assert.Equal(t, "Stopped", topoCond.Reason,
		"expected TopologyValidated Reason=Stopped (requeue, not hard error) when backup not found")
	cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
	require.NotNil(t, cond)
	assert.NotEqual(t, metav1.ConditionTrue, cond.Status,
		"EtcdRestored must not be True when source backup does not exist")
	t.Logf("[step 2] TopologyValidated=%s reason=%s — operator handled missing backup correctly", topoCond.Status, topoCond.Reason)
}

// TestRealEtcd_Restore_BackupWithNoEtcdArtefacts verifies that a PlatformRestore referencing a backup with no etcd artefacts skips etcd restore entirely and completes successfully without touching any Etcd CRs.
func TestRealEtcd_Restore_BackupWithNoEtcdArtefacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	backupName := "e2e-real-backup-noartefacts-" + suffix()
	restoreName := "e2e-real-restore-noartefacts-" + suffix()

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	// Wait for the operator to process the backup (any condition set).
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		return len(bkp.Status.Conditions) > 0
	}, 2*time.Minute, 5*time.Second, "backup never processed")

	// Clear the etcd artefacts via status update — simulating a backup where
	// etcd was not captured (e.g. no shards existed at backup time).
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		bkp.Status.Artefacts.Etcd = nil
		err := cl.Status().Update(ctx, bkp)
		if err != nil {
			t.Logf("[retry] status update conflict: %v", err)
		}
		return err == nil
	}, 30*time.Second, 2*time.Second, "failed to clear etcd artefacts")
	t.Logf("[step 1] backup %s created with etcd artefacts cleared", backupName)

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })
	t.Logf("[step 2] created PlatformRestore %s referencing backup with no etcd artefacts", restoreName)

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, "Ready")
		t.Logf("[poll] restore conditions=%+v", rst.Status.Conditions)
		return cond != nil && cond.Status == metav1.ConditionTrue
	}, 2*time.Minute, 5*time.Second, "restore never completed")

	t.Logf("[step 3] restore completed successfully with no etcd work — no-artefacts skip path verified")
}

// TestRealEtcd_Restore_Idempotent verifies that a forced re-reconcile of a PlatformRestore with EtcdRestored=True already set does not re-delete and re-recreate the Etcd CRs, confirming the annotation fast-path guards against double-restore.
func TestRealEtcd_Restore_Idempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-real-shard-rst-idem-" + id
	backupName := "e2e-real-backup-rst-idem-" + id
	restoreName := "e2e-real-restore-rst-idem-" + id

	shard := newRealEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	waitForShardReady(t, ctx, shard)
	t.Logf("[step 1] shard Ready=true")

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	waitForBackupComplete(t, ctx, bkp)
	t.Logf("[step 2] backup complete")

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	waitForRestoreComplete(t, ctx, rst)
	t.Logf("[step 3] EtcdRestored=True — restore complete")

	// Record the restored Etcd CR's UID. If the operator re-runs restoreShard
	// it would delete+recreate the CR, giving it a new UID. The annotation
	// fast-path (restore.go:182-187) must skip that cycle.
	var etcdAfterRestore druidv1alpha1.Etcd
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: shardName, Namespace: e2eNS}, &etcdAfterRestore))
	etcdUID := etcdAfterRestore.UID
	t.Logf("[step 3b] recorded Etcd CR UID = %s", etcdUID)

	// Force a re-reconcile by patching an annotation on the restore CR.
	var rstObj backupv1alpha1.PlatformRestore
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: restoreName}, &rstObj))
	patch := ctrlruntimeclient.MergeFrom(rstObj.DeepCopy())
	if rstObj.Annotations == nil {
		rstObj.Annotations = map[string]string{}
	}
	rstObj.Annotations["e2e/force-reconcile"] = "1"
	require.NoError(t, cl.Patch(ctx, &rstObj, patch))
	t.Logf("[step 4] forced re-reconcile via annotation patch, waiting 15s...")
	time.Sleep(15 * time.Second)

	// EtcdRestored must still be True and the Etcd CR must have the same UID —
	// the annotation fast-path skipped the delete+recreate cycle.
	var rstReread backupv1alpha1.PlatformRestore
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: restoreName}, &rstReread))
	assert.True(t, apimeta.IsStatusConditionTrue(rstReread.Status.Conditions, restore.ConditionEtcdRestored),
		"EtcdRestored condition must remain True after forced re-reconcile")

	var etcdReread druidv1alpha1.Etcd
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: shardName, Namespace: e2eNS}, &etcdReread))
	assert.Equal(t, etcdUID, etcdReread.UID,
		"Etcd CR UID changed — operator re-deleted+recreated the shard; annotation fast-path did not fire")
	t.Logf("[step 5] EtcdRestored still True, Etcd UID unchanged — annotation fast-path verified")
}

// TestRealEtcd_Restore_MissingEtcdShard verifies that when a backup recorded a shard that no longer exists in the cluster, the topology gate surfaces TopologyValidated=False/Stopped and blocks the restore before any Etcd CR is touched.
func TestRealEtcd_Restore_MissingEtcdShard(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-real-shard-missing-" + id
	backupName := "e2e-real-backup-missing-" + id
	restoreName := "e2e-real-restore-missing-" + id

	shard := newRealEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	waitForShardReady(t, ctx, shard)
	t.Logf("[step 1] shard Ready=true")

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	waitForBackupComplete(t, ctx, bkp)
	t.Logf("[step 2] backup complete, snapshot recorded for shard %s", shardName)

	// Delete the Etcd CR — simulates the shard being removed after backup.
	stripFinalizersAndDelete(t, shard)
	t.Logf("[step 3] deleted Etcd CR %s — shard is now missing", shardName)

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	// The missing shard is caught by topology validation (shard in backup but absent
	// from live cluster) — TopologyValidated=False/Stopped blocks the chain before
	// EtcdRestoreSubroutine ever runs.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
		t.Logf("[poll] TopologyValidated=%v", cond)
		return cond != nil && cond.Status == metav1.ConditionFalse
	}, 3*time.Minute, 5*time.Second, "expected TopologyValidated=False for missing shard, got: %+v", rst.Status.Conditions)

	cond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, "Stopped", cond.Reason,
		"topology gate must block restore when backup shard is absent from live cluster")
	assert.NotEmpty(t, cond.Message, "condition message should describe missing shard")
	t.Logf("[step 4] TopologyValidated=False — topology gate blocked restore: %s", cond.Message)
}

// TestRealEtcd_Restore_ConcurrentSameBackup verifies that two PlatformRestores submitted simultaneously against the same backup both settle with EtcdRestored set and leave the Etcd CR in a consistent state with the correct restore annotation.
func TestRealEtcd_Restore_ConcurrentSameBackup(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-real-shard-concurrent-" + id
	backupName := "e2e-real-backup-concurrent-" + id
	restore1Name := "e2e-real-restore-concurrent-1-" + id
	restore2Name := "e2e-real-restore-concurrent-2-" + id

	shard := newRealEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	waitForShardReady(t, ctx, shard)
	t.Logf("[step 1] shard Ready=true")

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	waitForBackupComplete(t, ctx, bkp)
	snapshotKey := bkp.Status.Artefacts.Etcd.Shards[shardName].SnapshotKey
	require.NotEmpty(t, snapshotKey)
	t.Logf("[step 2] backup complete, snapshot key = %q", snapshotKey)

	// Submit both restores concurrently — they race to delete+recreate the shard.
	rst1 := newPlatformRestore(restore1Name, backupName)
	rst2 := newPlatformRestore(restore2Name, backupName)
	require.NoError(t, cl.Create(ctx, rst1))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst1) })
	require.NoError(t, cl.Create(ctx, rst2))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst2) })
	t.Logf("[step 3] created two concurrent PlatformRestores %s and %s", restore1Name, restore2Name)

	// Both restores must eventually complete (True or False — the second one
	// may lose the delete+recreate race and report an error, which is acceptable).
	// What must NOT happen: a panic, a hang, or the shard CR being left in an
	// inconsistent state.
	for _, rstName := range []string{restore1Name, restore2Name} {
		capturedName := rstName
		require.Eventually(t, func() bool {
			var r backupv1alpha1.PlatformRestore
			if err := cl.Get(ctx, types.NamespacedName{Name: capturedName}, &r); err != nil {
				return false
			}
			cond := apimeta.FindStatusCondition(r.Status.Conditions, restore.ConditionEtcdRestored)
			t.Logf("[poll] %s EtcdRestored=%v", capturedName, cond)
			return cond != nil
		}, 15*time.Minute, 15*time.Second, "restore %s never set EtcdRestored condition", rstName)
	}
	t.Logf("[step 4] both restores completed — checking shard state")

	// The Etcd CR must still exist and carry the restore annotation.
	var recreated druidv1alpha1.Etcd
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: shardName, Namespace: e2eNS}, &recreated),
		"Etcd CR %s must still exist after concurrent restores", shardName)
	gotKey := recreated.Annotations[restore.AnnotationKeyRestoredFromSnapshot]
	assert.Equal(t, snapshotKey, gotKey,
		"restore annotation must equal snapshot key after concurrent restores")
	assert.Equal(t, backup.LabelComponentKCPShard, recreated.Labels[backup.LabelKeyComponent],
		"kcp-shard label must be present after concurrent restores")
	t.Logf("[step 5] shard %s restore annotation = %q — concurrent restore completed cleanly", shardName, gotKey)
}

// TestRealEtcd_Restore_TopologyMismatch_ExtraLiveShard verifies that with TopologyValidation=Strict, an extra live kcp-shard Etcd CR not recorded in the backup causes the restore to be blocked with TopologyValidated=False and leaves all shards untouched.
func TestRealEtcd_Restore_TopologyMismatch_ExtraLiveShard(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-real-shard-topo-" + id
	extraName := "e2e-real-shard-topo-extra-" + id
	backupName := "e2e-real-backup-topo-mismatch-" + id
	restoreName := "e2e-real-restore-topo-mismatch-" + id

	// Create and back up only shardName.
	shard := newRealEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	waitForShardReady(t, ctx, shard)
	t.Logf("[step 1] shard %s Ready=true", shardName)

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	waitForBackupComplete(t, ctx, bkp)
	t.Logf("[step 2] backup complete — artefact records only %s", shardName)

	// Now add an extra shard that was not in the backup.
	extra := newRealEtcdShard(extraName)
	require.NoError(t, cl.Create(ctx, extra))
	t.Cleanup(func() { stripFinalizersAndDelete(t, extra) })
	t.Logf("[step 3] added extra shard %s not recorded in backup", extraName)

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	// TopologyValidated must become False quickly (no cluster operations needed).
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
		t.Logf("[poll] TopologyValidated=%v", cond)
		return cond != nil && cond.Status == metav1.ConditionFalse
	}, 30*time.Second, 3*time.Second, "TopologyValidated never became False")

	cond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
	require.NotNil(t, cond)
	assert.Equal(t, "Stopped", cond.Reason)
	assert.Contains(t, cond.Message, extraName, "extra shard name must appear in mismatch message")
	t.Logf("[step 4] TopologyValidated=False — mismatch: %s", cond.Message)

	// EtcdRestored must NOT be True or False — lifecycle initialises it to Unknown
	// but the topology gate blocked the chain so the subroutine never ran.
	if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err == nil {
		etcdCond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
		if etcdCond != nil {
			assert.Equal(t, metav1.ConditionUnknown, etcdCond.Status,
				"EtcdRestored must stay Unknown when topology validation blocked the chain (got %s)", etcdCond.Status)
		}
	}

	// The backed-up shard must be untouched — it was never deleted/recreated.
	var untouched druidv1alpha1.Etcd
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: shardName, Namespace: e2eNS}, &untouched))
	assert.Empty(t, untouched.Annotations[restore.AnnotationKeyRestoredFromSnapshot],
		"backed-up shard must not carry a restore annotation — it was never touched")
	t.Logf("[step 5] shard %s unmodified — topology gate protected it", shardName)
}

// TestRealEtcd_Restore_TopologyMatch_FullRoundTrip verifies that when shard topology matches exactly, the restore proceeds through TopologyValidated=True and EtcdRestored=True, and data written to etcd before the backup survives the restore.
func TestRealEtcd_Restore_TopologyMatch_FullRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-real-shard-topo-match-" + id
	backupName := "e2e-real-backup-topo-match-" + id
	restoreName := "e2e-real-restore-topo-match-" + id

	const testKey = "/e2e/topology-integrity"
	const testPostKey = "/e2e/topology-post-backup"
	testValue := "topo-match-" + id
	testPostValue := "topo-post-" + id

	shard := newRealEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	waitForShardReady(t, ctx, shard)
	t.Logf("[step 1] shard %s Ready=true", shardName)

	// Write a known key so we can verify data integrity after restore.
	etcdEndpoint := fmt.Sprintf("http://%s-client.%s.svc:2379", shardName, e2eNS)
	require.NoError(t,
		runEtcdctlPod(ctx, t, "put-topo-"+id, []string{
			"etcdctl", "--endpoints=" + etcdEndpoint, "put", testKey, testValue,
		}),
		"etcdctl put failed",
	)
	t.Logf("[step 2] wrote key %s=%s to etcd", testKey, testValue)

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	waitForBackupComplete(t, ctx, bkp)
	shardArtefact := bkp.Status.Artefacts.Etcd.Shards[shardName]
	t.Logf("[step 3] backup complete: snapshot key = %q", shardArtefact.SnapshotKey)

	require.NoError(t, runEtcdctlPod(ctx, t, "put-topo-post-"+id, []string{
		"etcdctl", "--endpoints=" + etcdEndpoint, "put", testPostKey, testPostValue,
	}), "etcdctl put post-backup key failed")
	t.Logf("[step 3b] wrote post-backup key %s=%s (must be absent after restore)", testPostKey, testPostValue)

	rst := newPlatformRestore(restoreName, backupName) // TopologyValidation=Strict
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	// waitForRestoreComplete checks TopologyValidated=True then EtcdRestored=True.
	waitForRestoreComplete(t, ctx, rst)
	t.Logf("[step 4] TopologyValidated=True and EtcdRestored=True")

	// Verify the restored CR carries the annotation and label.
	var recreated druidv1alpha1.Etcd
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: shardName, Namespace: e2eNS}, &recreated))
	assert.Equal(t, shardArtefact.SnapshotKey, recreated.Annotations[restore.AnnotationKeyRestoredFromSnapshot])
	assert.Equal(t, backup.LabelComponentKCPShard, recreated.Labels[backup.LabelKeyComponent])

	// Wait for restored cluster to become ready (etcdbr replays snapshot on startup).
	waitForShardReady(t, ctx, &recreated)
	t.Logf("[step 5] restored shard Ready=true")

	// Verify the key written before backup survived the restore.
	out, err := runEtcdctlPodOutput(ctx, t, "get-topo-"+id, []string{
		"etcdctl", "--endpoints=" + etcdEndpoint, "get", "--print-value-only", testKey,
	})
	require.NoError(t, err, "etcdctl get failed after restore")
	require.Contains(t, out, testValue,
		"key %s not found or value mismatch after topology-aware restore — got: %q", testKey, out)
	t.Logf("[step 6] pre-backup key %s=%q confirmed after restore ✓", testKey, testValue)

	postOut, postErr := runEtcdctlPodOutput(ctx, t, "get-topo-post-"+id, []string{
		"etcdctl", "--endpoints=" + etcdEndpoint, "get", "--print-value-only", testPostKey,
	})
	require.NoError(t, postErr)
	require.NotContains(t, postOut, testPostValue,
		"post-backup key must be absent — etcdbr reused the stale PVC instead of replaying S3")
	t.Logf("[step 7] post-backup key absent ✓ — S3 snapshot replay confirmed")
}

// TestRealEtcd_Restore_CorruptTopologyAfterBackup verifies that adding an extra shard to the live cluster after a backup causes the topology gate to block the restore with TopologyValidated=False, leaving the original shard and its data untouched.
func TestRealEtcd_Restore_CorruptTopologyAfterBackup(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-real-shard-corrupt-topo-" + id
	extraName := "e2e-real-shard-corrupt-extra-" + id
	backupName := "e2e-real-backup-corrupt-topo-" + id
	restoreName := "e2e-real-restore-corrupt-topo-" + id

	const testKey = "/e2e/corrupt-topology"
	testValue := "pre-backup-" + id

	shard := newRealEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	waitForShardReady(t, ctx, shard)
	t.Logf("[step 1] shard %s Ready=true", shardName)

	etcdEndpoint := fmt.Sprintf("http://%s-client.%s.svc:2379", shardName, e2eNS)

	// Write a known key before taking the backup.
	require.NoError(t,
		runEtcdctlPod(ctx, t, "put-corrupt-topo-"+id, []string{
			"etcdctl", "--endpoints=" + etcdEndpoint, "put", testKey, testValue,
		}),
		"etcdctl put failed",
	)
	t.Logf("[step 2] wrote %s=%s to etcd before backup", testKey, testValue)

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	waitForBackupComplete(t, ctx, bkp)
	t.Logf("[step 3] backup complete — topology at backup time: {%s}", shardName)

	// Corrupt the topology: add an extra shard that was not in the backup.
	extra := newRealEtcdShard(extraName)
	require.NoError(t, cl.Create(ctx, extra))
	t.Cleanup(func() { stripFinalizersAndDelete(t, extra) })
	t.Logf("[step 4] topology corrupted — added %s after backup; live set now {%s, %s}", extraName, shardName, extraName)

	rst := newPlatformRestore(restoreName, backupName) // TopologyValidation=Strict
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	// Restore must be blocked by topology gate.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
		t.Logf("[poll] TopologyValidated=%v", cond)
		return cond != nil && cond.Status == metav1.ConditionFalse
	}, 30*time.Second, 3*time.Second, "TopologyValidated never became False")

	cond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
	require.NotNil(t, cond)
	assert.Equal(t, "Stopped", cond.Reason)
	assert.Contains(t, cond.Message, extraName)
	t.Logf("[step 5] TopologyValidated=False — restore blocked: %s", cond.Message)

	// EtcdRestored must NOT be True or False — lifecycle initialises it to Unknown
	// but the topology gate blocked the chain so the subroutine never ran.
	if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err == nil {
		etcdCond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
		if etcdCond != nil {
			assert.Equal(t, metav1.ConditionUnknown, etcdCond.Status,
				"EtcdRestored must stay Unknown when topology gate blocked the restore (got %s)", etcdCond.Status)
		}
	}

	// The original shard must be untouched: data written before backup is still readable.
	out, err := runEtcdctlPodOutput(ctx, t, "get-corrupt-topo-"+id, []string{
		"etcdctl", "--endpoints=" + etcdEndpoint, "get", "--print-value-only", testKey,
	})
	require.NoError(t, err, "etcdctl get on original shard failed")
	require.Contains(t, out, testValue,
		"original shard data must be unmodified — topology gate must have protected it: got %q", out)
	t.Logf("[step 6] original shard data %s=%q intact — topology gate protected the cluster", testKey, testValue)
}

// TestRealEtcd_Restore_CorruptEtcdAfterBackup verifies that after restoring from a snapshot,
// the pre-backup key is present and a post-backup key (written after the snapshot was taken)
// is absent in the restored etcd cluster. This proves the operator caused etcdbr to replay
// the S3 snapshot rather than reuse the stale PVC data.
//
// The PVC deletion step is critical here: without it, etcdbr detects a valid data directory
// on the surviving PVC and skips the S3 restore entirely — the post-backup key would remain,
// and the test would fail.
func TestRealEtcd_Restore_CorruptEtcdAfterBackup(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-real-shard-corrupt-etcd-" + id
	backupName := "e2e-real-backup-corrupt-etcd-" + id
	restoreName := "e2e-real-restore-corrupt-etcd-" + id

	const preKey = "/e2e/pre-backup"
	const postKey = "/e2e/post-backup"
	preValue := "before-backup-" + id
	postValue := "after-backup-" + id

	shard := newRealEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	waitForShardReady(t, ctx, shard)
	t.Logf("[step 1] shard %s Ready=true", shardName)

	etcdEndpoint := fmt.Sprintf("http://%s-client.%s.svc:2379", shardName, e2eNS)

	// Write the pre-backup key — this must survive restore.
	require.NoError(t,
		runEtcdctlPod(ctx, t, "put-pre-"+id, []string{
			"etcdctl", "--endpoints=" + etcdEndpoint, "put", preKey, preValue,
		}),
		"etcdctl put pre-backup key failed",
	)
	t.Logf("[step 2] wrote pre-backup key %s=%s", preKey, preValue)

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	waitForBackupComplete(t, ctx, bkp)
	t.Logf("[step 3] backup complete — snapshot captures %s", preKey)

	// Write a post-backup key — this must NOT appear after restore, proving
	// etcdbr replayed the S3 snapshot rather than reusing the stale PVC data.
	require.NoError(t,
		runEtcdctlPod(ctx, t, "put-post-"+id, []string{
			"etcdctl", "--endpoints=" + etcdEndpoint, "put", postKey, postValue,
		}),
		"etcdctl put post-backup key failed",
	)
	t.Logf("[step 4] wrote post-backup key %s=%s (must be absent after restore)", postKey, postValue)

	rst := newPlatformRestore(restoreName, backupName) // TopologyValidation=Strict
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	waitForRestoreComplete(t, ctx, rst)
	t.Logf("[step 5] TopologyValidated=True and EtcdRestored=True")

	// Wait for the restored cluster to be ready before reading.
	var recreated druidv1alpha1.Etcd
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: shardName, Namespace: e2eNS}, &recreated))
	waitForShardReady(t, ctx, &recreated)
	t.Logf("[step 6] restored shard Ready=true")

	// Pre-backup key must be present — snapshot replay succeeded.
	preOut, err := runEtcdctlPodOutput(ctx, t, "get-pre-"+id, []string{
		"etcdctl", "--endpoints=" + etcdEndpoint, "get", "--print-value-only", preKey,
	})
	require.NoError(t, err, "etcdctl get pre-backup key failed")
	require.Contains(t, preOut, preValue,
		"pre-backup key must survive restore (snapshot replay); got %q", preOut)
	t.Logf("[step 7] pre-backup key %s=%q present after restore ✓", preKey, preOut)

	// Post-backup key must be absent — it was written after the snapshot, so the
	// S3 restore must have rolled the cluster back to the snapshot point.
	// If this key is present, the PVC was reused and etcdbr skipped the S3 restore.
	postOut, err := runEtcdctlPodOutput(ctx, t, "get-post-"+id, []string{
		"etcdctl", "--endpoints=" + etcdEndpoint, "get", "--print-value-only", postKey,
	})
	require.NoError(t, err, "etcdctl get post-backup key failed")
	require.NotContains(t, postOut, postValue,
		"post-backup key must be absent after restore — if present, etcdbr reused the stale PVC instead of replaying S3")
	t.Logf("[step 8] post-backup key %s absent after restore ✓ — S3 snapshot replay confirmed", postKey)
}

// TestRealEtcd_Restore_MultiShard verifies that the operator correctly backs up and
// restores two independent kcp-shard Etcd CRs in a single PlatformBackup/Restore cycle.
// Each shard gets its own snapshot key; the operator deletes and recreates both CRs and
// verifies that data written to each shard before the backup is present after restore.
func TestRealEtcd_Restore_MultiShard(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardAName := "e2e-real-shard-ms-a-" + id
	shardBName := "e2e-real-shard-ms-b-" + id
	backupName := "e2e-real-backup-ms-" + id
	restoreName := "e2e-real-restore-ms-" + id

	const keyA = "/e2e/shard-a"
	const keyB = "/e2e/shard-b"
	valA := "val-a-" + id
	valB := "val-b-" + id

	shardA := newRealEtcdShard(shardAName)
	shardB := newRealEtcdShard(shardBName)
	require.NoError(t, cl.Create(ctx, shardA))
	require.NoError(t, cl.Create(ctx, shardB))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shardA) })
	t.Cleanup(func() { stripFinalizersAndDelete(t, shardB) })

	waitForShardReady(t, ctx, shardA)
	waitForShardReady(t, ctx, shardB)
	t.Logf("[step 1] both shards ready")

	epA := fmt.Sprintf("http://%s-client.%s.svc:2379", shardAName, e2eNS)
	epB := fmt.Sprintf("http://%s-client.%s.svc:2379", shardBName, e2eNS)

	const postKeyA = "/e2e/post-shard-a"
	const postKeyB = "/e2e/post-shard-b"
	postValA := "post-a-" + id
	postValB := "post-b-" + id

	require.NoError(t, runEtcdctlPod(ctx, t, "put-a-"+id, []string{"etcdctl", "--endpoints=" + epA, "put", keyA, valA}))
	require.NoError(t, runEtcdctlPod(ctx, t, "put-b-"+id, []string{"etcdctl", "--endpoints=" + epB, "put", keyB, valB}))
	t.Logf("[step 2] wrote %s=%s to shard-a, %s=%s to shard-b", keyA, valA, keyB, valB)

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	waitForBackupComplete(t, ctx, bkp)
	require.Len(t, bkp.Status.Artefacts.Etcd.Shards, 2, "backup must record artefacts for both shards")
	t.Logf("[step 3] backup complete — artefacts: %v", bkp.Status.Artefacts.Etcd.Shards)

	require.NoError(t, runEtcdctlPod(ctx, t, "put-post-a-"+id, []string{"etcdctl", "--endpoints=" + epA, "put", postKeyA, postValA}))
	require.NoError(t, runEtcdctlPod(ctx, t, "put-post-b-"+id, []string{"etcdctl", "--endpoints=" + epB, "put", postKeyB, postValB}))
	t.Logf("[step 4] wrote post-backup keys (must be absent after restore)")

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	waitForRestoreComplete(t, ctx, rst)
	t.Logf("[step 4] EtcdRestored=True for both shards")

	var recA, recB druidv1alpha1.Etcd
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: shardAName, Namespace: e2eNS}, &recA))
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: shardBName, Namespace: e2eNS}, &recB))
	waitForShardReady(t, ctx, &recA)
	waitForShardReady(t, ctx, &recB)
	t.Logf("[step 5] both restored shards Ready=true")

	outA, err := runEtcdctlPodOutput(ctx, t, "get-a-"+id, []string{"etcdctl", "--endpoints=" + epA, "get", "--print-value-only", keyA})
	require.NoError(t, err)
	require.Contains(t, outA, valA, "shard-a key must survive restore; got %q", outA)

	outB, err := runEtcdctlPodOutput(ctx, t, "get-b-"+id, []string{"etcdctl", "--endpoints=" + epB, "get", "--print-value-only", keyB})
	require.NoError(t, err)
	require.Contains(t, outB, valB, "shard-b key must survive restore; got %q", outB)
	t.Logf("[step 6] pre-backup keys confirmed on both shards ✓")

	postOutA, errA := runEtcdctlPodOutput(ctx, t, "get-post-a-"+id, []string{"etcdctl", "--endpoints=" + epA, "get", "--print-value-only", postKeyA})
	require.NoError(t, errA)
	require.NotContains(t, postOutA, postValA, "shard-a post-backup key must be absent — stale PVC reused")

	postOutB, errB := runEtcdctlPodOutput(ctx, t, "get-post-b-"+id, []string{"etcdctl", "--endpoints=" + epB, "get", "--print-value-only", postKeyB})
	require.NoError(t, errB)
	require.NotContains(t, postOutB, postValB, "shard-b post-backup key must be absent — stale PVC reused")
	t.Logf("[step 7] post-backup keys absent on both shards ✓ — S3 snapshot replay confirmed")
}

// TestRealEtcd_Restore_CustomVolumeClaimTemplate verifies that the operator correctly
// deletes PVCs when the Etcd spec uses a custom VolumeClaimTemplate name. Without this
// fix, the operator would generate the wrong PVC name, miss the deletion, and etcdbr
// would reuse the stale PVC instead of restoring from S3.
func TestRealEtcd_Restore_CustomVolumeClaimTemplate(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-real-shard-cvct-" + id
	backupName := "e2e-real-backup-cvct-" + id
	restoreName := "e2e-real-restore-cvct-" + id

	const preKey = "/e2e/pre-backup-cvct"
	const postKey = "/e2e/post-backup-cvct"
	preValue := "pre-cvct-" + id
	postValue := "post-cvct-" + id
	const customVCT = "etcd-data"

	shard := newRealEtcdShard(shardName)
	shard.Spec.VolumeClaimTemplate = ptr.To(customVCT)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	waitForShardReady(t, ctx, shard)
	t.Logf("[step 1] shard %s (VolumeClaimTemplate=%s) Ready=true", shardName, customVCT)

	ep := fmt.Sprintf("http://%s-client.%s.svc:2379", shardName, e2eNS)

	require.NoError(t, runEtcdctlPod(ctx, t, "put-pre-cvct-"+id, []string{"etcdctl", "--endpoints=" + ep, "put", preKey, preValue}))
	t.Logf("[step 2] wrote pre-backup key %s=%s", preKey, preValue)

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	waitForBackupComplete(t, ctx, bkp)
	t.Logf("[step 3] backup complete")

	require.NoError(t, runEtcdctlPod(ctx, t, "put-post-cvct-"+id, []string{"etcdctl", "--endpoints=" + ep, "put", postKey, postValue}))
	t.Logf("[step 4] wrote post-backup key %s=%s (must be absent after restore)", postKey, postValue)

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	waitForRestoreComplete(t, ctx, rst)
	t.Logf("[step 5] EtcdRestored=True")

	var recreated druidv1alpha1.Etcd
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: shardName, Namespace: e2eNS}, &recreated))
	waitForShardReady(t, ctx, &recreated)
	t.Logf("[step 6] restored shard Ready=true")

	preOut, err := runEtcdctlPodOutput(ctx, t, "get-pre-cvct-"+id, []string{"etcdctl", "--endpoints=" + ep, "get", "--print-value-only", preKey})
	require.NoError(t, err)
	require.Contains(t, preOut, preValue, "pre-backup key must survive restore; got %q", preOut)
	t.Logf("[step 7] pre-backup key present ✓")

	postOut, err := runEtcdctlPodOutput(ctx, t, "get-post-cvct-"+id, []string{"etcdctl", "--endpoints=" + ep, "get", "--print-value-only", postKey})
	require.NoError(t, err)
	require.NotContains(t, postOut, postValue,
		"post-backup key must be absent — if present, the PVC named '%s-%s-0' was not deleted (wrong VCT name used)", customVCT, shardName)
	t.Logf("[step 8] post-backup key absent ✓ — custom VolumeClaimTemplate PVC correctly deleted")
}
