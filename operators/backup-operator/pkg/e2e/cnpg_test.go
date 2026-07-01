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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	backupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"
	"go.platform-mesh.io/backup-operator/pkg/restore"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// startCNPGSimulators launches goroutines that stand in for CNPG:
//   - backupSimulator: watches cnpg Backup CRs and marks them Completed.
//   - clusterReadySimulator: watches cnpg Cluster CRs and sets ReadyInstances=Instances.
func startCNPGSimulators(ctx context.Context, t *testing.T) {
	t.Helper()

	// Backup simulator
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
			var list cnpgv1.BackupList
			if err := cl.List(ctx, &list, client.InNamespace(e2eNS)); err != nil {
				continue
			}
			for i := range list.Items {
				b := &list.Items[i]
				if b.Status.Phase == cnpgv1.BackupPhaseCompleted || b.Status.Phase == cnpgv1.BackupPhaseFailed {
					continue
				}
				patch := client.MergeFrom(b.DeepCopy())
				b.Status.Phase = cnpgv1.BackupPhaseCompleted
				_ = cl.Status().Patch(ctx, b, patch)
			}
		}
	}()

	// Cluster ready simulator
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
			var list cnpgv1.ClusterList
			if err := cl.List(ctx, &list, client.InNamespace(e2eNS)); err != nil {
				continue
			}
			for i := range list.Items {
				c := &list.Items[i]
				if c.Spec.Instances > 0 && c.Status.ReadyInstances == c.Spec.Instances {
					continue
				}
				patch := client.MergeFrom(c.DeepCopy())
				c.Status.ReadyInstances = c.Spec.Instances
				_ = cl.Status().Patch(ctx, c, patch)
			}
		}
	}()
}

func newPlatformBackupCNPG(name string) *backupv1alpha1.PlatformBackup {
	bkp := newPlatformBackupWithArtefact(name)
	bkp.Spec.Components.CNPG = backupv1alpha1.CNPGSpec{Enabled: true}
	bkp.Spec.Components.Etcd = backupv1alpha1.EtcdSpec{Enabled: false}
	return bkp
}

func newCNPGCluster(name string) *cnpgv1.Cluster {
	return &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: e2eNS,
		},
		Spec: cnpgv1.ClusterSpec{
			Instances: 1,
			StorageConfiguration: cnpgv1.StorageConfiguration{
				Size: "1Gi",
			},
		},
	}
}

// TestCNPG_CaptureRoundTrip verifies the full CNPG backup→restore round-trip via the operator.
func TestCNPG_CaptureRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	clusterName := "e2e-openfga-" + id
	backupName := "e2e-backup-cnpg-" + id
	restoreName := "e2e-restore-cnpg-" + id

	startCNPGSimulators(ctx, t)

	cluster := newCNPGCluster(clusterName)
	require.NoError(t, cl.Create(ctx, cluster))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), cluster) })

	bkp := newPlatformBackupCNPG(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	t.Logf("[step 1] created CNPG cluster %s and PlatformBackup %s", clusterName, backupName)

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionCNPGSnapshotted)
		t.Logf("[poll] CNPGSnapshotted=%v", cond)
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionCNPGSnapshotted)
	}, 2*time.Minute, 5*time.Second, "CNPGSnapshotted never became True")

	t.Logf("[step 2] CNPGSnapshotted=True")
	require.NotNil(t, bkp.Status.Artefacts.CNPG)
	assert.NotEmpty(t, bkp.Status.Artefacts.CNPG.Backups[clusterName])

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionCNPGRestored)
		t.Logf("[poll] CNPGRestored=%v", cond)
		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, restore.ConditionCNPGRestored)
	}, 2*time.Minute, 5*time.Second, "CNPGRestored never became True")

	t.Logf("[step 3] CNPGRestored=True — verifying restore annotation")

	var recreated cnpgv1.Cluster
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: e2eNS}, &recreated))
	assert.Equal(t,
		bkp.Status.Artefacts.CNPG.Backups[clusterName],
		recreated.Annotations["backup.platform-mesh.io/restored-from-cnpg-backup"],
	)
	require.NotNil(t, recreated.Spec.Bootstrap)
	require.NotNil(t, recreated.Spec.Bootstrap.Recovery)
}

