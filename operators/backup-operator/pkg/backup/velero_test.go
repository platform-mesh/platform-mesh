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

package backup_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	backupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// runVeleroBackupSimulator watches for velero Backup objects and transitions them to Completed.
func runVeleroBackupSimulator(ctx context.Context, cl client.Client) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			var list velerov1.BackupList
			if err := cl.List(ctx, &list, client.InNamespace(testNamespace)); err != nil {
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
}

func makePlatformBackupVeleroIntegration(t *testing.T, cl client.Client, name string) *backupv1alpha1.PlatformBackup {
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
	return bkp
}

// TestVeleroCapture_Success_Integration verifies a Velero Backup CR is created and artefact recorded.
func TestVeleroCapture_Success_Integration(t *testing.T) {
	cl := newVeleroTestClient(t)
	stop := func() {}
	defer stop()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	runVeleroBackupSimulator(ctx, cl)

	bkp := makePlatformBackupVeleroIntegration(t, cl, "backup-velero")

	sub := backup.NewVeleroCaptureSubroutine(testNamespace).
		WithPollIntervals(100*time.Millisecond, 20*time.Second)

	require.Eventually(t, func() bool {
		result, err := sub.Process(injectClient(ctx, cl), bkp)
		require.NoError(t, err)
		return result.IsContinue()
	}, 20*time.Second, 100*time.Millisecond)

	require.NotNil(t, bkp.Status.Artefacts.Velero)
	assert.Equal(t, "backup-velero", bkp.Status.Artefacts.Velero.BackupName)
}

// TestVeleroCapture_Idempotent_Integration verifies a second call is a no-op.
func TestVeleroCapture_Idempotent_Integration(t *testing.T) {
	cl := newVeleroTestClient(t)
	stop := func() {}
	defer stop()

	ctx := t.Context()
	bkp := makePlatformBackupVeleroIntegration(t, cl, "backup-velero-idem")
	bkp.Status.Artefacts.Velero = &backupv1alpha1.VeleroArtefact{BackupName: "backup-velero-idem"}

	sub := backup.NewVeleroCaptureSubroutine(testNamespace).
		WithPollIntervals(100*time.Millisecond, 5*time.Second)

	result, err := sub.Process(injectClient(ctx, cl), bkp)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())

	// No Backup CR should have been created.
	var list velerov1.BackupList
	require.NoError(t, cl.List(ctx, &list, client.InNamespace(testNamespace)))
	assert.Empty(t, list.Items)
}

// TestVeleroCapture_Failed_Integration verifies a failed Backup CR surfaces as error.
func TestVeleroCapture_Failed_Integration(t *testing.T) {
	cl := newVeleroTestClient(t)
	stop := func() {}
	defer stop()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	// Simulator that marks backups as failed.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			var list velerov1.BackupList
			if err := cl.List(ctx, &list, client.InNamespace(testNamespace)); err != nil {
				continue
			}
			for i := range list.Items {
				b := &list.Items[i]
				if b.Status.Phase != "" {
					continue
				}
				patch := client.MergeFrom(b.DeepCopy())
				b.Status.Phase = velerov1.BackupPhaseFailed
				_ = cl.Patch(ctx, b, patch)
			}
		}
	}()

	bkp := makePlatformBackupVeleroIntegration(t, cl, "backup-velero-fail")
	sub := backup.NewVeleroCaptureSubroutine(testNamespace).
		WithPollIntervals(100*time.Millisecond, 20*time.Second)

	var lastErr error
	require.Eventually(t, func() bool {
		_, lastErr = sub.Process(injectClient(ctx, cl), bkp)
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
	return fake.NewClientBuilder().WithScheme(s).Build()
}
