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

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	backupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"
	"go.platform-mesh.io/backup-operator/pkg/restore"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestEtcDruid_CaptureRoundTrip is the primary e2e test. It:
//  1. Creates an Etcd CR labelled as a kcp-shard in the operator's namespace.
//  2. Creates a PlatformBackup with etcd enabled.
//  3. Waits for the operator to set EtcdSnapshotted=True on the backup.
//  4. Asserts that a non-empty snapshot key was recorded in the backup artefacts.
//  5. Creates a PlatformRestore referencing that backup.
//  6. Waits for the operator to set EtcdRestored=True on the restore.
//  7. Asserts the Etcd CR was deleted and recreated with the restore annotation.
//
// Prerequisites (must be running in the cluster before this test):
//   - backup-operator deployment
//   - etcd-druid deployment (processes EtcdOpsTask CRs and updates full-snap leases)
func TestEtcDruid_CaptureRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)
	startSimulators(ctx, t)

	id := suffix()
	shardName := "e2e-shard-" + id
	backupName := "e2e-backup-" + id
	restoreName := "e2e-restore-" + id

	// --- Create the Etcd shard CR -------------------------------------------------

	shard := newEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() {
		stripFinalizersAndDelete(t, shard)
	})

	t.Logf("[step 1] created Etcd CR %s/%s", e2eNS, shardName)

	// --- Create PlatformBackup ---------------------------------------------------

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() {
		_ = cl.Delete(context.Background(), bkp)
	})

	t.Logf("[step 2] created PlatformBackup %s, waiting for EtcdSnapshotted=True...", backupName)

	// Wait for the operator to process the backup. etcd-druid must complete the
	// EtcdOpsTask and update the full-snap lease for this to succeed.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			t.Logf("[poll] Get PlatformBackup error: %v", err)
			return false
		}
		cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
		t.Logf("[poll] backup conditions=%+v EtcdSnapshotted=%v", bkp.Status.Conditions, cond)
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
	}, 8*time.Minute, 10*time.Second, "EtcdSnapshotted condition never became True on backup %s", backupName)

	t.Logf("[step 3] EtcdSnapshotted=True confirmed")

	// Assert the snapshot key was recorded.
	require.NotNil(t, bkp.Status.Artefacts.Etcd, "backup.Status.Artefacts.Etcd is nil after EtcdSnapshotted=True")
	shardArtefact, ok := bkp.Status.Artefacts.Etcd.Shards[shardName]
	require.True(t, ok, "no artefact recorded for shard %s", shardName)
	require.NotEmpty(t, shardArtefact.SnapshotKey, "snapshot key is empty for shard %s", shardName)
	assert.False(t, shardArtefact.SnapshotTime.IsZero(), "snapshot time is zero for shard %s", shardName)

	t.Logf("[step 4] backup complete: shard %s snapshot key = %q", shardName, shardArtefact.SnapshotKey)

	// --- Create PlatformRestore --------------------------------------------------

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() {
		_ = cl.Delete(context.Background(), rst)
	})

	t.Logf("[step 5] created PlatformRestore %s, waiting for EtcdRestored=True...", restoreName)

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			t.Logf("[poll] Get PlatformRestore error: %v", err)
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
		t.Logf("[poll] restore conditions=%+v EtcdRestored=%v", rst.Status.Conditions, cond)
		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, restore.ConditionEtcdRestored)
	}, 8*time.Minute, 10*time.Second, "EtcdRestored condition never became True on restore %s", restoreName)

	t.Logf("[step 6] EtcdRestored=True confirmed")

	// Assert topology validation also passed (it must precede EtcdRestored in the chain).
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst))
	assert.True(t, apimeta.IsStatusConditionTrue(rst.Status.Conditions, restore.ConditionTopologyValidated),
		"TopologyValidated condition must be True when EtcdRestored=True")

	// --- Verify the Etcd CR was recreated with the restore annotation -------------

	var recreated druidv1alpha1.Etcd
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: shardName, Namespace: e2eNS}, &recreated),
		"Etcd CR %s not found after restore", shardName)

	gotKey := recreated.Annotations[restore.AnnotationKeyRestoredFromSnapshot]
	assert.Equal(t, shardArtefact.SnapshotKey, gotKey,
		"restore annotation on Etcd CR does not match recorded snapshot key")
	assert.Equal(t, backup.LabelComponentKCPShard, recreated.Labels[backup.LabelKeyComponent],
		"kcp-shard label missing from recreated Etcd CR")

	t.Logf("[step 7] restore complete: Etcd CR %s recreated with annotation %s=%q",
		shardName, restore.AnnotationKeyRestoredFromSnapshot, gotKey)
}

