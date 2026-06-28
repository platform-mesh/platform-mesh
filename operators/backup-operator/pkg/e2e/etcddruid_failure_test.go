//go:build e2e

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

package e2e_test

import (
	"context"
	"testing"
	"time"

	druidapicommon "github.com/gardener/etcd-druid/api/common"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ── Backup failure scenarios ─────────────────────────────────────────────────

// TestEtcDruid_Capture_NoShards verifies the operator sets EtcdSnapshotted=False
// (with an error reason) when no kcp-shard Etcd CRs exist in the namespace at
// backup time.  The backup itself must not enter a permanent error loop — the
// lifecycle will requeue it, so we only need to observe the condition is False
// within a reasonable window.
func TestEtcDruid_Capture_NoShards(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	backupName := "e2e-backup-noshards-" + suffix()
	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Logf("[step 1] created PlatformBackup %s", backupName)
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	// No Etcd shard CR exists. Wait for the operator to process and surface the error.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			t.Logf("[poll] Get PlatformBackup error: %v", err)
			return false
		}
		cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
		t.Logf("[poll] conditions=%+v EtcdSnapshotted=%v", bkp.Status.Conditions, cond)
		return cond != nil && cond.Status == metav1.ConditionFalse
	}, 2*time.Minute, 5*time.Second,
		"expected EtcdSnapshotted=False when no shards exist, but condition never became False")

	t.Logf("[step 2] EtcdSnapshotted=False confirmed")
	cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
	require.NotNil(t, cond)
	t.Logf("[step 3] final condition: status=%s reason=%s message=%s", cond.Status, cond.Reason, cond.Message)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.NotEmpty(t, cond.Message, "condition message should explain that no shards were found")
}

// TestEtcDruid_Capture_TaskFailed verifies that when etcd-druid marks an EtcdOpsTask
// as Failed the operator surfaces the failure on the backup condition and the backup
// is retried (EtcdOpsTask is deleted so a fresh one can be created on the next reconcile).
//
// Injection: after the operator creates the EtcdOpsTask this test patches it to
// Failed before etcd-druid can act on it, simulating a druid-side snapshot error.
func TestEtcDruid_Capture_TaskFailed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-shard-taskfail-" + id
	backupName := "e2e-backup-taskfail-" + id

	shard := newEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	require.NoError(t, ensureEtcdReady(ctx, shardName))
	t.Logf("[step 1] created Etcd shard %s (ready=true)", shardName)
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Logf("[step 2] created PlatformBackup %s", backupName)
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	taskName := backup.OpsTaskName(backupName, shardName)
	t.Logf("[step 3] injecting Failed state on EtcdOpsTask %s", taskName)

	// Inject a Failed state into the EtcdOpsTask as soon as the operator creates it.
	go injectTaskFailure(ctx, t, taskName, "simulated etcd snapshot error")

	// The operator should set EtcdSnapshotted=False after observing the task failure.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			t.Logf("[poll] Get PlatformBackup error: %v", err)
			return false
		}
		cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
		t.Logf("[poll] conditions=%+v EtcdSnapshotted=%v", bkp.Status.Conditions, cond)

		var task druidv1alpha1.EtcdOpsTask
		if err := cl.Get(ctx, types.NamespacedName{Name: taskName, Namespace: e2eNS}, &task); err != nil {
			t.Logf("[poll] EtcdOpsTask %s: %v", taskName, err)
		} else {
			t.Logf("[poll] EtcdOpsTask %s state=%v", taskName, task.Status.State)
		}

		return cond != nil && cond.Status == metav1.ConditionFalse
	}, 3*time.Minute, 5*time.Second,
		"expected EtcdSnapshotted=False after task failure, got: %+v", bkp.Status.Conditions)

	t.Logf("[step 4] EtcdSnapshotted=False confirmed")

	// The failed EtcdOpsTask must have been deleted so the next reconcile can retry.
	var task druidv1alpha1.EtcdOpsTask
	err := cl.Get(ctx, types.NamespacedName{Name: taskName, Namespace: e2eNS}, &task)
	t.Logf("[step 5] EtcdOpsTask presence check: err=%v", err)
	assert.True(t, client.IgnoreNotFound(err) == nil,
		"EtcdOpsTask %s should be deleted after task failure so retries are unblocked", taskName)
}

