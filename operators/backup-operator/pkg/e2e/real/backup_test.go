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
	"go.platform-mesh.io/backup-operator/pkg/backup"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// TestRealEtcd_Backup_SingleShard verifies that a PlatformBackup against a single real etcd shard produces a non-empty snapshot key, a non-zero snapshot time, and a Full snapshot object present in minio.
func TestRealEtcd_Backup_SingleShard(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-real-shard-" + id
	backupName := "e2e-real-backup-" + id

	shard := newRealEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })
	t.Logf("[step 1] created real Etcd CR %s/%s", e2eNS, shardName)

	t.Logf("[step 2] waiting for Etcd.Status.Ready=true (real pods starting)...")
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: shardName, Namespace: e2eNS}, shard); err != nil {
			t.Logf("[poll] Get Etcd error: %v", err)
			return false
		}
		t.Logf("[poll] Etcd ready=%v currentReplicas=%d readyReplicas=%d",
			shard.Status.Ready, shard.Status.CurrentReplicas, shard.Status.ReadyReplicas)
		return shard.Status.Ready != nil && *shard.Status.Ready
	}, 10*time.Minute, 15*time.Second, "Etcd CR %s never became ready", shardName)
	t.Logf("[step 2] Etcd.Status.Ready=true confirmed")

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })
	t.Logf("[step 3] created PlatformBackup %s, waiting for EtcdSnapshotted=True...", backupName)

	waitForBackupComplete(t, ctx, bkp)
	t.Logf("[step 4] EtcdSnapshotted=True confirmed")

	require.NotNil(t, bkp.Status.Artefacts.Etcd, "backup.Status.Artefacts.Etcd is nil")
	assert.Len(t, bkp.Status.Artefacts.Etcd.Shards, 1,
		"expected exactly 1 shard in backup artefacts, got %d: %v",
		len(bkp.Status.Artefacts.Etcd.Shards), bkp.Status.Artefacts.Etcd.Shards)
	shardArtefact, ok := bkp.Status.Artefacts.Etcd.Shards[shardName]
	require.True(t, ok, "no artefact recorded for shard %s", shardName)
	require.NotEmpty(t, shardArtefact.SnapshotKey, "snapshot key is empty for shard %s", shardName)
	require.False(t, shardArtefact.SnapshotTime.IsZero(), "snapshot time is zero for shard %s", shardName)
	assert.True(t, !shardArtefact.SnapshotTime.Time.Before(bkp.CreationTimestamp.Time),
		"snapshot time %v must not be before backup creation time %v — stale timestamp from a prior run?",
		shardArtefact.SnapshotTime, bkp.CreationTimestamp.Time)
	t.Logf("[step 5] shard %s snapshot key = %q time = %v", shardName, shardArtefact.SnapshotKey, shardArtefact.SnapshotTime)

	t.Logf("[step 6] verifying S3 snapshot exists under prefix %s/v2/", shardName)
	require.NoError(t,
		VerifyS3SnapshotExists(ctx, cl, e2eNS, shardName),
		"no Full snapshot found for shard %s in minio bucket %q", shardName, minioBucket,
	)
	t.Logf("[step 6] S3 snapshot confirmed present in minio")
}

// TestRealEtcd_Backup_Idempotent verifies that running a second PlatformBackup against the same shard records the same snapshot key as the first, confirming the idempotency guard works correctly with real etcdbr.
func TestRealEtcd_Backup_Idempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-real-shard-idem-" + id
	backup1Name := "e2e-real-backup-idem-1-" + id
	backup2Name := "e2e-real-backup-idem-2-" + id

	shard := newRealEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })
	t.Logf("[step 1] created shard %s, waiting for Ready=true...", shardName)

	waitForShardReady(t, ctx, shard)
	t.Logf("[step 1] shard Ready=true")

	bkp1 := newPlatformBackup(backup1Name)
	require.NoError(t, cl.Create(ctx, bkp1))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp1) })
	t.Logf("[step 2] created first PlatformBackup %s", backup1Name)

	waitForBackupComplete(t, ctx, bkp1)

	require.NotNil(t, bkp1.Status.Artefacts.Etcd)
	firstKey := bkp1.Status.Artefacts.Etcd.Shards[shardName].SnapshotKey
	require.NotEmpty(t, firstKey)
	t.Logf("[step 3] first backup snapshot key = %q", firstKey)

	bkp2 := newPlatformBackup(backup2Name)
	require.NoError(t, cl.Create(ctx, bkp2))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp2) })
	t.Logf("[step 4] created second PlatformBackup %s", backup2Name)

	waitForBackupComplete(t, ctx, bkp2)

	require.NotNil(t, bkp2.Status.Artefacts.Etcd)
	secondKey := bkp2.Status.Artefacts.Etcd.Shards[shardName].SnapshotKey
	secondTime := bkp2.Status.Artefacts.Etcd.Shards[shardName].SnapshotTime
	require.NotEmpty(t, secondKey)
	t.Logf("[step 5] second backup snapshot key = %q time = %v", secondKey, secondTime)

	// The snapshot key must be identical — same lease value reused.
	// SnapshotTime is recorded as metav1.Now() at the moment captureOne reads
	// the result, so it advances on every backup call even when the underlying
	// snapshot is the same. We only assert the key.
	assert.Equal(t, firstKey, secondKey,
		"snapshot key changed between backups — result should be the same lease key")
	t.Logf("[step 6] idempotency confirmed: both backups recorded key=%q", firstKey)
}

