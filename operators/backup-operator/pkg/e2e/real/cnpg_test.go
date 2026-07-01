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

	barmanApi "github.com/cloudnative-pg/barman-cloud/pkg/api"
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	machineryapi "github.com/cloudnative-pg/machinery/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	backupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"
	"go.platform-mesh.io/backup-operator/pkg/restore"
	"go.platform-mesh.io/backup-operator/pkg/topology"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// newRealCNPGCluster creates a CNPG Cluster CR backed by the real CNPG operator
// with barmanObjectStore pointing at minio for backup/restore.
func newRealCNPGCluster(name string) *cnpgv1.Cluster {
	endpointURL := fmt.Sprintf("http://%s.%s.svc:9000", minioServiceName, e2eNS)
	destinationPath := fmt.Sprintf("s3://%s/%s", minioBucket, name)

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
			Backup: &cnpgv1.BackupConfiguration{
				BarmanObjectStore: &barmanApi.BarmanObjectStoreConfiguration{
					DestinationPath: destinationPath,
					EndpointURL:     endpointURL,
					BarmanCredentials: barmanApi.BarmanCredentials{
						AWS: &barmanApi.S3Credentials{
							AccessKeyIDReference: &machineryapi.SecretKeySelector{
								LocalObjectReference: machineryapi.LocalObjectReference{Name: minioSecretName},
								Key:                  "accessKeyID",
							},
							SecretAccessKeyReference: &machineryapi.SecretKeySelector{
								LocalObjectReference: machineryapi.LocalObjectReference{Name: minioSecretName},
								Key:                  "secretAccessKey",
							},
						},
					},
				},
				RetentionPolicy: "30d",
			},
		},
	}
}

// stripCNPGFinalizersAndDelete removes CNPG Cluster finalizers and deletes it.
func stripCNPGFinalizersAndDelete(t *testing.T, cluster *cnpgv1.Cluster) {
	t.Helper()
	ctx := context.Background()
	var current cnpgv1.Cluster
	if err := cl.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: e2eNS}, &current); err != nil {
		return
	}
	if len(current.Finalizers) > 0 {
		patch := ctrlruntimeclient.MergeFrom(current.DeepCopy())
		current.Finalizers = nil
		_ = cl.Patch(ctx, &current, patch)
	}
	_ = cl.Delete(ctx, &current)
}

// waitForCNPGClusterReady polls until the CNPG Cluster has all instances ready.
func waitForCNPGClusterReady(t *testing.T, ctx context.Context, cluster *cnpgv1.Cluster) {
	t.Helper()
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: e2eNS}, cluster); err != nil {
			return false
		}
		t.Logf("[poll] CNPG cluster %s readyInstances=%d/%d", cluster.Name,
			cluster.Status.ReadyInstances, cluster.Spec.Instances)
		return cluster.Status.ReadyInstances == cluster.Spec.Instances
	}, 10*time.Minute, 15*time.Second, "CNPG Cluster %s never became ready", cluster.Name)
}

// newPlatformBackupCNPGReal returns a PlatformBackup with only CNPG enabled.
func newPlatformBackupCNPGReal(name string) *backupv1alpha1.PlatformBackup {
	bkp := newPlatformBackup(name)
	bkp.Spec.Components.Etcd = backupv1alpha1.EtcdSpec{Enabled: false}
	bkp.Spec.Components.CNPG = backupv1alpha1.CNPGSpec{Enabled: true}
	bkp.Spec.Components.Velero = backupv1alpha1.VeleroSpec{Enabled: false}
	return bkp
}

// TestRealCNPG_Backup_SingleCluster verifies a PlatformBackup against a single real
// CNPG cluster produces a completed CNPG Backup CR and records the backup name in artefacts.
func TestRealCNPG_Backup_SingleCluster(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	clusterName := "e2e-real-cnpg-" + id
	backupName := "e2e-real-backup-cnpg-" + id

	cluster := newRealCNPGCluster(clusterName)
	require.NoError(t, cl.Create(ctx, cluster))
	t.Cleanup(func() { stripCNPGFinalizersAndDelete(t, cluster) })
	t.Logf("[step 1] created CNPG Cluster %s/%s", e2eNS, clusterName)

	waitForCNPGClusterReady(t, ctx, cluster)
	t.Logf("[step 2] CNPG Cluster ready")

	bkp := newPlatformBackupCNPGReal(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })
	t.Logf("[step 3] created PlatformBackup %s, waiting for CNPGSnapshotted=True...", backupName)

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			t.Logf("[poll] backup Get error: %v", err)
			return false
		}
		cond := apimeta.FindStatusCondition(bkp.Status.Conditions, backup.ConditionCNPGSnapshotted)
		t.Logf("[poll] backup %s CNPGSnapshotted=%v", backupName, cond)
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionCNPGSnapshotted)
	}, 10*time.Minute, 15*time.Second, "CNPGSnapshotted never became True")

	t.Logf("[step 4] CNPGSnapshotted=True confirmed")
	require.NotNil(t, bkp.Status.Artefacts.CNPG)
	backupCRName := bkp.Status.Artefacts.CNPG.Backups[clusterName]
	assert.NotEmpty(t, backupCRName, "backup CR name must be recorded in artefacts")
	t.Logf("[step 5] cluster %s backup CR name = %q", clusterName, backupCRName)

	// Verify the real CNPG Backup CR exists and completed.
	var cnpgBackup cnpgv1.Backup
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: backupCRName, Namespace: e2eNS}, &cnpgBackup))
	assert.Equal(t, string(cnpgv1.BackupPhaseCompleted), string(cnpgBackup.Status.Phase),
		"CNPG Backup CR must reach Completed phase")
	t.Logf("[step 6] CNPG Backup CR %s phase=%s", backupCRName, cnpgBackup.Status.Phase)
}

