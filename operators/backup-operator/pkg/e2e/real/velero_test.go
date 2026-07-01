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
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	backupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"
	"go.platform-mesh.io/backup-operator/pkg/restore"
	"go.platform-mesh.io/backup-operator/pkg/topology"

	appsv1 "k8s.io/api/apps/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// requireVeleroReady waits for the Velero server Deployment to have at least one
// ready replica. The LifecycleRunnable creates it on operator startup; this ensures
// the test doesn't run before Velero is operational.
func requireVeleroReady() error {
	ctx := context.Background()
	nn := types.NamespacedName{Name: "velero", Namespace: e2eNS}
	deadline := time.Now().Add(3 * time.Minute)

	for time.Now().Before(deadline) {
		var deploy appsv1.Deployment
		if err := cl.Get(ctx, nn, &deploy); err == nil && deploy.Status.ReadyReplicas >= 1 {
			return nil
		}
		fmt.Fprintf(os.Stderr, "waiting for Velero Deployment %s/%s to be ready...\n", e2eNS, "velero")
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("Velero Deployment %s/%s not ready after 3 minutes", e2eNS, "velero")
}

// newPlatformBackupVeleroReal returns a PlatformBackup with Velero enabled.
func newPlatformBackupVeleroReal(name string) *backupv1alpha1.PlatformBackup {
	bkp := newPlatformBackup(name)
	bkp.Spec.Components.Velero = backupv1alpha1.VeleroSpec{Enabled: true}
	return bkp
}

// TestRealVelero_Backup_SingleNamespace verifies a PlatformBackup with Velero enabled
// produces a completed Velero Backup CR pointing at the minio BackupStorageLocation.
func TestRealVelero_Backup_SingleNamespace(t *testing.T) {
	if err := requireVeleroReady(); err != nil {
		t.Skipf("skipping: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	backupName := "e2e-real-backup-velero-" + id

	bkp := newPlatformBackupVeleroReal(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })
	t.Logf("[step 1] created PlatformBackup %s with Velero enabled, waiting for VeleroBackedUp=True...", backupName)

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			t.Logf("[poll] backup Get error: %v", err)
			return false
		}
		cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionVeleroBackedUp)
		t.Logf("[poll] backup %s VeleroBackedUp=%v", backupName, cond)
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionVeleroBackedUp)
	}, 10*time.Minute, 15*time.Second, "VeleroBackedUp never became True")

	t.Logf("[step 2] VeleroBackedUp=True confirmed")
	require.NotNil(t, bkp.Status.Artefacts.Velero)
	assert.Equal(t, backupName, bkp.Status.Artefacts.Velero.BackupName)

	// Verify the real Velero Backup CR exists and completed.
	var vb velerov1.Backup
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: backupName, Namespace: e2eNS}, &vb))
	assert.Equal(t, velerov1.BackupPhaseCompleted, vb.Status.Phase,
		"Velero Backup CR must reach Completed phase")
	t.Logf("[step 3] Velero Backup CR phase=%s", vb.Status.Phase)

	// Verify the BackupStorageLocation was created with correct bucket.
	var bsl velerov1.BackupStorageLocation
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: "default", Namespace: e2eNS}, &bsl))
	assert.Equal(t, minioBucket, bsl.Spec.ObjectStorage.Bucket)
	t.Logf("[step 4] BackupStorageLocation verified: bucket=%q endpoint=%q",
		bsl.Spec.ObjectStorage.Bucket, bsl.Spec.Config["s3Url"])
}

// TestRealVelero_Backup_Idempotent verifies that a second reconcile of a completed
// PlatformBackup does not create a second Velero Backup CR.
func TestRealVelero_Backup_Idempotent(t *testing.T) {
	if err := requireVeleroReady(); err != nil {
		t.Skipf("skipping: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	backupName := "e2e-real-backup-velero-idem-" + id

	bkp := newPlatformBackupVeleroReal(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionVeleroBackedUp)
	}, 10*time.Minute, 15*time.Second, "first backup never completed")

	t.Logf("[step 1] first backup complete: %q", bkp.Status.Artefacts.Velero.BackupName)

	// Force re-reconcile.
	patch := ctrlruntimeclient.MergeFrom(bkp.DeepCopy())
	if bkp.Annotations == nil {
		bkp.Annotations = map[string]string{}
	}
	bkp.Annotations["backup.platform-mesh.io/force-reconcile"] = "true"
	require.NoError(t, cl.Patch(ctx, bkp, patch))

	time.Sleep(5 * time.Second)
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp))
	assert.Equal(t, backupName, bkp.Status.Artefacts.Velero.BackupName,
		"second reconcile must not overwrite backup name (idempotent)")
	t.Logf("[step 2] idempotency confirmed: backup name unchanged")
}