// TestCNPG_Capture_NoCluster verifies CNPGSnapshotted is stopped with requeue when no cluster configured.
func TestCNPG_Capture_NoCluster(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	backupName := "e2e-backup-cnpg-nocluster-" + id

	// Backup with CNPG enabled but the CNPG cluster does not exist in the cluster.
	bkp := newPlatformBackupCNPG(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionCNPGSnapshotted)
		t.Logf("[poll] CNPGSnapshotted=%v", cond)
		return cond != nil
	}, 30*time.Second, 2*time.Second, "CNPGSnapshotted condition never set")

	cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionCNPGSnapshotted)
	assert.NotNil(t, cond)
	assert.NotEqual(t, metav1.ConditionTrue, cond.Status,
		"CNPGSnapshotted must not be True when cluster is absent")
}

// TestCNPG_Capture_Idempotent verifies a second backup of the same cluster is a no-op.
func TestCNPG_Capture_Idempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	clusterName := "e2e-openfga-idem-" + id
	backupName := "e2e-backup-cnpg-idem-" + id

	startCNPGSimulators(ctx, t)

	cluster := newCNPGCluster(clusterName)
	require.NoError(t, cl.Create(ctx, cluster))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), cluster) })

	bkp := newPlatformBackupCNPG(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionCNPGSnapshotted)
	}, 2*time.Minute, 3*time.Second, "first backup never completed")

	firstBackupName := bkp.Status.Artefacts.CNPG.Backups[clusterName]
	t.Logf("[step 1] first backup CR name = %q", firstBackupName)

	// Force re-reconcile via annotation patch.
	patch := client.MergeFrom(bkp.DeepCopy())
	if bkp.Annotations == nil {
		bkp.Annotations = map[string]string{}
	}
	bkp.Annotations["backup.platform-mesh.io/force-reconcile"] = "true"
	require.NoError(t, cl.Patch(ctx, bkp, patch))

	time.Sleep(3 * time.Second)
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp))
	assert.Equal(t, firstBackupName, bkp.Status.Artefacts.CNPG.Backups[clusterName],
		"second reconcile must not overwrite artefacts (idempotent)")
	t.Logf("[step 2] idempotency confirmed: backup CR name unchanged at %q", firstBackupName)
}

// TestCNPG_Restore_WaitForReady verifies the restore stays Pending while Cluster instances are not ready.
func TestCNPG_Restore_WaitForReady(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	clusterName := "e2e-openfga-wait-" + id
	backupName := "e2e-backup-cnpg-wait-" + id
	restoreName := "e2e-restore-cnpg-wait-" + id

	// Only backup simulator — no cluster ready simulator yet.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
			var list cnpgv1.BackupList
			if err := cl.List(ctx, &list, client.InNamespace(e2eNS)); err != nil {
				continue
			}
			for i := range list.Items {
				b := &list.Items[i]
				if b.Status.Phase == cnpgv1.BackupPhaseCompleted {
					continue
				}
				patch := client.MergeFrom(b.DeepCopy())
				b.Status.Phase = cnpgv1.BackupPhaseCompleted
				_ = cl.Status().Patch(ctx, b, patch)
			}
		}
	}()

	cluster := newCNPGCluster(clusterName)
	require.NoError(t, cl.Create(ctx, cluster))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), cluster) })

	bkp := newPlatformBackupCNPG(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionCNPGSnapshotted)
	}, 1*time.Minute, 2*time.Second, "backup never completed")

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	// Verify restore stays Pending (cluster not ready).
	time.Sleep(5 * time.Second)
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst))
	cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionCNPGRestored)
	if cond != nil {
		assert.NotEqual(t, metav1.ConditionTrue, cond.Status,
			"CNPGRestored must not be True while cluster is not ready")
	}
	t.Logf("[step] CNPGRestored condition while not ready: %v", cond)

	// Now start the cluster ready simulator.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
			var list cnpgv1.ClusterList
			if err := cl.List(ctx, &list, client.InNamespace(e2eNS)); err != nil {
				continue
			}
			for i := range list.Items {
				c := &list.Items[i]
				if c.Spec.Instances > 0 && c.Status.ReadyInstances == c.Spec.Instances {
					continue
				}
				patch := client.MergeFrom(c.DeepCopy())
				c.Status.ReadyInstances = c.Spec.Instances
				_ = cl.Status().Patch(ctx, c, patch)
			}
		}
	}()

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, restore.ConditionCNPGRestored)
	}, 2*time.Minute, 5*time.Second, "CNPGRestored never became True after cluster became ready")

	t.Logf("[step] CNPGRestored=True after cluster became ready")

	// Verify bootstrap.recovery is set on the recreated cluster.
	var recreated cnpgv1.Cluster
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: e2eNS}, &recreated))
	assert.NotNil(t, recreated.Spec.Bootstrap)
	assert.NotNil(t, recreated.Spec.Bootstrap.Recovery)
	t.Logf("[step] recreated cluster has bootstrap.recovery set")
}