// TestEtcDruid_Restore_MultiShard verifies that all shards from a multi-shard backup
// are restored concurrently and each recreated Etcd CR carries the correct per-shard
// snapshot annotation.
func TestEtcDruid_Restore_MultiShard(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)
	startSimulators(ctx, t)

	id := suffix()
	shardA := "e2e-shard-a-" + id
	shardB := "e2e-shard-b-" + id
	backupName := "e2e-backup-rst-multi-" + id
	restoreName := "e2e-restore-multi-" + id

	// --- Create two Etcd shards and back them up --------------------------------

	for _, name := range []string{shardA, shardB} {
		shard := newEtcdShard(name)
		require.NoError(t, cl.Create(ctx, shard))
		t.Logf("[step 1] created Etcd shard %s", name)
		capturedName := name
		t.Cleanup(func() {
			stripFinalizersAndDelete(t, &druidv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{Name: capturedName, Namespace: e2eNS},
			})
		})
	}

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Logf("[step 2] created 2-shard PlatformBackup %s", backupName)
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			t.Logf("[poll] Get PlatformBackup error: %v", err)
			return false
		}
		cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
		t.Logf("[poll] backup conditions=%+v EtcdSnapshotted=%v", bkp.Status.Conditions, cond)
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
	}, 8*time.Minute, 10*time.Second, "EtcdSnapshotted never True for backup %s", backupName)

	t.Logf("[step 3] EtcdSnapshotted=True confirmed")

	require.NotNil(t, bkp.Status.Artefacts.Etcd)
	snapshotKeys := make(map[string]string, 2)
	for _, name := range []string{shardA, shardB} {
		artefact, ok := bkp.Status.Artefacts.Etcd.Shards[name]
		require.True(t, ok, "no artefact for shard %s", name)
		require.NotEmpty(t, artefact.SnapshotKey, "empty snapshot key for shard %s", name)
		snapshotKeys[name] = artefact.SnapshotKey
		t.Logf("[step 4] shard %s snapshot key = %q", name, artefact.SnapshotKey)
	}

	// --- Restore from the backup ------------------------------------------------

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Logf("[step 5] created PlatformRestore %s", restoreName)
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			t.Logf("[poll] Get PlatformRestore error: %v", err)
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
		t.Logf("[poll] restore conditions=%+v EtcdRestored=%v", rst.Status.Conditions, cond)
		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, restore.ConditionEtcdRestored)
	}, 8*time.Minute, 10*time.Second, "EtcdRestored never True for restore %s", restoreName)

	t.Logf("[step 6] EtcdRestored=True confirmed")

	// Assert topology validation also passed.
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst))
	assert.True(t, apimeta.IsStatusConditionTrue(rst.Status.Conditions, restore.ConditionTopologyValidated),
		"TopologyValidated condition must be True when EtcdRestored=True")

	// Assert each shard was recreated with the annotation pointing at its own key.
	for _, name := range []string{shardA, shardB} {
		var recreated druidv1alpha1.Etcd
		require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: name, Namespace: e2eNS}, &recreated))
		gotKey := recreated.Annotations[restore.AnnotationKeyRestoredFromSnapshot]
		assert.Equal(t, snapshotKeys[name], gotKey,
			"shard %s: annotation key mismatch: want %q got %q", name, snapshotKeys[name], gotKey)
		t.Logf("[step 7] shard %s restore annotation = %q", name, gotKey)
	}
}

// TestEtcDruid_Capture_MultiShard exercises the fan-out: two shards are snapshotted
// concurrently and both keys must appear in the backup artefacts.
func TestEtcDruid_Capture_MultiShard(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)
	startSimulators(ctx, t)

	id := suffix()
	shardA := "e2e-shard-a-" + id
	shardB := "e2e-shard-b-" + id
	backupName := "e2e-backup-multi-" + id

	for _, name := range []string{shardA, shardB} {
		shard := newEtcdShard(name)
		require.NoError(t, cl.Create(ctx, shard))
		t.Logf("[step 1] created Etcd shard %s", name)
		capturedName := name
		t.Cleanup(func() {
			stripFinalizersAndDelete(t, &druidv1alpha1.Etcd{
				ObjectMeta: metav1.ObjectMeta{Name: capturedName, Namespace: e2eNS},
			})
		})
	}

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	t.Logf("[step 2] created 2-shard backup %s, waiting for EtcdSnapshotted=True...", backupName)

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			t.Logf("[poll] Get PlatformBackup error: %v", err)
			return false
		}
		cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
		t.Logf("[poll] conditions=%+v EtcdSnapshotted=%v", bkp.Status.Conditions, cond)
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
	}, 8*time.Minute, 10*time.Second, "EtcdSnapshotted never True for multi-shard backup %s", backupName)

	t.Logf("[step 3] EtcdSnapshotted=True confirmed")

	require.NotNil(t, bkp.Status.Artefacts.Etcd)
	for _, name := range []string{shardA, shardB} {
		artefact, ok := bkp.Status.Artefacts.Etcd.Shards[name]
		require.True(t, ok, "no artefact for shard %s", name)
		assert.NotEmpty(t, artefact.SnapshotKey, "empty snapshot key for shard %s", name)
		t.Logf("[step 4] shard %s snapshot key = %q", name, artefact.SnapshotKey)
	}
}

