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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	backupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// runCNPGBackupSimulator watches for cnpg Backup objects and transitions them to Completed.
func runCNPGBackupSimulator(ctx context.Context, t *testing.T, cl client.Client) {
	t.Helper()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}

			var list cnpgv1.BackupList
			if err := cl.List(ctx, &list, client.InNamespace(testNamespace)); err != nil {
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
}

func makeCNPGCluster(t *testing.T, cl client.Client, name string) {
	t.Helper()
	cluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: cnpgv1.ClusterSpec{Instances: 1},
	}
	require.NoError(t, cl.Create(t.Context(), cluster))
}

func makePlatformBackupCNPG(t *testing.T, cl client.Client, name string) *backupv1alpha1.PlatformBackup {
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
				CNPG: backupv1alpha1.CNPGSpec{Enabled: true},
			},
		},
	}
	require.NoError(t, cl.Create(t.Context(), bkp))
	return bkp
}

// TestCNPGCapture_Success_Integration verifies CNPG Backup CRs are created and artefacts recorded.
func TestCNPGCapture_Success_Integration(t *testing.T) {
	cl := newCNPGTestClient(t)
	stop := func() {}
	defer stop()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	runCNPGBackupSimulator(ctx, t, cl)

	makeCNPGCluster(t, cl, "openfga-db")
	bkp := makePlatformBackupCNPG(t, cl, "backup-cnpg")

	sub := backup.NewCNPGCaptureSubroutine(testNamespace, []string{"openfga-db"}).
		WithPollIntervals(100*time.Millisecond, 20*time.Second)

	// Non-blocking: call Process repeatedly until IsContinue().
	require.Eventually(t, func() bool {
		result, err := sub.Process(injectClient(ctx, cl), bkp)
		if err != nil {
			return false
		}
		return result.IsContinue()
	}, 20*time.Second, 100*time.Millisecond)

	require.NotNil(t, bkp.Status.Artefacts.CNPG)
	assert.NotEmpty(t, bkp.Status.Artefacts.CNPG.Backups["openfga-db"])
}

// TestCNPGCapture_MultiCluster_Integration verifies all CNPG clusters are captured.
func TestCNPGCapture_MultiCluster_Integration(t *testing.T) {
	cl := newCNPGTestClient(t)
	stop := func() {}
	defer stop()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	runCNPGBackupSimulator(ctx, t, cl)

	makeCNPGCluster(t, cl, "openfga-db")
	makeCNPGCluster(t, cl, "keycloak-db")
	bkp := makePlatformBackupCNPG(t, cl, "backup-cnpg-multi")

	sub := backup.NewCNPGCaptureSubroutine(testNamespace, []string{"openfga-db", "keycloak-db"}).
		WithPollIntervals(100*time.Millisecond, 20*time.Second)

	require.Eventually(t, func() bool {
		result, err := sub.Process(injectClient(ctx, cl), bkp)
		if err != nil {
			return false
		}
		return result.IsContinue()
	}, 20*time.Second, 100*time.Millisecond)

	require.NotNil(t, bkp.Status.Artefacts.CNPG)
	assert.Len(t, bkp.Status.Artefacts.CNPG.Backups, 2)
}

// TestCNPGCapture_Failed_Integration verifies a failed Backup CR surfaces as an error.
func TestCNPGCapture_Failed_Integration(t *testing.T) {
	cl := newCNPGTestClient(t)
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
			var list cnpgv1.BackupList
			if err := cl.List(ctx, &list, client.InNamespace(testNamespace)); err != nil {
				continue
			}
			for i := range list.Items {
				b := &list.Items[i]
				if b.Status.Phase == cnpgv1.BackupPhaseCompleted || b.Status.Phase == cnpgv1.BackupPhaseFailed {
					continue
				}
				patch := client.MergeFrom(b.DeepCopy())
				b.Status.Phase = cnpgv1.BackupPhaseFailed
				b.Status.Error = "simulated cnpg backup failure"
				_ = cl.Status().Patch(ctx, b, patch)
			}
		}
	}()

	makeCNPGCluster(t, cl, "openfga-db")
	bkp := makePlatformBackupCNPG(t, cl, "backup-cnpg-fail")

	sub := backup.NewCNPGCaptureSubroutine(testNamespace, []string{"openfga-db"}).
		WithPollIntervals(100*time.Millisecond, 20*time.Second)

	// Non-blocking: loop until error or timeout.
	var gotErr error
	require.Eventually(t, func() bool {
		_, err := sub.Process(injectClient(ctx, cl), bkp)
		if err != nil {
			gotErr = err
			return true
		}
		return false
	}, 20*time.Second, 100*time.Millisecond, "expected error from failed backup")

	require.Error(t, gotErr)
	assert.Contains(t, gotErr.Error(), "simulated cnpg backup failure")
}

// TestCNPGCapture_Idempotent_Integration verifies a second call is a no-op.
func TestCNPGCapture_Idempotent_Integration(t *testing.T) {
	cl := newCNPGTestClient(t)
	stop := func() {}
	defer stop()

	ctx := t.Context()

	bkp := makePlatformBackupCNPG(t, cl, "backup-cnpg-idem")
	bkp.Status.Artefacts.CNPG = &backupv1alpha1.CNPGArtefact{
		Backups: map[string]string{"openfga-db": "backup-cnpg-idem-openfga-db"},
	}

	sub := backup.NewCNPGCaptureSubroutine(testNamespace, []string{"openfga-db"}).
		WithPollIntervals(100*time.Millisecond, 5*time.Second)

	result, err := sub.Process(injectClient(ctx, cl), bkp)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())

	// Verify no Backup CR was created.
	var backupList cnpgv1.BackupList
	require.NoError(t, cl.List(ctx, &backupList, client.InNamespace(testNamespace)))
	assert.Empty(t, backupList.Items)
}

// TestCNPGCapture_BackupCRVerification_Integration verifies the Backup CR has correct spec.
func TestCNPGCapture_BackupCRVerification_Integration(t *testing.T) {
	cl := newCNPGTestClient(t)
	stop := func() {}
	defer stop()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	runCNPGBackupSimulator(ctx, t, cl)
	makeCNPGCluster(t, cl, "openfga-db")
	bkp := makePlatformBackupCNPG(t, cl, "backup-cnpg-verify")

	sub := backup.NewCNPGCaptureSubroutine(testNamespace, []string{"openfga-db"}).
		WithPollIntervals(100*time.Millisecond, 20*time.Second)

	_, _ = sub.Process(injectClient(ctx, cl), bkp)

	var b cnpgv1.Backup
	require.NoError(t, cl.Get(ctx, types.NamespacedName{
		Name:      "backup-cnpg-verify-openfga-db",
		Namespace: testNamespace,
	}, &b))
	assert.Equal(t, "openfga-db", b.Spec.Cluster.Name)
	assert.Equal(t, cnpgv1.BackupMethodBarmanObjectStore, b.Spec.Method)
}

func newCNPGTestClient(t *testing.T) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("adding clientgo scheme: %v", err)
	}
	if err := backupv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("adding backup scheme: %v", err)
	}
	if err := cnpgv1.AddToScheme(s); err != nil {
		t.Fatalf("adding cnpg scheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&cnpgv1.Backup{}, &cnpgv1.Cluster{}).Build()
}