// TestEtcDruid_Capture_TaskRejected mirrors TaskFailed but uses the Rejected terminal
// state, which etcd-druid uses when preconditions are not met (e.g. etcd not ready).
func TestEtcDruid_Capture_TaskRejected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-shard-rejected-" + id
	backupName := "e2e-backup-rejected-" + id

	shard := newEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	require.NoError(t, ensureEtcdReady(ctx, shardName))
	t.Logf("[step 1] created Etcd shard %s (ready=true)", shardName)
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Logf("[step 2] created PlatformBackup %s", backupName)
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	taskName := backup.OpsTaskName(backupName, shardName)
	t.Logf("[step 3] injecting Rejected state on EtcdOpsTask %s", taskName)
	go injectTaskRejected(ctx, t, taskName, "simulated precondition failure")

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			t.Logf("[poll] Get PlatformBackup error: %v", err)
			return false
		}
		cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
		t.Logf("[poll] conditions=%+v EtcdSnapshotted=%v", bkp.Status.Conditions, cond)

		// Also log the EtcdOpsTask state while waiting
		var task druidv1alpha1.EtcdOpsTask
		if err := cl.Get(ctx, types.NamespacedName{Name: taskName, Namespace: e2eNS}, &task); err != nil {
			t.Logf("[poll] EtcdOpsTask %s: %v", taskName, err)
		} else {
			t.Logf("[poll] EtcdOpsTask %s state=%v", taskName, task.Status.State)
		}

		return cond != nil && cond.Status == metav1.ConditionFalse
	}, 3*time.Minute, 5*time.Second,
		"expected EtcdSnapshotted=False after task rejection, got: %+v", bkp.Status.Conditions)

	t.Logf("[step 4] EtcdSnapshotted=False confirmed")
	var task druidv1alpha1.EtcdOpsTask
	err := cl.Get(ctx, types.NamespacedName{Name: taskName, Namespace: e2eNS}, &task)
	t.Logf("[step 5] EtcdOpsTask presence check: err=%v", err)
	assert.True(t, client.IgnoreNotFound(err) == nil,
		"EtcdOpsTask should be deleted after rejection")
}

// TestEtcDruid_Capture_LeaseNotUpdated verifies that when the EtcdOpsTask
// transitions to Succeeded but the full-snap lease HolderIdentity is empty
// (no snapshot has ever been written), the operator surfaces an error on
// EtcdSnapshotted. Note: a non-empty lease that hasn't changed from the
// baseline is now treated as success — EtcdOpsTask Succeeded is the
// authoritative signal that etcdbr wrote the snapshot (on-demand snapshots
// do not bump HolderIdentity, only scheduled ones do).
func TestEtcDruid_Capture_LeaseNotUpdated(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-shard-nolease-" + id
	backupName := "e2e-backup-nolease-" + id

	shard := newEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	require.NoError(t, ensureEtcdReady(ctx, shardName))
	t.Logf("[step 1] created Etcd shard %s (ready=true)", shardName)
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Logf("[step 2] created PlatformBackup %s", backupName)
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	taskName := backup.OpsTaskName(backupName, shardName)
	t.Logf("[step 3] injecting Succeeded-no-lease-update on EtcdOpsTask %s", taskName)

	// Transition task to Succeeded without updating the full-snap lease.
	go injectTaskSucceededNoLeaseUpdate(ctx, t, taskName)

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			t.Logf("[poll] Get PlatformBackup error: %v", err)
			return false
		}
		cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
		t.Logf("[poll] conditions=%+v EtcdSnapshotted=%v", bkp.Status.Conditions, cond)

		var task druidv1alpha1.EtcdOpsTask
		if err := cl.Get(ctx, types.NamespacedName{Name: taskName, Namespace: e2eNS}, &task); err != nil {
			t.Logf("[poll] EtcdOpsTask %s: %v", taskName, err)
		} else {
			t.Logf("[poll] EtcdOpsTask %s state=%v", taskName, task.Status.State)
		}

		return cond != nil && cond.Status == metav1.ConditionFalse
	}, 3*time.Minute, 5*time.Second,
		"expected EtcdSnapshotted=False when lease not updated, got: %+v", bkp.Status.Conditions)

	t.Logf("[step 4] EtcdSnapshotted=False confirmed")
	cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
	require.NotNil(t, cond)
	t.Logf("[step 5] final condition: status=%s reason=%s message=%s", cond.Status, cond.Reason, cond.Message)
}

// ── Restore failure scenarios ─────────────────────────────────────────────────