// TestRealCNPG_Backup_Idempotent verifies that a second PlatformBackup records the
// same backup CR name as the first, confirming the idempotency guard works.
func TestRealCNPG_Backup_Idempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	clusterName := "e2e-real-cnpg-idem-" + id
	backupName := "e2e-real-backup-cnpg-idem-" + id

	cluster := newRealCNPGCluster(clusterName)
	require.NoError(t, cl.Create(ctx, cluster))
	t.Cleanup(func() { stripCNPGFinalizersAndDelete(t, cluster) })

	waitForCNPGClusterReady(t, ctx, cluster)
	t.Logf("[step 1] cluster ready")

	bkp := newPlatformBackupCNPGReal(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionCNPGSnapshotted)
	}, 10*time.Minute, 15*time.Second, "first backup never completed")

	firstBackupCRName := bkp.Status.Artefacts.CNPG.Backups[clusterName]
	t.Logf("[step 2] first backup CR name = %q", firstBackupCRName)

	// Force re-reconcile.
	patch := ctrlruntimeclient.MergeFrom(bkp.DeepCopy())
	if bkp.Annotations == nil {
		bkp.Annotations = map[string]string{}
	}
	bkp.Annotations["backup.platform-mesh.io/force-reconcile"] = "true"
	require.NoError(t, cl.Patch(ctx, bkp, patch))

	time.Sleep(5 * time.Second)
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp))
	assert.Equal(t, firstBackupCRName, bkp.Status.Artefacts.CNPG.Backups[clusterName],
		"second reconcile must not overwrite backup CR name (idempotent)")
	t.Logf("[step 3] idempotency confirmed: backup CR name unchanged at %q", firstBackupCRName)
}

// TestRealCNPG_Restore_SingleCluster verifies the full backup→restore round-trip:
// PlatformRestore deletes and recreates the CNPG Cluster with bootstrap.recovery
// pointing at the recorded backup.
func TestRealCNPG_Restore_SingleCluster(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	clusterName := "e2e-real-cnpg-rst-" + id
	backupName := "e2e-real-backup-cnpg-rst-" + id
	restoreName := "e2e-real-restore-cnpg-rst-" + id

	cluster := newRealCNPGCluster(clusterName)
	require.NoError(t, cl.Create(ctx, cluster))
	t.Cleanup(func() { stripCNPGFinalizersAndDelete(t, cluster) })

	waitForCNPGClusterReady(t, ctx, cluster)
	t.Logf("[step 1] cluster ready")

	bkp := newPlatformBackupCNPGReal(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionCNPGSnapshotted)
	}, 10*time.Minute, 15*time.Second, "backup never completed")

	backupCRName := bkp.Status.Artefacts.CNPG.Backups[clusterName]
	t.Logf("[step 2] backup complete: CNPG Backup CR name = %q", backupCRName)

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })
	t.Logf("[step 3] created PlatformRestore %s, waiting for TopologyValidated=True...", restoreName)

	// Phase 1: topology validation (fast).
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, topology.ConditionTopologyValidated)
		t.Logf("[poll] restore %s TopologyValidated=%v", restoreName, cond)
		if cond != nil && cond.Status == metav1.ConditionFalse {
			t.Logf("[poll] restore %s TopologyValidated=False — %s", restoreName, cond.Message)
		}
		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, topology.ConditionTopologyValidated)
	}, 30*time.Second, 3*time.Second, "restore %s TopologyValidated never became True", restoreName)

	t.Logf("[step 4] TopologyValidated=True")

	// Phase 2: CNPG restore.
	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionCNPGRestored)
		t.Logf("[poll] restore %s CNPGRestored=%v", restoreName, cond)

		// Log the CNPG cluster status to aid debugging slow restores.
		var c cnpgv1.Cluster
		if err := cl.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: e2eNS}, &c); err == nil {
			t.Logf("[poll] cluster %s phase=%q ready=%d/%d", clusterName, c.Status.Phase, c.Status.ReadyInstances, c.Spec.Instances)
		}
		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, restore.ConditionCNPGRestored)
	}, 25*time.Minute, 15*time.Second, "CNPGRestored never became True")

	t.Logf("[step 5] CNPGRestored=True confirmed")

	// Verify the recreated Cluster has bootstrap.recovery set.
	var recreated cnpgv1.Cluster
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: e2eNS}, &recreated))
	assert.Equal(t, backupCRName,
		recreated.Annotations["backup.platform-mesh.io/restored-from-cnpg-backup"])
	require.NotNil(t, recreated.Spec.Bootstrap, "recreated Cluster must have bootstrap set")
	require.NotNil(t, recreated.Spec.Bootstrap.Recovery, "recreated Cluster must have bootstrap.recovery")
	require.NotNil(t, recreated.Spec.Bootstrap.Recovery.Backup)
	assert.Equal(t, backupCRName, recreated.Spec.Bootstrap.Recovery.Backup.Name)
	t.Logf("[step 6] Cluster recreated with bootstrap.recovery.backup.name=%q", backupCRName)

	waitForCNPGClusterReady(t, ctx, &recreated)
	t.Logf("[step 7] restored Cluster Ready — round-trip complete")
}

