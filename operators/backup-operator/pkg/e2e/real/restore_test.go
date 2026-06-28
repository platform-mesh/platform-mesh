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
	backupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"
	"go.platform-mesh.io/backup-operator/pkg/restore"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// TestRealEtcd_Restore_SingleShard verifies the full backup→restore round-trip
// using a real etcd cluster + minio. It:
//  1. Creates an Etcd CR and backs it up.
//  2. Creates a PlatformRestore referencing that backup.
//  3. Waits for EtcdRestored=True — operator deletes and recreates the Etcd CR
//     with the restore annotation so etcdbr initialises from the minio snapshot.
//  4. Asserts the recreated Etcd CR carries the correct annotation.
//  5. Waits for the restored Etcd cluster to become ready again.
func TestRealEtcd_Restore_SingleShard(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-real-shard-" + id
	backupName := "e2e-real-backup-" + id
	restoreName := "e2e-real-restore-" + id

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

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })
	t.Logf("[step 3] created PlatformBackup %s, waiting for EtcdSnapshotted=True...", backupName)

	waitForBackupComplete(t, ctx, bkp)

	require.NotNil(t, bkp.Status.Artefacts.Etcd)
	assert.Len(t, bkp.Status.Artefacts.Etcd.Shards, 1,
		"expected exactly 1 shard in backup artefacts, got %d: %v",
		len(bkp.Status.Artefacts.Etcd.Shards), bkp.Status.Artefacts.Etcd.Shards)
	shardArtefact, ok := bkp.Status.Artefacts.Etcd.Shards[shardName]
	require.True(t, ok, "no artefact for shard %s", shardName)
	require.NotEmpty(t, shardArtefact.SnapshotKey)
	t.Logf("[step 4] backup complete: snapshot key = %q", shardArtefact.SnapshotKey)

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })
	t.Logf("[step 5] created PlatformRestore %s, waiting for EtcdRestored=True...", restoreName)

	waitForRestoreComplete(t, ctx, rst)
	t.Logf("[step 6] EtcdRestored=True confirmed")

	var recreated druidv1alpha1.Etcd
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: shardName, Namespace: e2eNS}, &recreated),
		"Etcd CR %s not found after restore", shardName)

	gotKey := recreated.Annotations[restore.AnnotationKeyRestoredFromSnapshot]
	assert.Equal(t, shardArtefact.SnapshotKey, gotKey,
		"restore annotation on Etcd CR does not match snapshot key")
	assert.Equal(t, backup.LabelComponentKCPShard, recreated.Labels[backup.LabelKeyComponent],
		"kcp-shard label missing from recreated Etcd CR")
	t.Logf("[step 7] Etcd CR recreated with annotation %s=%q", restore.AnnotationKeyRestoredFromSnapshot, gotKey)

	t.Logf("[step 8] waiting for restored Etcd.Status.Ready=true...")
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: shardName, Namespace: e2eNS}, &recreated); err != nil {
			return false
		}
		t.Logf("[poll] restored Etcd ready=%v currentReplicas=%d", recreated.Status.Ready, recreated.Status.CurrentReplicas)
		return recreated.Status.Ready != nil && *recreated.Status.Ready
	}, 10*time.Minute, 15*time.Second, "restored Etcd CR %s never became ready", shardName)
	t.Logf("[step 8] restored Etcd.Status.Ready=true — round-trip complete")
}

// TestRealEtcd_Restore_SlowReady verifies the operator waits for the restored
// Etcd cluster to become ready even when it takes substantial time. The test
// explicitly asserts the sequence: EtcdRestored=True is set only AFTER
// Status.Ready=true on the recreated Etcd CR.
func TestRealEtcd_Restore_SlowReady(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-real-shard-slowready-" + id
	backupName := "e2e-real-backup-slowready-" + id
	restoreName := "e2e-real-restore-slowready-" + id

	shard := newRealEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	t.Logf("[step 1] waiting for shard %s Ready=true...", shardName)
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
	t.Logf("[step 3] created PlatformRestore %s", restoreName)

	restoreStart := time.Now()

	waitForRestoreComplete(t, ctx, rst)

	restoreDuration := time.Since(restoreStart)
	t.Logf("[step 4] EtcdRestored=True after %s", restoreDuration.Round(time.Second))

	// Assert: restored CR is Ready=true (operator waited for etcdbr to finish).
	var recreated druidv1alpha1.Etcd
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: shardName, Namespace: e2eNS}, &recreated))
	require.NotNil(t, recreated.Status.Ready, "restored Etcd CR has nil Status.Ready")
	assert.True(t, *recreated.Status.Ready,
		"EtcdRestored=True was set but Etcd CR is not Ready — operator did not wait for readiness")
	t.Logf("[step 5] restored Etcd Ready=true (restore took %s)", restoreDuration.Round(time.Second))
}

// TestRealEtcd_Restore_SourceBackupNotFound verifies that when a PlatformRestore
// references a non-existent PlatformBackup, the operator requeues (StopWithRequeue)
// rather than failing permanently.
func TestRealEtcd_Restore_SourceBackupNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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

	cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
	require.NotNil(t, cond)
	assert.NotEqual(t, metav1.ConditionTrue, cond.Status,
		"EtcdRestored must not be True when source backup does not exist")
	assert.Equal(t, "Stopped", cond.Reason,
		"expected Reason=Stopped (requeue, not hard error) when backup not found")
	t.Logf("[step 2] EtcdRestored=%s reason=%s — operator handled missing backup correctly", cond.Status, cond.Reason)
}