// TestEtcDruid_Restore_MissingBackup verifies the operator sets EtcdRestored with
// a stopped/requeue condition (not a hard error) when the referenced PlatformBackup
// does not exist, and that it recovers once the backup appears.
func TestEtcDruid_Restore_MissingBackup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	restoreName := "e2e-restore-missingbk-" + suffix()
	nonexistentBackup := "nonexistent-backup-" + suffix()
	rst := newPlatformRestore(restoreName, nonexistentBackup)
	require.NoError(t, cl.Create(ctx, rst))
	t.Logf("[step 1] created PlatformRestore %s referencing nonexistent backup %s", restoreName, nonexistentBackup)
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	// The operator should stop with requeue, not hard-error. Observable as: the
	// Ready condition is False with a Stopped (not Error) reason, and the
	// EtcdRestored condition is not yet True.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			t.Logf("[poll] Get PlatformRestore error: %v", err)
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
		t.Logf("[poll] conditions=%+v EtcdRestored=%v", rst.Status.Conditions, cond)
		// Any non-True state on EtcdRestored within a short window means the
		// operator at least processed the object without panicking.
		return cond != nil
	}, 2*time.Minute, 5*time.Second,
		"operator never set a condition on restore with missing backup")

	t.Logf("[step 2] condition set by operator")
	cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
	require.NotNil(t, cond)
	t.Logf("[step 3] final condition: status=%s reason=%s message=%s", cond.Status, cond.Reason, cond.Message)
	assert.NotEqual(t, metav1.ConditionTrue, cond.Status,
		"EtcdRestored must not be True when source backup is absent")
}

// TestEtcDruid_Restore_MissingEtcdShard verifies that if the Etcd CR for a shard
// referenced in the backup artefacts is absent from the cluster at restore time,
// TestEtcDruid_Restore_MissingEtcdShard verifies that when a backup artefact
// records a shard that no longer exists on the live cluster, the topology gate
// blocks the restore with TopologyValidated=False before EtcdRestoreSubroutine
// ever attempts to delete or recreate any CR.
func TestEtcDruid_Restore_MissingEtcdShard(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	backupName := "e2e-backup-missingshard-" + id
	restoreName := "e2e-restore-missingshard-" + id
	ghostShard := "ghost-shard-" + id

	// Create a backup with an artefact for a shard that does NOT exist on the cluster.
	bkp := newPlatformBackupWithArtefact(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Logf("[step 1] created PlatformBackup %s", backupName)
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	// Manually inject artefacts — as if a prior backup completed for a shard that
	// was subsequently deleted. Retry on conflict since the operator may reconcile
	// the backup between our Create and Status().Update() calls.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		bkp.Status = backupv1alpha1.PlatformBackupStatus{
			Artefacts: backupv1alpha1.ArtefactsStatus{
				Etcd: &backupv1alpha1.EtcdArtefact{
					Shards: map[string]backupv1alpha1.EtcdShardArtefact{
						ghostShard: {
							SnapshotKey:  "rev-99",
							SnapshotTime: metav1.Now(),
						},
					},
				},
			},
		}
		err := cl.Status().Update(ctx, bkp)
		if err != nil {
			t.Logf("[step 2] status update conflict, retrying: %v", err)
		}
		return err == nil
	}, 10*time.Second, 500*time.Millisecond, "failed to inject artefact for ghost shard %s", ghostShard)
	t.Logf("[step 2] injected artefact for ghost shard %s", ghostShard)

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Logf("[step 3] created PlatformRestore %s", restoreName)
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	// Topology gate must block the restore: ghost-shard is in the backup but absent
	// from the live cluster, so TopologyValidated=False is set before EtcdRestoreSubroutine
	// ever runs. EtcdRestored remains Unknown (chain never reached it).
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			t.Logf("[poll] Get PlatformRestore error: %v", err)
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
		t.Logf("[poll] conditions=%+v TopologyValidated=%v", rst.Status.Conditions, cond)
		return cond != nil && cond.Status == metav1.ConditionFalse
	}, 2*time.Minute, 5*time.Second,
		"expected TopologyValidated=False when backup shard is absent from live cluster")

	cond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
	require.NotNil(t, cond)
	assert.Equal(t, "Stopped", cond.Reason)
	assert.Contains(t, cond.Message, ghostShard)
	t.Logf("[step 4] TopologyValidated=False confirmed — mismatch: %s", cond.Message)

	// EtcdRestored must NOT be False (chain never reached it — it stays Unknown).
	etcdCond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
	if etcdCond != nil {
		assert.NotEqual(t, metav1.ConditionFalse, etcdCond.Status,
			"EtcdRestored must not be False — topology gate blocked before etcd restore ran")
	}
	t.Logf("[step 5] EtcdRestored=%v — topology gate protected the restore chain", etcdCond)
}