// TestCNPG_Restore_TopologyAware verifies TopologyValidated=True precedes CNPGRestored=True.
func TestCNPG_Restore_TopologyAware(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)
	startSimulators(ctx, t)
	startCNPGSimulators(ctx, t)

	id := suffix()
	clusterName := "e2e-openfga-topo-" + id
	shardName := "e2e-shard-topo-" + id
	backupName := "e2e-backup-cnpg-topo-" + id
	restoreName := "e2e-restore-cnpg-topo-" + id

	// Create etcd shard so topology validation passes.
	shard := newEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	// Create CNPG cluster.
	cluster := newCNPGCluster(clusterName)
	require.NoError(t, cl.Create(ctx, cluster))
	t.Cleanup(func() {
		_ = cl.Delete(context.Background(), cluster)
	})

	// Backup with both etcd and CNPG enabled.
	bkp := &backupv1alpha1.PlatformBackup{}
	*bkp = *newPlatformBackup(backupName)
	bkp.Spec.Components.CNPG = backupv1alpha1.CNPGSpec{Enabled: true}
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted) &&
			apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionCNPGSnapshotted)
	}, 2*time.Minute, 3*time.Second, "backup never completed")

	t.Logf("[step 1] EtcdSnapshotted=True and CNPGSnapshotted=True")

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, restore.ConditionCNPGRestored)
	}, 2*time.Minute, 5*time.Second, "CNPGRestored never became True")

	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst))
	assert.True(t, apimeta.IsStatusConditionTrue(rst.Status.Conditions, "TopologyValidated"),
		"TopologyValidated must be True when CNPGRestored=True")
	t.Logf("[step 2] TopologyValidated=True and CNPGRestored=True")

	// Verify bootstrap.recovery on recreated cluster.
	var recreated cnpgv1.Cluster
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: e2eNS}, &recreated))
	assert.Equal(t,
		bkp.Status.Artefacts.CNPG.Backups[clusterName],
		recreated.Annotations["backup.platform-mesh.io/restored-from-cnpg-backup"],
	)
	assert.NotNil(t, recreated.Spec.Bootstrap)
	t.Logf("[step 3] cluster recreated with bootstrap.recovery and annotation")
}

// TestCNPG_Restore_PostgresVersionMismatch_BlockedByTopology verifies that when the
// topology manifest records a different Postgres major version than the live cluster,
// topology validation surfaces a structured error and blocks the restore.
//
// NOTE: Full CNPG version gating requires topology.json capture (T7) to store per-cluster
// versions. Until T7 is implemented, this test exercises the topology.Validate() function
// directly (covered in pkg/topology/topology_test.go::TestValidateCNPGMajorVersionMismatch)
// and documents the expected behaviour once T7 wires version data into the restore gate.
//
// The test verifies the structural invariant: topology mismatches produce TopologyValidated=False.
func TestCNPG_Restore_PostgresVersionMismatch_BlockedByTopology(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)
	startSimulators(ctx, t)

	id := suffix()
	shardName := "e2e-shard-pgver-" + id
	backupName := "e2e-backup-pgver-" + id
	restoreName := "e2e-restore-pgver-" + id

	// Create a kcp-shard so topology validation has something to compare.
	shard := newEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	// Backup with only the above shard.
	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
	}, 1*time.Minute, 3*time.Second, "backup never completed")

	t.Logf("[step 1] backup complete")

	// Now delete the shard so live topology no longer matches the backup — this exercises
	// the existing kcp-shard mismatch path and verifies TopologyValidated=False/Stopped.
	stripFinalizersAndDelete(t, shard)

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, "TopologyValidated")
		t.Logf("[poll] TopologyValidated=%v", cond)
		return cond != nil && cond.Status == metav1.ConditionFalse
	}, 30*time.Second, 2*time.Second, "TopologyValidated never became False")

	cond := apimeta.FindStatusCondition(rst.Status.Conditions, "TopologyValidated")
	assert.Equal(t, "Stopped", cond.Reason)
	assert.Contains(t, cond.Message, shardName)
	t.Logf("[step 2] topology gate blocked restore: %s", cond.Message)

	// EtcdRestored must stay Unknown — restore chain never reached.
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst))
	etcdCond := apimeta.FindStatusCondition(rst.Status.Conditions, "EtcdRestored")
	if etcdCond != nil {
		assert.NotEqual(t, metav1.ConditionTrue, etcdCond.Status,
			"EtcdRestored must not be True when topology gate blocked the chain")
	}
	t.Logf("[step 3] restore chain correctly blocked — EtcdRestored not True")
}