// TestEtcDruid_Capture_Idempotent runs Process twice on the same backup and asserts
// the second call is a no-op (no new EtcdOpsTask created, artefacts unchanged).
func TestEtcDruid_Capture_Idempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)
	startSimulators(ctx, t)

	id := suffix()
	shardName := "e2e-shard-idem-" + id
	backupName := "e2e-backup-idem-" + id

	shard := newEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Logf("[step 1] created Etcd shard %s", shardName)
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Logf("[step 2] created PlatformBackup %s", backupName)
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	// Wait for the first successful snapshot.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			t.Logf("[poll] Get PlatformBackup error: %v", err)
			return false
		}
		cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
		t.Logf("[poll] conditions=%+v EtcdSnapshotted=%v", bkp.Status.Conditions, cond)
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
	}, 8*time.Minute, 10*time.Second, "EtcdSnapshotted never True for %s", backupName)

	t.Logf("[step 3] EtcdSnapshotted=True confirmed")

	require.NotNil(t, bkp.Status.Artefacts.Etcd, "backup.Status.Artefacts.Etcd is nil after EtcdSnapshotted=True")
	firstKey := bkp.Status.Artefacts.Etcd.Shards[shardName].SnapshotKey
	require.NotEmpty(t, firstKey)
	t.Logf("[step 4] first snapshot key = %q", firstKey)

	// Force a second reconcile by patching an unrelated annotation on the backup.
	patch := client.MergeFrom(bkp.DeepCopy())
	if bkp.Annotations == nil {
		bkp.Annotations = map[string]string{}
	}
	bkp.Annotations["e2e/force-reconcile"] = "1"
	require.NoError(t, cl.Patch(ctx, bkp, patch))
	t.Logf("[step 5] forced second reconcile via annotation patch, sleeping 10s...")

	// Give the operator time to process the reconcile.
	time.Sleep(10 * time.Second)

	var reread backupv1alpha1.PlatformBackup
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: backupName}, &reread))

	secondKey := reread.Status.Artefacts.Etcd.Shards[shardName].SnapshotKey
	t.Logf("[step 6] second snapshot key = %q (should be unchanged)", secondKey)

	// Snapshot key must be unchanged — no second EtcdOpsTask was triggered.
	assert.Equal(t, firstKey, secondKey,
		"snapshot key changed on second reconcile — idempotency guard failed")
}

// TestEtcDruid_Restore_TopologyAware is the canonical backup+restore test that
// explicitly verifies the full topology-aware condition sequence:
//  1. Backup with one shard → EtcdSnapshotted=True
//  2. Restore with TopologyValidation=Strict against a matching live shard set →
//     TopologyValidated=True (must appear before EtcdRestored)
//  3. EtcdRestored=True
//
// This test proves that topology validation is wired into the restore lifecycle
// and that a successful restore always produces both conditions.
func TestEtcDruid_Restore_TopologyAware(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)
	startSimulators(ctx, t)

	id := suffix()
	shardName := "e2e-shard-topo-" + id
	backupName := "e2e-backup-topo-" + id
	restoreName := "e2e-restore-topo-" + id

	shard := newEtcdShard(shardName)
	require.NoError(t, cl.Create(ctx, shard))
	t.Cleanup(func() { stripFinalizersAndDelete(t, shard) })

	bkp := newPlatformBackup(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionEtcdSnapshotted)
	}, 8*time.Minute, 10*time.Second, "EtcdSnapshotted never True for %s", backupName)
	t.Logf("[step 1] backup complete: EtcdSnapshotted=True")

	rst := newPlatformRestore(restoreName, backupName) // TopologyValidation=Strict
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	// TopologyValidated must become True first.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionTopologyValidated)
		t.Logf("[poll] conditions=%+v TopologyValidated=%v", rst.Status.Conditions, cond)
		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, restore.ConditionTopologyValidated)
	}, 30*time.Second, time.Second, "TopologyValidated never became True for %s", restoreName)
	t.Logf("[step 2] TopologyValidated=True (shard set matched)")

	// Then EtcdRestored must also become True.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionEtcdRestored)
		t.Logf("[poll] EtcdRestored=%v", cond)
		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, restore.ConditionEtcdRestored)
	}, 8*time.Minute, 10*time.Second, "EtcdRestored never True for %s", restoreName)
	t.Logf("[step 3] EtcdRestored=True — topology-aware restore complete")
}