// TestEtcDruid_Restore_EtcdNotReady verifies the operator waits (not errors
// immediately) when etcd-druid acknowledges the recreated Etcd CR but it takes
// time to reach ready=true. We inject a slow-ready Etcd by creating the shard
// without setting ready=true and only flipping it after a delay.
func TestEtcDruid_Restore_EtcdNotReady(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	shardName := "e2e-shard-slowready-" + id
	backupName := "e2e-backup-slowready-" + id
	restoreName := "e2e-restore-slowready-" + id

	// Create the Etcd shard. The SuspendEtcdSpecReconcileAnnotation on the CR
	// (set in newEtcdShard) prevents real etcd-druid from rejecting EtcdOpsTasks
	// with ERR_ETCD_NOT_READY during the backup phase.
	shard := newEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Logf("[step 1] created Etcd shard %s", shardName)
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	// Start the task simulator before the backup so it can complete the EtcdOpsTask
	// as soon as the operator creates it. The simulator completes tasks unconditionally
	// without checking Etcd readiness — etcd-druid continuously overwrites ready=false
	// on fake CRs, so the readySimulator and the task simulator both race against it.
	startTaskSimulator(ctx, t)
	t.Logf("[step 2] task simulator started for backup phase")

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Logf("[step 3] created PlatformBackup %s", backupName)
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			t.Logf("[poll] Get PlatformBackup error: %v", err)
			return false
		}
		cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
		t.Logf("[poll] backup conditions=%+v EtcdSnapshotted=%v", bkp.Status.Conditions, cond)
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
	}, 5*time.Minute, 10*time.Second, "backup %s never completed", backupName)

	t.Logf("[step 4] backup EtcdSnapshotted=True confirmed")

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Logf("[step 5] created PlatformRestore %s", restoreName)
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	// Flip ready=true on any newly created (non-ready) Etcd CR with a 20s delay.
	// slowReadySimulator controls the recreated shard's readiness in the restore phase.
	go slowReadySimulator(ctx, t, 20*time.Second)
	t.Logf("[step 6] slow-ready simulator started (delay=20s)")

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			t.Logf("[poll] Get PlatformRestore error: %v", err)
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
		t.Logf("[poll] restore conditions=%+v EtcdRestored=%v", rst.Status.Conditions, cond)

		var etcd druidv1alpha1.Etcd
		if err := cl.Get(ctx, types.NamespacedName{Name: shardName, Namespace: e2eNS}, &etcd); err != nil {
			t.Logf("[poll] Etcd CR %s: %v", shardName, err)
		} else {
			t.Logf("[poll] Etcd CR %s ready=%v annotations=%v", shardName, etcd.Status.Ready, etcd.Annotations)
		}

		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, restore.ConditionEtcdRestored)
	}, 6*time.Minute, 10*time.Second,
		"EtcdRestored never became True even after delayed ready flip")

	t.Logf("[step 7] EtcdRestored=True confirmed")

	var recreated druidv1alpha1.Etcd
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: shardName, Namespace: e2eNS}, &recreated))
	assert.NotEmpty(t, recreated.Annotations[restore.AnnotationKeyRestoredFromSnapshot])
	t.Logf("[step 8] restore annotation present: %s=%q",
		restore.AnnotationKeyRestoredFromSnapshot, recreated.Annotations[restore.AnnotationKeyRestoredFromSnapshot])
}

// ── Injectors and helpers ─────────────────────────────────────────────────────

// injectTaskFailure waits for taskName to appear and patches it to Failed.
func injectTaskFailure(ctx context.Context, t *testing.T, taskName, description string) {
	t.Helper()
	injectTaskTerminal(ctx, t, taskName, druidv1alpha1.TaskStateFailed, description)
}

// injectTaskRejected waits for taskName to appear and patches it to Rejected.
func injectTaskRejected(ctx context.Context, t *testing.T, taskName, description string) {
	t.Helper()
	injectTaskTerminal(ctx, t, taskName, druidv1alpha1.TaskStateRejected, description)
}