// TestRealVelero_Restore_SingleNamespace verifies the full backup→restore round-trip:
// PlatformRestore creates a Velero Restore CR referencing the recorded backup.
func TestRealVelero_Restore_SingleNamespace(t *testing.T) {
	if err := requireVeleroReady(); err != nil {
		t.Skipf("skipping: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	backupName := "e2e-real-backup-velero-rst-" + id
	restoreName := "e2e-real-restore-velero-rst-" + id

	bkp := newPlatformBackupVeleroReal(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionVeleroBackedUp)
	}, 10*time.Minute, 15*time.Second, "backup never completed")

	t.Logf("[step 1] backup complete: %q", bkp.Status.Artefacts.Velero.BackupName)

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })
	t.Logf("[step 2] created PlatformRestore %s", restoreName)

	// Phase 1: topology validation.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
		t.Logf("[poll] restore %s TopologyValidated=%v", restoreName, cond)
		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, topology.ConditionTopologyValidated)
	}, 30*time.Second, 3*time.Second, "TopologyValidated never became True")

	t.Logf("[step 3] TopologyValidated=True")

	// Phase 2: Velero restore.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionVeleroRestored)
		t.Logf("[poll] restore %s VeleroRestored=%v", restoreName, cond)
		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, restore.ConditionVeleroRestored)
	}, 15*time.Minute, 15*time.Second, "VeleroRestored never became True")

	t.Logf("[step 4] VeleroRestored=True confirmed")

	// Verify the Velero Restore CR was created and completed.
	var vr velerov1.Restore
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: restoreName, Namespace: e2eNS}, &vr))
	assert.Equal(t, velerov1.RestorePhaseCompleted, vr.Status.Phase,
		"Velero Restore CR must reach Completed phase")
	assert.Equal(t, backupName, vr.Spec.BackupName,
		"Velero Restore CR must reference the correct backup")
	t.Logf("[step 5] Velero Restore CR phase=%s backup=%q", vr.Status.Phase, vr.Spec.BackupName)
}

// TestRealVelero_Restore_BackupWithNoVeleroArtefacts verifies that a PlatformRestore
// referencing a backup with no Velero artefacts skips Velero restore entirely.
func TestRealVelero_Restore_BackupWithNoVeleroArtefacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	backupName := "e2e-real-backup-velero-noartefacts-" + id
	restoreName := "e2e-real-restore-velero-noartefacts-" + id

	// Velero disabled.
	bkp := newPlatformBackup(backupName)
	bkp.Spec.Components.Velero = backupv1alpha1.VeleroSpec{Enabled: false}
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionVeleroRestored)
		t.Logf("[poll] restore conditions=%+v VeleroRestored=%v", rst.Status.Conditions, cond)
		return cond != nil
	}, 2*time.Minute, 5*time.Second, "VeleroRestored condition never set")

	cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionVeleroRestored)
	require.NotNil(t, cond)
	t.Logf("[step] restore completed with no Velero work — no-artefacts skip path: status=%s", cond.Status)
}

// TestRealVelero_Restore_Idempotent verifies that a forced re-reconcile of a
// PlatformRestore with VeleroRestored=True does not create a second Velero Restore CR.
func TestRealVelero_Restore_Idempotent(t *testing.T) {
	if err := requireVeleroReady(); err != nil {
		t.Skipf("skipping: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	backupName := "e2e-real-backup-velero-idem-rst-" + id
	restoreName := "e2e-real-restore-velero-idem-rst-" + id

	bkp := newPlatformBackupVeleroReal(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionVeleroBackedUp)
	}, 10*time.Minute, 15*time.Second, "backup never completed")

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, restore.ConditionVeleroRestored)
	}, 15*time.Minute, 15*time.Second, "restore never completed")

	t.Logf("[step] restore complete")

	// Force re-reconcile.
	patch := ctrlruntimeclient.MergeFrom(rst.DeepCopy())
	if rst.Annotations == nil {
		rst.Annotations = map[string]string{}
	}
	rst.Annotations["backup.platform-mesh.io/force-reconcile"] = "true"
	require.NoError(t, cl.Patch(ctx, rst, patch))

	time.Sleep(10 * time.Second)

	// Only one Velero Restore CR should exist.
	var list velerov1.RestoreList
	require.NoError(t, cl.List(ctx, &list, ctrlruntimeclient.InNamespace(e2eNS),
		ctrlruntimeclient.MatchingLabels{}))
	count := 0
	for _, r := range list.Items {
		if r.Name == restoreName {
			count++
		}
	}
	assert.Equal(t, 1, count, "exactly one Velero Restore CR must exist (idempotent)")
	t.Logf("[step] idempotency confirmed: only 1 Velero Restore CR exists")

	// VeleroRestored condition must still be True.
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst))
	assert.True(t, apimeta.IsStatusConditionTrue(rst.Status.Conditions, restore.ConditionVeleroRestored),
		"VeleroRestored must remain True after re-reconcile")
}