// TestRealEtcd_Backup_ContentIntegrity verifies that a key written to etcd before backup is still readable after a full backup→restore cycle, proving etcdbr snapshots and replays data faithfully.
func TestRealEtcd_Backup_ContentIntegrity(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-real-shard-integrity-" + id
	backupName := "e2e-real-backup-integrity-" + id
	restoreName := "e2e-real-restore-integrity-" + id

	const testKey = "/e2e/integrity-check"
	const testValue = "platform-mesh-e2e-real"

	shard := newRealEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	t.Logf("[step 1] waiting for shard %s Ready=true...", shardName)
	waitForShardReady(t, ctx, shard)
	t.Logf("[step 1] shard Ready=true")

	etcdEndpoint := fmt.Sprintf("http://%s-client.%s.svc:2379", shardName, e2eNS)

	t.Logf("[step 2] writing key %s=%s via etcdctl pod", testKey, testValue)
	require.NoError(t,
		runEtcdctlPod(ctx, t, "put-"+id, []string{
			"etcdctl", "--endpoints=" + etcdEndpoint, "put", testKey, testValue,
		}),
		"etcdctl put failed",
	)
	t.Logf("[step 2] key written")

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	waitForBackupComplete(t, ctx, bkp)
	t.Logf("[step 3] backup complete")

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	waitForRestoreComplete(t, ctx, rst)
	t.Logf("[step 4] EtcdRestored=True")

	recreated := druidv1alpha1.Etcd{}
	recreated.Name = shardName
	waitForShardReady(t, ctx, &recreated)
	t.Logf("[step 5] restored shard Ready=true")

	t.Logf("[step 6] verifying key %s is present after restore...", testKey)
	out, err := runEtcdctlPodOutput(ctx, t, "get-"+id, []string{
		"etcdctl", "--endpoints=" + etcdEndpoint, "get", "--print-value-only", testKey,
	})
	require.NoError(t, err, "etcdctl get failed")
	require.Contains(t, out, testValue,
		"key %s not found or value mismatch after restore — got: %q", testKey, out)
	t.Logf("[step 6] key %s=%q confirmed after restore — content integrity verified", testKey, testValue)
}

// TestRealEtcd_Backup_NoShards verifies that when a PlatformBackup is created with no kcp-shard Etcd CRs present, the operator surfaces EtcdSnapshotted with Reason=Stopped and requeues rather than permanently failing.
func TestRealEtcd_Backup_NoShards(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)
	// Do NOT create any shard CR — the namespace has no kcp-shard labelled Etcd CRs.

	backupName := "e2e-real-backup-noshards-" + suffix()
	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })
	t.Logf("[step 1] created PlatformBackup %s with no kcp-shard Etcd CRs present", backupName)

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
		t.Logf("[poll] conditions=%+v EtcdSnapshotted=%v", bkp.Status.Conditions, cond)
		return cond != nil
	}, 2*time.Minute, 5*time.Second, "operator never set EtcdSnapshotted condition")

	cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
	require.NotNil(t, cond)
	assert.NotEqual(t, metav1.ConditionTrue, cond.Status,
		"EtcdSnapshotted must not be True when no shards exist")
	assert.Equal(t, "Stopped", cond.Reason,
		"expected Reason=Stopped (requeue, not hard error) when no shards found")
	assert.NotEmpty(t, cond.Message, "condition message should explain no shards found")
	t.Logf("[step 2] EtcdSnapshotted=%s reason=%s message=%s", cond.Status, cond.Reason, cond.Message)
}