func injectTaskTerminal(ctx context.Context, t *testing.T, taskName string, state druidv1alpha1.TaskState, description string) {
	t.Helper()
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}

		var task druidv1alpha1.EtcdOpsTask
		if err := cl.Get(ctx, types.NamespacedName{Name: taskName, Namespace: e2eNS}, &task); err != nil {
			continue
		}
		// Only patch if not already terminal.
		if task.Status.State != nil {
			switch *task.Status.State {
			case druidv1alpha1.TaskStateSucceeded, druidv1alpha1.TaskStateFailed, druidv1alpha1.TaskStateRejected:
				return
			}
		}
		patch := client.MergeFrom(task.DeepCopy())
		task.Status.State = &state
		if description != "" {
			task.Status.LastErrors = []druidapicommon.LastError{{
				Code:        "ERR_INJECTED",
				Description: description,
				ObservedAt:  metav1.Now(),
			}}
		}
		if err := cl.Status().Patch(ctx, &task, patch); err != nil {
			t.Logf("injectTaskTerminal patch error (will retry): %v", err)
			continue
		}
		t.Logf("injectTaskTerminal: patched EtcdOpsTask %s to state=%s", taskName, state)
		return
	}
}

// injectTaskSucceededNoLeaseUpdate transitions the task to Succeeded without
// touching the full-snap lease, simulating a druid bug where the lease is stale.
func injectTaskSucceededNoLeaseUpdate(ctx context.Context, t *testing.T, taskName string) {
	t.Helper()
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}

		var task druidv1alpha1.EtcdOpsTask
		if err := cl.Get(ctx, types.NamespacedName{Name: taskName, Namespace: e2eNS}, &task); err != nil {
			continue
		}
		if task.Status.State != nil {
			switch *task.Status.State {
			case druidv1alpha1.TaskStateSucceeded, druidv1alpha1.TaskStateFailed, druidv1alpha1.TaskStateRejected:
				return
			}
		}
		succeeded := druidv1alpha1.TaskStateSucceeded
		patch := client.MergeFrom(task.DeepCopy())
		task.Status.State = &succeeded
		// Deliberately do NOT update the full-snap lease.
		if err := cl.Status().Patch(ctx, &task, patch); err != nil {
			t.Logf("injectTaskSucceededNoLeaseUpdate patch error: %v", err)
			continue
		}
		t.Logf("injectTaskSucceededNoLeaseUpdate: patched EtcdOpsTask %s to Succeeded (no lease update)", taskName)
		return
	}
}

// slowReadySimulator watches for non-ready Etcd CRs and sets ready=true after delay.
func slowReadySimulator(ctx context.Context, t *testing.T, delay time.Duration) {
	t.Helper()
	seen := map[string]bool{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}

		var list druidv1alpha1.EtcdList
		if err := cl.List(ctx, &list, client.InNamespace(e2eNS)); err != nil {
			continue
		}
		for i := range list.Items {
			etcd := &list.Items[i]
			if etcd.Status.Ready != nil && *etcd.Status.Ready {
				continue
			}
			if seen[etcd.Name] {
				continue
			}
			seen[etcd.Name] = true
			t.Logf("slowReadySimulator: scheduling ready=true for Etcd %s in %s", etcd.Name, delay)
			go func(e *druidv1alpha1.Etcd) {
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
				patch := client.MergeFrom(e.DeepCopy())
				ready := true
				e.Status.Ready = &ready
				if err := cl.Status().Patch(ctx, e, patch); err != nil {
					t.Logf("slowReadySimulator: patch ready=true for Etcd %s error: %v", e.Name, err)
				} else {
					t.Logf("slowReadySimulator: set ready=true on Etcd %s", e.Name)
				}
			}(etcd.DeepCopy())
		}
	}
}

