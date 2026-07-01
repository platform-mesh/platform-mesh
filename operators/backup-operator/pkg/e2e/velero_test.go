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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	backupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"
	"go.platform-mesh.io/backup-operator/pkg/restore"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// startVeleroSimulators launches goroutines that stand in for Velero:
//   - backupSimulator: watches velero Backup CRs and marks them Completed.
//   - restoreSimulator: watches velero Restore CRs and marks them Completed.
func startVeleroSimulators(ctx context.Context, t *testing.T) {
	t.Helper()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
			var list velerov1.BackupList
			if err := cl.List(ctx, &list, client.InNamespace(e2eNS)); err != nil {
				continue
			}
			for i := range list.Items {
				b := &list.Items[i]
				if b.Status.Phase == velerov1.BackupPhaseCompleted || b.Status.Phase == velerov1.BackupPhaseFailed {
					continue
				}
				patch := client.MergeFrom(b.DeepCopy())
				b.Status.Phase = velerov1.BackupPhaseCompleted
				_ = cl.Patch(ctx, b, patch)
			}
		}
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
			var list velerov1.RestoreList
			if err := cl.List(ctx, &list, client.InNamespace(e2eNS)); err != nil {
				continue
			}
			for i := range list.Items {
				r := &list.Items[i]
				if r.Status.Phase == velerov1.RestorePhaseCompleted || r.Status.Phase == velerov1.RestorePhaseFailed {
					continue
				}
				patch := client.MergeFrom(r.DeepCopy())
				r.Status.Phase = velerov1.RestorePhaseCompleted
				_ = cl.Patch(ctx, r, patch)
			}
		}
	}()
}

func newPlatformBackupVeleroE2E(name string) *backupv1alpha1.PlatformBackup {
	bkp := newPlatformBackupWithArtefact(name)
	bkp.Spec.Components.Velero = backupv1alpha1.VeleroSpec{Enabled: true}
	bkp.Spec.Components.Etcd = backupv1alpha1.EtcdSpec{Enabled: false}
	bkp.Spec.Components.CNPG = backupv1alpha1.CNPGSpec{Enabled: false}
	return bkp
}

// TestVelero_CaptureRoundTrip verifies the full Velero backup→restore round-trip via the operator.
func TestVelero_CaptureRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)
	startVeleroSimulators(ctx, t)

	id := suffix()
	backupName := "e2e-backup-velero-" + id
	restoreName := "e2e-restore-velero-" + id

	bkp := newPlatformBackupVeleroE2E(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	t.Logf("[step 1] created PlatformBackup %s with Velero enabled", backupName)

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionVeleroBackedUp)
		t.Logf("[poll] VeleroBackedUp=%v", cond)
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionVeleroBackedUp)
	}, 2*time.Minute, 5*time.Second, "VeleroBackedUp never became True")

	t.Logf("[step 2] VeleroBackedUp=True")
	require.NotNil(t, bkp.Status.Artefacts.Velero)
	assert.Equal(t, backupName, bkp.Status.Artefacts.Velero.BackupName)

	// Verify Velero Backup CR exists.
	var vb velerov1.Backup
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: backupName, Namespace: e2eNS}, &vb))
	assert.Equal(t, velerov1.BackupPhaseCompleted, vb.Status.Phase)

	// Verify BackupStorageLocation was created.
	var bsl velerov1.BackupStorageLocation
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: "default", Namespace: e2eNS}, &bsl))
	assert.Equal(t, "backups", bsl.Spec.ObjectStorage.Bucket)

	t.Logf("[step 3] Velero BackupStorageLocation and Backup CR confirmed")

	// Restore
	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionVeleroRestored)
		t.Logf("[poll] VeleroRestored=%v", cond)
		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, restore.ConditionVeleroRestored)
	}, 2*time.Minute, 5*time.Second, "VeleroRestored never became True")

	t.Logf("[step 4] VeleroRestored=True")

	// Verify Velero Restore CR exists.
	var vr velerov1.Restore
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: restoreName, Namespace: e2eNS}, &vr))
	assert.Equal(t, velerov1.RestorePhaseCompleted, vr.Status.Phase)
	assert.Equal(t, backupName, vr.Spec.BackupName)
	t.Logf("[step 5] Velero Restore CR confirmed: backup=%q", vr.Spec.BackupName)
}

// TestVelero_Capture_Idempotent verifies a second backup does not create a new Velero Backup CR.
func TestVelero_Capture_Idempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)
	startVeleroSimulators(ctx, t)

	id := suffix()
	backupName := "e2e-backup-velero-idem-" + id

	bkp := newPlatformBackupVeleroE2E(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionVeleroBackedUp)
	}, 2*time.Minute, 3*time.Second, "first backup never completed")

	t.Logf("[step 1] first backup complete: %q", bkp.Status.Artefacts.Velero.BackupName)

	// Force re-reconcile.
	patch := client.MergeFrom(bkp.DeepCopy())
	if bkp.Annotations == nil {
		bkp.Annotations = map[string]string{}
	}
	bkp.Annotations["backup.platform-mesh.io/force-reconcile"] = "true"
	require.NoError(t, cl.Patch(ctx, bkp, patch))

	time.Sleep(3 * time.Second)
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp))
	assert.Equal(t, backupName, bkp.Status.Artefacts.Velero.BackupName,
		"idempotency: backup name must not change on second reconcile")
	t.Logf("[step 2] idempotency confirmed")
}

// TestVelero_Backup_NoShards verifies VeleroBackedUp=False when Velero is disabled.
func TestVelero_Backup_Disabled(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	backupName := "e2e-backup-velero-disabled-" + id

	// Velero disabled, only CNPG enabled (to satisfy CEL rule).
	bkp := &backupv1alpha1.PlatformBackup{}
	*bkp = *newPlatformBackupWithArtefact(backupName)
	bkp.Spec.Components.Velero = backupv1alpha1.VeleroSpec{Enabled: false}
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	// Wait a moment — no VeleroBackedUp condition should appear.
	time.Sleep(5 * time.Second)
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp))

	cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionVeleroBackedUp)
	if cond != nil {
		assert.NotEqual(t, metav1.ConditionTrue, cond.Status,
			"VeleroBackedUp must not be True when Velero is disabled")
	}
	t.Logf("[step] VeleroBackedUp not True when Velero disabled: cond=%v", cond)
}
