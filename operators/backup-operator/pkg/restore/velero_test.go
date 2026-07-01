//go:build integration

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

package restore_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	backupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/restore"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// runVeleroRestoreSimulator watches for velero Restore objects and transitions them to Completed.
func runVeleroRestoreSimulator(ctx context.Context, cl client.Client) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			var list velerov1.RestoreList
			if err := cl.List(ctx, &list, client.InNamespace(testNamespace)); err != nil {
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

func makeBackupWithVelero(t *testing.T, cl client.Client, name, veleroBackupName string) *backupv1alpha1.PlatformBackup {
	t.Helper()
	bkp := &backupv1alpha1.PlatformBackup{
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
				Velero: backupv1alpha1.VeleroSpec{Enabled: true},
			},
		},
	}
	require.NoError(t, cl.Create(t.Context(), bkp))
	if veleroBackupName != "" {
		bkp.Status.Artefacts.Velero = &backupv1alpha1.VeleroArtefact{BackupName: veleroBackupName}
		require.NoError(t, cl.Status().Update(t.Context(), bkp))
	}
	return bkp
}

// TestVeleroRestore_Success_Integration verifies a Velero Restore CR is created and completes.
func TestVeleroRestore_Success_Integration(t *testing.T) {
	cl := newVeleroTestClient(t)
	stop := func() {}
	defer stop()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	runVeleroRestoreSimulator(ctx, cl)

	bkp := makeBackupWithVelero(t, cl, "bkp-velero-rst", "bkp-velero-rst")
	rst := makePlatformRestore(t, cl, "restore-velero", bkp.Name)

	sub := restore.NewVeleroRestoreSubroutine(testNamespace).
		WithPollIntervals(100*time.Millisecond, 20*time.Second)

	require.Eventually(t, func() bool {
		result, err := sub.Process(injectClient(ctx, cl), rst)
		require.NoError(t, err)
		return result.IsContinue()
	}, 20*time.Second, 100*time.Millisecond)
}

// TestVeleroRestore_MissingBackup_StopsWithRequeue verifies StopWithRequeue for missing backup.
func TestVeleroRestore_MissingBackup_StopsWithRequeue(t *testing.T) {
	cl := newVeleroTestClient(t)
	stop := func() {}
	defer stop()

	rst := makePlatformRestore(t, cl, "restore-velero-missing", "nonexistent")
	sub := restore.NewVeleroRestoreSubroutine(testNamespace)
	result, err := sub.Process(injectClient(t.Context(), cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsStopWithRequeue())
}

// TestVeleroRestore_NoVeleroArtefacts_Skips verifies OK when backup has no Velero artefacts.
func TestVeleroRestore_NoVeleroArtefacts_Skips(t *testing.T) {
	cl := newVeleroTestClient(t)
	stop := func() {}
	defer stop()

	bkp := makeBackupWithVelero(t, cl, "bkp-no-velero", "")
	rst := makePlatformRestore(t, cl, "restore-velero-skip", bkp.Name)
	sub := restore.NewVeleroRestoreSubroutine(testNamespace)
	result, err := sub.Process(injectClient(t.Context(), cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
}

// TestVeleroRestore_Failed_Integration verifies a failed Restore CR surfaces as error.
func TestVeleroRestore_Failed_Integration(t *testing.T) {
	cl := newVeleroTestClient(t)
	stop := func() {}
	defer stop()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	// Simulator that marks restores as failed.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			var list velerov1.RestoreList
			if err := cl.List(ctx, &list, client.InNamespace(testNamespace)); err != nil {
				continue
			}
			for i := range list.Items {
				r := &list.Items[i]
				if r.Status.Phase != "" {
					continue
				}
				patch := client.MergeFrom(r.DeepCopy())
				r.Status.Phase = velerov1.RestorePhaseFailed
				_ = cl.Patch(ctx, r, patch)
			}
		}
	}()

	bkp := makeBackupWithVelero(t, cl, "bkp-velero-fail", "bkp-velero-fail")
	rst := makePlatformRestore(t, cl, "restore-velero-fail", bkp.Name)
	sub := restore.NewVeleroRestoreSubroutine(testNamespace).
		WithPollIntervals(100*time.Millisecond, 20*time.Second)

	var lastErr error
	require.Eventually(t, func() bool {
		_, lastErr = sub.Process(injectClient(ctx, cl), rst)
		return lastErr != nil
	}, 20*time.Second, 100*time.Millisecond)
	assert.Contains(t, lastErr.Error(), "failed")
}

func newVeleroTestClient(t *testing.T) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("adding clientgo scheme: %v", err)
	}
	if err := backupv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("adding backup scheme: %v", err)
	}
	if err := velerov1.SchemeBuilder.AddToScheme(s); err != nil {
		t.Fatalf("adding velero scheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&backupv1alpha1.PlatformBackup{}).Build()
}