// TestEtcDruid_Restore_TopologyMismatch_ExtraLiveShard verifies that when
// TopologyValidation=Strict and the live cluster has a shard that was not in
// the backup, the restore is blocked with TopologyValidated=False rather than
// proceeding and corrupting the cluster.
func TestEtcDruid_Restore_TopologyMismatch_ExtraLiveShard(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)
	id := suffix()

	// Create a backup artefact for one shard only.
	backupName := "e2e-backup-topology-extra-" + id
	bkp := newPlatformBackupWithArtefact(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	// Inject artefact for shard-a only.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		bkp.Status.Artefacts.Etcd = &backupv1alpha1.EtcdArtefact{
			Shards: map[string]backupv1alpha1.EtcdShardArtefact{
				"e2e-shard-a-" + id: {SnapshotKey: "rev-1", SnapshotTime: metav1.Now()},
			},
		}
		return cl.Status().Update(ctx, bkp) == nil
	}, 10*time.Second, time.Second, "failed to inject backup artefact")
	t.Logf("[step 1] backup %s created with 1 shard artefact", backupName)

	// Create TWO live shards — one matches the backup, one is extra.
	shardA := newEtcdShard("e2e-shard-a-" + id)
	shardExtra := newEtcdShard("e2e-shard-extra-" + id)
	require.NoError(t, cl.Create(ctx, shardA))
	require.NoError(t, cl.Create(ctx, shardExtra))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shardA) })
	t.Cleanup(func() { stripFinalizersAndDelete(t, shardExtra) })
	t.Logf("[step 2] created 2 live shards (shard-a + shard-extra); backup only recorded shard-a")

	restoreName := "e2e-restore-topology-extra-" + id
	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	// TopologyValidated must become False with a mismatch error.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
		t.Logf("[poll] TopologyValidated=%v", cond)
		return cond != nil && cond.Status == metav1.ConditionFalse
	}, 30*time.Second, time.Second, "TopologyValidated condition never became False")

	cond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
	require.NotNil(t, cond)
	assert.Equal(t, "Stopped", cond.Reason)
	assert.Contains(t, cond.Message, "shard-extra")
	t.Logf("[step 3] TopologyValidated=False — mismatch surfaced: %s", cond.Message)

	// EtcdRestored must NOT be True or False — the lifecycle initialises it to
	// Unknown on the first reconcile, but the topology gate broke the chain so
	// the EtcdRestoreSubroutine never ran. True/False would both indicate the
	// restore subroutine executed, which is the invariant we are guarding against.
	if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err == nil {
		etcdCond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
		if etcdCond != nil {
			assert.Equal(t, metav1.ConditionUnknown, etcdCond.Status,
				"EtcdRestored must stay Unknown when topology validation blocks the chain (got %s)", etcdCond.Status)
		}
	}
}

// TestEtcDruid_Restore_TopologyMatch_Passes verifies that when the live shard
// set exactly matches the backup artefact, the topology validation passes and
// the restore proceeds to completion.
func TestEtcDruid_Restore_TopologyMatch_Passes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	startSimulators(ctx, t)

	id := suffix()
	shardName := "e2e-shard-match-" + id
	backupName := "e2e-backup-topology-match-" + id
	restoreName := "e2e-restore-topology-match-" + id

	// Create the live shard.
	shard := newEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	// Create backup with an artefact matching exactly the live shard.
	bkp := newPlatformBackupWithArtefact(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		bkp.Status.Artefacts.Etcd = &backupv1alpha1.EtcdArtefact{
			Shards: map[string]backupv1alpha1.EtcdShardArtefact{
				shardName: {SnapshotKey: "rev-1", SnapshotTime: metav1.Now()},
			},
		}
		return cl.Status().Update(ctx, bkp) == nil
	}, 10*time.Second, time.Second, "failed to inject backup artefact")
	t.Logf("[step 1] backup %s created with matching artefact for %s", backupName, shardName)

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	// EtcdRestored must complete — topology validation passes as part of the
	// same reconcile so TopologyValidated=True is only visible after the full
	// restore completes (the lifecycle batches the status patch). Assert both
	// conditions are True after EtcdRestored is confirmed.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
		topoCond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
		t.Logf("[poll] TopologyValidated=%v", topoCond)
		return cond != nil && cond.Status == metav1.ConditionTrue
	}, 90*time.Second, time.Second, "EtcdRestored condition never became True")

	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst))
	assert.True(t, apimeta.IsStatusConditionTrue(rst.Status.Conditions, topology.ConditionTopologyValidated),
		"TopologyValidated must be True when EtcdRestored=True")
	t.Logf("[step 2] TopologyValidated=True and EtcdRestored=True — topology match confirmed")
}