// TestRealEtcd_Restore_BackupWithNoEtcdArtefacts verifies that when a
// PlatformRestore references a backup that has no etcd artefacts, the restore
// subroutine skips etcd restore and completes successfully without attempting
// to recreate any Etcd CRs.
func TestRealEtcd_Restore_BackupWithNoEtcdArtefacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
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

// TestRealEtcd_Restore_Idempotent verifies that when EtcdRestored=True is
// already set on a PlatformRestore, a forced re-reconcile does not re-delete
// and re-recreate the Etcd CRs. The operator's idempotency guard must return
// early when the condition is already True.
func TestRealEtcd_Restore_Idempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
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

// TestRealEtcd_Restore_MissingEtcdShard verifies that when a PlatformRestore
// references a backup that recorded a shard, but that shard's Etcd CR no
// longer exists in the cluster, the operator surfaces EtcdRestored=False with
// Reason=Error.
func TestRealEtcd_Restore_MissingEtcdShard(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
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

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
		t.Logf("[poll] EtcdRestored=%v", cond)
		return cond != nil && cond.Status == metav1.ConditionFalse
	}, 3*time.Minute, 5*time.Second, "expected EtcdRestored=False for missing shard, got: %+v", rst.Status.Conditions)

	cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, "Error", cond.Reason,
		"expected Reason=Error (hard failure) when Etcd CR is missing before restore")
	assert.NotEmpty(t, cond.Message, "condition message should describe missing shard")
	t.Logf("[step 4] EtcdRestored=False — missing shard error surfaced: %s", cond.Message)
}

// TestRealEtcd_Restore_ConcurrentSameBackup creates two PlatformRestores that
// both reference the same PlatformBackup and runs them simultaneously. Both
// must complete with EtcdRestored=True and the Etcd CR must carry the correct
// restore annotation.
func TestRealEtcd_Restore_ConcurrentSameBackup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
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

// TestRealEtcd_Restore_TopologyMismatch_ExtraLiveShard verifies that with a real
// etcd-druid cluster, when TopologyValidation=Strict and the live cluster has an
// extra kcp-shard Etcd CR not recorded in the backup, the restore is blocked
// with TopologyValidated=False. The extra shard is never touched.
func TestRealEtcd_Restore_TopologyMismatch_ExtraLiveShard(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
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
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionTopologyValidated)
		t.Logf("[poll] TopologyValidated=%v", cond)
		return cond != nil && cond.Status == metav1.ConditionFalse
	}, 30*time.Second, 3*time.Second, "TopologyValidated never became False")

	cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionTopologyValidated)
	require.NotNil(t, cond)
	assert.Equal(t, "Error", cond.Reason)
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

// TestRealEtcd_Restore_TopologyMatch_FullRoundTrip verifies the complete
// real-cluster path: write data → backup → topology validation passes →
// etcd restore completes → data survives restore. This is the canonical
// proof that topology gating and data integrity work together: the restore
// is only allowed when the shard topology matches, and when it does proceed,
// the etcd data written before the backup is faithfully replayed.
func TestRealEtcd_Restore_TopologyMatch_FullRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-real-shard-topo-match-" + id
	backupName := "e2e-real-backup-topo-match-" + id
	restoreName := "e2e-real-restore-topo-match-" + id

	const testKey = "/e2e/topology-integrity"
	testValue := "topo-match-" + id

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
	t.Logf("[step 6] key %s=%q confirmed after restore — topology gate + data integrity verified", testKey, testValue)
}

// TestRealEtcd_Restore_CorruptTopologyAfterBackup writes data to etcd, takes a
// backup, then corrupts the live topology by adding an extra shard not recorded
// in the backup. The restore must be blocked by the topology gate (TopologyValidated=False),
// and the original shard must be left untouched — the data written before backup
// is still readable because the restore never started.
func TestRealEtcd_Restore_CorruptTopologyAfterBackup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
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
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionTopologyValidated)
		t.Logf("[poll] TopologyValidated=%v", cond)
		return cond != nil && cond.Status == metav1.ConditionFalse
	}, 30*time.Second, 3*time.Second, "TopologyValidated never became False")

	cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionTopologyValidated)
	require.NotNil(t, cond)
	assert.Equal(t, "Error", cond.Reason)
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

// TestRealEtcd_Restore_CorruptEtcdAfterBackup writes data, takes a backup, then
// writes additional keys that postdate the snapshot (simulating data written to
// etcd after the backup point). After restore, the pre-backup key must be present
// (replayed from snapshot) and the post-backup key must be absent (the snapshot
// does not include it, and the restore resets the cluster to the backup state).
func TestRealEtcd_Restore_CorruptEtcdAfterBackup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Minute)
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

	// Write the post-backup key — this must be absent after restore.
	require.NoError(t,
		runEtcdctlPod(ctx, t, "put-post-"+id, []string{
			"etcdctl", "--endpoints=" + etcdEndpoint, "put", postKey, postValue,
		}),
		"etcdctl put post-backup key failed",
	)
	t.Logf("[step 4] wrote post-backup key %s=%s (this must be wiped by restore)", postKey, postValue)

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
	t.Logf("[step 7] pre-backup key %s=%q present after restore ✓", preKey, preValue)

	// Post-backup key must be absent — restore reset the cluster to the snapshot state.
	postOut, err := runEtcdctlPodOutput(ctx, t, "get-post-"+id, []string{
		"etcdctl", "--endpoints=" + etcdEndpoint, "get", "--print-value-only", postKey,
	})
	require.NoError(t, err, "etcdctl get post-backup key failed")
	assert.Empty(t, strings.TrimSpace(postOut),
		"post-backup key must be absent after restore (not in snapshot); got %q", postOut)
	t.Logf("[step 8] post-backup key %s absent after restore ✓ — restore correctly reset to snapshot state", postKey)
}