// TestRealEtcd_Backup_ShardDeletedDuringBackup verifies that deleting a shard's Etcd CR while an EtcdOpsTask is in-flight causes the operator to surface a clean EtcdSnapshotted error condition rather than panicking or hanging.
func TestRealEtcd_Backup_ShardDeletedDuringBackup(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-real-shard-delduring-" + id
	backupName := "e2e-real-backup-delduring-" + id

	shard := newRealEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	waitForShardReady(t, ctx, shard)
	t.Logf("[step 1] shard Ready=true")

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })
	t.Logf("[step 2] created PlatformBackup %s", backupName)

	// Wait until the operator has created the EtcdOpsTask (backup started).
	taskName := backup.OpsTaskName(backupName, shardName)
	require.Eventually(t, func() bool {
		var task druidv1alpha1.EtcdOpsTask
		return cl.Get(ctx, types.NamespacedName{Name: taskName, Namespace: e2eNS}, &task) == nil
	}, 2*time.Minute, 2*time.Second, "EtcdOpsTask %s never created", taskName)
	t.Logf("[step 3] EtcdOpsTask %s created — deleting shard CR mid-backup", taskName)

	stripFinalizersAndDelete(t, shard)
	t.Logf("[step 3] shard %s deleted", shardName)

	// The backup must eventually settle — either the task completed before
	// deletion (EtcdSnapshotted=True) or the operator surfaced an error.
	// Either outcome is acceptable; what is NOT acceptable is a hang or panic.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
		t.Logf("[poll] EtcdSnapshotted=%v", cond)
		return cond != nil
	}, 5*time.Minute, 5*time.Second, "backup never settled after shard deletion")

	cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
	require.NotNil(t, cond)
	t.Logf("[step 4] backup settled: EtcdSnapshotted=%s reason=%s message=%s",
		cond.Status, cond.Reason, cond.Message)
	// If it failed, reason must be Error or Stopped (not a nil-panic reported as Pending).
	// When the shard is deleted before the operator re-lists, it sees zero shards and
	// stops-with-requeue (Reason=Stopped). When the task itself fails, Reason=Error.
	// Both are valid terminal states; what matters is that the operator did not hang
	// or silently drop the failure.
	if cond.Status != metav1.ConditionTrue {
		assert.Contains(t, []string{"Error", "Stopped"}, cond.Reason,
			"failed backup must carry Reason=Error or Reason=Stopped, not %q", cond.Reason)
	}
}

// TestRealEtcd_Backup_AfterRestore verifies that after a full backup→restore cycle the restored cluster can be backed up again, ensuring etcdbr correctly reinitialises its snapshotter after a restore.
func TestRealEtcd_Backup_AfterRestore(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-real-shard-bar-" + id // bar = backup-after-restore
	backup1Name := "e2e-real-backup-bar-1-" + id
	restoreName := "e2e-real-restore-bar-" + id
	backup2Name := "e2e-real-backup-bar-2-" + id

	shard := newRealEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	t.Logf("[step 1] waiting for shard %s Ready=true...", shardName)
	waitForShardReady(t, ctx, shard)
	t.Logf("[step 1] shard Ready=true")

	bkp1 := newPlatformBackup(backup1Name)
	require.NoError(t, cl.Create(ctx, bkp1))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp1) })

	waitForBackupComplete(t, ctx, bkp1)
	t.Logf("[step 2] first backup complete")

	require.NotNil(t, bkp1.Status.Artefacts.Etcd)
	firstKey := bkp1.Status.Artefacts.Etcd.Shards[shardName].SnapshotKey
	require.NotEmpty(t, firstKey)

	rst := newPlatformRestore(restoreName, backup1Name)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	waitForRestoreComplete(t, ctx, rst)
	t.Logf("[step 3] restore complete")

	restoredShard := druidv1alpha1.Etcd{}
	restoredShard.Name = shardName
	waitForShardReady(t, ctx, &restoredShard)
	t.Logf("[step 4] restored shard Ready=true")

	bkp2 := newPlatformBackup(backup2Name)
	require.NoError(t, cl.Create(ctx, bkp2))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp2) })
	t.Logf("[step 5] created second backup %s after restore", backup2Name)

	waitForBackupComplete(t, ctx, bkp2)

	require.NotNil(t, bkp2.Status.Artefacts.Etcd)
	secondKey := bkp2.Status.Artefacts.Etcd.Shards[shardName].SnapshotKey
	require.NotEmpty(t, secondKey)
	t.Logf("[step 5] second backup complete: key=%q (first was %q)", secondKey, firstKey)

	require.NoError(t, VerifyS3SnapshotExists(ctx, cl, e2eNS, shardName),
		"second backup S3 snapshot not found in minio")
	t.Logf("[step 6] backup-after-restore complete: etcdbr can snapshot from a restored cluster")
}