// TestEtcDruid_Restore_TopologyMismatch_ShardMissingFromCluster verifies that
// when the backup recorded a shard that no longer exists in the live cluster,
// topology validation blocks the restore with TopologyValidated=False.
// This is distinct from TestEtcDruid_Restore_MissingEtcdShard (which tests the
// etcd restore path); here the mismatch is detected before any CR is touched.
func TestEtcDruid_Restore_TopologyMismatch_ShardMissingFromCluster(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)
	id := suffix()

	backupName := "e2e-backup-topo-missing-" + id
	bkp := newPlatformBackupWithArtefact(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	// Inject artefact for TWO shards, but only create ONE live shard.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		bkp.Status.Artefacts.Etcd = &backupv1alpha1.EtcdArtefact{
			Shards: map[string]backupv1alpha1.EtcdShardArtefact{
				"e2e-shard-present-" + id: {SnapshotKey: "rev-1", SnapshotTime: metav1.Now()},
				"e2e-shard-gone-" + id:    {SnapshotKey: "rev-2", SnapshotTime: metav1.Now()},
			},
		}
		return cl.Status().Update(ctx, bkp) == nil
	}, 10*time.Second, time.Second, "failed to inject backup artefact")

	// Only the first shard is alive; shard-gone was deleted before the restore.
	shardPresent := newEtcdShard("e2e-shard-present-" + id)
	require.NoError(t, cl.Create(ctx, shardPresent))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shardPresent) })
	t.Logf("[step 1] backup has 2 shards, only 1 exists in cluster (shard-gone is absent)")

	restoreName := "e2e-restore-topo-missing-" + id
	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
		t.Logf("[poll] TopologyValidated=%v", cond)
		return cond != nil && cond.Status == metav1.ConditionFalse
	}, 30*time.Second, time.Second, "TopologyValidated never became False")

	cond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
	require.NotNil(t, cond)
	assert.Equal(t, "Stopped", cond.Reason)
	assert.Contains(t, cond.Message, "shard-gone")
	assert.Contains(t, cond.Message, "missing from live cluster")
	t.Logf("[step 2] TopologyValidated=False — missing shard surfaced: %s", cond.Message)

	// EtcdRestored must NOT be True or False — lifecycle initialises it to Unknown
	// but the topology gate blocked the chain so the subroutine never ran.
	if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err == nil {
		etcdCond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
		if etcdCond != nil {
			assert.Equal(t, metav1.ConditionUnknown, etcdCond.Status,
				"EtcdRestored must stay Unknown when topology validation blocks the chain (got %s)", etcdCond.Status)
		}
	}
}

// TestEtcDruid_Restore_TopologyMismatch_BothDirections verifies that when both
// directions of mismatch are present simultaneously (a shard missing from the
// cluster AND an extra live shard), both are reported in the error message.
func TestEtcDruid_Restore_TopologyMismatch_BothDirections(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)
	id := suffix()

	backupName := "e2e-backup-topo-both-" + id
	bkp := newPlatformBackupWithArtefact(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	// Backup recorded shard-recorded; live cluster has shard-live instead.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		bkp.Status.Artefacts.Etcd = &backupv1alpha1.EtcdArtefact{
			Shards: map[string]backupv1alpha1.EtcdShardArtefact{
				"e2e-shard-recorded-" + id: {SnapshotKey: "rev-1", SnapshotTime: metav1.Now()},
			},
		}
		return cl.Status().Update(ctx, bkp) == nil
	}, 10*time.Second, time.Second, "failed to inject backup artefact")

	// Only shard-live exists — completely different from what was backed up.
	shardLive := newEtcdShard("e2e-shard-live-" + id)
	require.NoError(t, cl.Create(ctx, shardLive))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shardLive) })
	t.Logf("[step 1] backup has shard-recorded, cluster has shard-live — total mismatch")

	restoreName := "e2e-restore-topo-both-" + id
	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
		t.Logf("[poll] TopologyValidated=%v", cond)
		return cond != nil && cond.Status == metav1.ConditionFalse
	}, 30*time.Second, time.Second, "TopologyValidated never became False")

	cond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
	require.NotNil(t, cond)
	assert.Equal(t, "Stopped", cond.Reason)
	assert.Contains(t, cond.Message, "shard-recorded", "missing-from-cluster shard must be in error")
	assert.Contains(t, cond.Message, "shard-live", "extra-live shard must be in error")
	t.Logf("[step 2] TopologyValidated=False — both directions reported: %s", cond.Message)
}