// --- Helpers ------------------------------------------------------------------

func newEtcdShard(name string) *druidv1alpha1.Etcd {
	localProvider := druidv1alpha1.StorageProvider("Local")
	return &druidv1alpha1.Etcd{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: e2eNS,
			Labels: map[string]string{
				backup.LabelKeyComponent: backup.LabelComponentKCPShard,
			},
			// Prevent real etcd-druid from reconciling these test-only CRs.
			// Without this, etcd-druid rejects EtcdOpsTasks with ERR_ETCD_NOT_READY
			// because no real etcd pods back the CR.
			Annotations: map[string]string{
				druidv1alpha1.SuspendEtcdSpecReconcileAnnotation: "true",
			},
		},
		Spec: druidv1alpha1.EtcdSpec{
			Replicas: 1,
			Labels:   map[string]string{"app": name},
			Backup: druidv1alpha1.BackupSpec{
				Store: &druidv1alpha1.StoreSpec{
					Prefix:   name,
					Provider: &localProvider,
				},
			},
		},
	}
}

func newPlatformBackup(name string) *backupv1alpha1.PlatformBackup {
	return &backupv1alpha1.PlatformBackup{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: backupv1alpha1.PlatformBackupSpec{
			Storage: backupv1alpha1.StorageSpec{
				S3: backupv1alpha1.S3StorageSpec{
					Endpoint:       "http://minio:9000",
					Bucket:         "backups",
					CredentialsRef: corev1.LocalObjectReference{Name: "s3-credentials"},
				},
			},
			Components: backupv1alpha1.ComponentsSpec{
				Etcd: backupv1alpha1.EtcdSpec{Enabled: true},
			},
		},
	}
}

// newPlatformBackupWithArtefact creates a backup with Etcd disabled so the
// capture subroutine skips immediately (EtcdSnapshotted=True via skip path).
// This allows tests to inject arbitrary artefacts via Status().Update() without
// the operator overwriting them on each reconcile.
// CNPG is enabled to satisfy the CEL rule requiring at least one component.
// The CNPG subroutine is not yet implemented so it is always a no-op.
func newPlatformBackupWithArtefact(name string) *backupv1alpha1.PlatformBackup {
	return &backupv1alpha1.PlatformBackup{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: backupv1alpha1.PlatformBackupSpec{
			Storage: backupv1alpha1.StorageSpec{
				S3: backupv1alpha1.S3StorageSpec{
					Endpoint:       "http://minio:9000",
					Bucket:         "backups",
					CredentialsRef: corev1.LocalObjectReference{Name: "s3-credentials"},
				},
			},
			Components: backupv1alpha1.ComponentsSpec{
				Etcd: backupv1alpha1.EtcdSpec{Enabled: false},
				CNPG: backupv1alpha1.CNPGSpec{Enabled: true},
			},
		},
	}
}

func newPlatformRestore(name, backupID string) *backupv1alpha1.PlatformRestore {
	return &backupv1alpha1.PlatformRestore{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: backupv1alpha1.PlatformRestoreSpec{
			Source: backupv1alpha1.RestoreSourceSpec{
				BackupID: backupID,
				Storage: backupv1alpha1.StorageSpec{
					S3: backupv1alpha1.S3StorageSpec{
						Endpoint:       "http://minio:9000",
						Bucket:         "backups",
						CredentialsRef: corev1.LocalObjectReference{Name: "s3-credentials"},
					},
				},
			},
			TopologyValidation: backupv1alpha1.TopologyValidationStrict,
		},
	}
}

// stripFinalizersAndDelete removes all finalizers from an Etcd CR then deletes it.
// Needed because etcd-druid adds a finalizer; without removal the object stalls in
// Terminating and subsequent test runs cannot recreate the same name.
func stripFinalizersAndDelete(t *testing.T, etcd *druidv1alpha1.Etcd) {
	t.Helper()
	ctx := context.Background()

	var current druidv1alpha1.Etcd
	if err := cl.Get(ctx, types.NamespacedName{Name: etcd.Name, Namespace: e2eNS}, &current); err != nil {
		return // already gone
	}
	if len(current.Finalizers) > 0 {
		patch := client.MergeFrom(current.DeepCopy())
		current.Finalizers = nil
		_ = cl.Patch(ctx, &current, patch)
	}
	_ = cl.Delete(ctx, &current)
}