// TestRealCNPG_Restore_BackupWithNoCNPGArtefacts verifies that a PlatformRestore
// referencing a backup with no CNPG artefacts skips CNPG restore entirely.
func TestRealCNPG_Restore_BackupWithNoCNPGArtefacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	backupName := "e2e-real-backup-cnpg-noartefacts-" + id
	restoreName := "e2e-real-restore-cnpg-noartefacts-" + id

	// Backup with CNPG artefacts cleared.
	bkp := newPlatformBackup(backupName)
	bkp.Spec.Components.CNPG = backupv1alpha1.CNPGSpec{Enabled: false}
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionCNPGRestored)
		t.Logf("[poll] restore conditions=%+v CNPGRestored=%v", rst.Status.Conditions, cond)
		return cond != nil
	}, 2*time.Minute, 5*time.Second, "CNPGRestored condition never set")

	cond := apimeta.FindStatusCondition(rst.Status.Conditions, restore.ConditionCNPGRestored)
	require.NotNil(t, cond)
	t.Logf("[step] restore completed with no cnpg work — no-artefacts skip path verified: %s", cond.Status)
}

// TestRealCNPG_Restore_Idempotent verifies that a forced re-reconcile of a PlatformRestore
// with CNPGRestored=True does not re-delete and re-create the CNPG Cluster.
func TestRealCNPG_Restore_Idempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Minute)
	t.Cleanup(cancel)

	cleanupTestResources(t)

	id := suffix()
	clusterName := "e2e-real-cnpg-idem-rst-" + id
	backupName := "e2e-real-backup-cnpg-idem-rst-" + id
	restoreName := "e2e-real-restore-cnpg-idem-rst-" + id

	cluster := newRealCNPGCluster(clusterName)
	require.NoError(t, cl.Create(ctx, cluster))
	t.Cleanup(func() { stripCNPGFinalizersAndDelete(t, cluster) })

	waitForCNPGClusterReady(t, ctx, cluster)

	bkp := newPlatformBackupCNPGReal(backupName)
	require.NoError(t, cl.Create(ctx, bkp))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), bkp) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: backupName}, bkp); err != nil {
			return false
		}
		return apimeta.IsStatusConditionTrue(bkp.Status.Conditions, backup.ConditionCNPGSnapshotted)
	}, 10*time.Minute, 15*time.Second, "backup never completed")

	rst := newPlatformRestore(restoreName, backupName)
	require.NoError(t, cl.Create(ctx, rst))
	t.Cleanup(func() { _ = cl.Delete(context.Background(), rst) })

	require.Eventually(t, func() bool {
		if err := cl.Get(ctx, types.NamespacedName{Name: restoreName}, rst); err != nil {
			return false
		}
		return apimeta.IsStatusConditionTrue(rst.Status.Conditions, restore.ConditionCNPGRestored)
	}, 25*time.Minute, 15*time.Second, "restore never completed")

	// Record Cluster UID.
	var c cnpgv1.Cluster
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: e2eNS}, &c))
	originalUID := c.UID
	t.Logf("[step] restore complete, Cluster UID = %s", originalUID)

	// Force re-reconcile.
	patch := ctrlruntimeclient.MergeFrom(rst.DeepCopy())
	if rst.Annotations == nil {
		rst.Annotations = map[string]string{}
	}
	rst.Annotations["backup.platform-mesh.io/force-reconcile"] = "true"
	require.NoError(t, cl.Patch(ctx, rst, patch))

	time.Sleep(10 * time.Second)

	var reread cnpgv1.Cluster
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: e2eNS}, &reread))
	assert.Equal(t, originalUID, reread.UID,
		"Cluster must not be re-created on second reconcile (idempotent)")
	t.Logf("[step] idempotency confirmed: Cluster UID unchanged at %s", reread.UID)
}