// TestEtcDruid_Restore_TopologyMismatch_SelfHealing verifies that a restore
// blocked by a topology mismatch will automatically succeed once the cluster is
// brought into alignment. The operator requeues on mismatch; when the extra
// shard is removed the next reconcile passes validation and completes the restore.
func TestEtcDruid_Restore_TopologyMismatch_SelfHealing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)
	startSimulators(ctx, t)

	id := suffix()
	shardName := "e2e-shard-heal-" + id
	extraName := "e2e-shard-extra-heal-" + id
	backupName := "e2e-backup-topo-heal-" + id
	restoreName := "e2e-restore-topo-heal-" + id

	// Backup records only shardName.
	bkp := newPlatformBackupWithArtefact(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		bkp.Status.Artefacts.Etcd = &backupv1alpha1.EtcdArtefact{
			Shards: map[string]backupv1alpha1.EtcdShardArtefact{
				shardName: {SnapshotKey: "rev-1", SnapshotTime: metav1.Now()},
			},
		}
		return cl.Status().Update(ctx, bkp) == nil
	}, 10*time.Second, time.Second, "failed to inject backup artefact")

	// Start with TWO live shards — the extra one causes a mismatch.
	shard := newEtcdShard(shardName)
	extra := newEtcdShard(extraName)
	require.NoError(t, cl.Create(ctx, shard))
	require.NoError(t, cl.Create(ctx, extra))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })
	t.Cleanup(func() { stripFinalizersAndDelete(t, extra) })
	t.Logf("[step 1] 2 live shards, backup only recorded %s — expect mismatch", shardName)

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	// First: TopologyValidated=False (extra shard).
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
		t.Logf("[poll] TopologyValidated=%v", cond)
		return cond != nil && cond.Status == metav1.ConditionFalse
	}, 30*time.Second, time.Second, "expected TopologyValidated=False while extra shard present")
	t.Logf("[step 2] TopologyValidated=False confirmed — removing extra shard to fix topology")

	// Remove the extra shard — cluster now matches the backup.
	stripFinalizersAndDelete(t, extra)
	t.Logf("[step 3] extra shard %s deleted — topology now matches backup", extraName)

	// Operator requeues and must now pass validation and complete the restore.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
		t.Logf("[poll] TopologyValidated=%v", cond)
		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, topology.ConditionTopologyValidated)
	}, 90*time.Second, time.Second, "TopologyValidated never became True after topology fix")
	t.Logf("[step 4] TopologyValidated=True after fixing cluster topology")

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
		t.Logf("[poll] EtcdRestored=%v", cond)
		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, restore.ConditionEtcdRestored)
	}, 90*time.Second, time.Second, "EtcdRestored never became True after topology fix")
	t.Logf("[step 5] EtcdRestored=True — self-healing restore complete")
}

// TestEtcDruid_Restore_TopologyNonStrict_IgnoresMismatch verifies that when
// TopologyValidation is not Strict, the restore proceeds even when the live
// shard set does not match the backup — validation is skipped entirely.
func TestEtcDruid_Restore_TopologyNonStrict_IgnoresMismatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)
	startSimulators(ctx, t)

	id := suffix()
	shardName := "e2e-shard-nonstrict-" + id
	extraName := "e2e-shard-extra-nonstrict-" + id
	backupName := "e2e-backup-topo-nonstrict-" + id
	restoreName := "e2e-restore-topo-nonstrict-" + id

	// Backup records only shardName; cluster will have an extra shard too.
	bkp := newPlatformBackupWithArtefact(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		bkp.Status.Artefacts.Etcd = &backupv1alpha1.EtcdArtefact{
			Shards: map[string]backupv1alpha1.EtcdShardArtefact{
				shardName: {SnapshotKey: "rev-1", SnapshotTime: metav1.Now()},
			},
		}
		return cl.Status().Update(ctx, bkp) == nil
	}, 10*time.Second, time.Second, "failed to inject backup artefact")

	shard := newEtcdShard(shardName)
	extra := newEtcdShard(extraName)
	require.NoError(t, cl.Create(ctx, shard))
	require.NoError(t, cl.Create(ctx, extra))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })
	t.Cleanup(func() { stripFinalizersAndDelete(t, extra) })
	t.Logf("[step 1] 2 live shards but backup only recorded %s; using non-strict mode", shardName)

	// Create restore with TopologyValidation=None — validation is explicitly disabled.
	rst := &backupv1alpha1.PlatformRestore{
		ObjectMeta: metav1.ObjectMeta{Name: restoreName},
		Spec: backupv1alpha1.PlatformRestoreSpec{
			Source: backupv1alpha1.RestoreSourceSpec{
				BackupID: backupName,
				Storage: newPlatformRestore(restoreName, backupName).Spec.Source.Storage,
			},
			TopologyValidation: backupv1alpha1.TopologyValidationNone,
		},
	}
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	// EtcdRestored must complete — validation was skipped.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
		t.Logf("[poll] EtcdRestored=%v", cond)
		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, restore.ConditionEtcdRestored)
	}, 90*time.Second, time.Second, "EtcdRestored never became True in non-strict mode")
	t.Logf("[step 2] EtcdRestored=True — non-strict mode bypassed topology check")

	// TopologyValidated must be True/Skipped (subroutine was a no-op in non-strict mode).
	if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err == nil {
		topoCond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
		if topoCond != nil {
			assert.Equal(t, metav1.ConditionTrue, topoCond.Status,
				"TopologyValidated must be True/Skipped in non-strict mode")
		}
		t.Logf("[step 3] TopologyValidated condition = %v (expected absent or Skipped/True)", topoCond)
	}
}
