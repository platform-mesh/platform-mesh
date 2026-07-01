//go:build !integration

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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/restore"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const veleroRestoreNS = "default"

func newVeleroRestoreScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, pmbackupv1alpha1.AddToScheme(s))
	require.NoError(t, velerov1.SchemeBuilder.AddToScheme(s))
	return s
}

func fakePlatformBackupWithVelero(name, veleroBackupName string) *pmbackupv1alpha1.PlatformBackup {
	b := &pmbackupv1alpha1.PlatformBackup{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: pmbackupv1alpha1.PlatformBackupSpec{
			Storage: pmbackupv1alpha1.StorageSpec{
				S3: pmbackupv1alpha1.S3StorageSpec{
					Endpoint:       "http://minio:9000",
					Bucket:         "backups",
					CredentialsRef: corev1.LocalObjectReference{Name: "s3-credentials"},
				},
			},
			Components: pmbackupv1alpha1.ComponentsSpec{
				Velero: pmbackupv1alpha1.VeleroSpec{Enabled: true},
			},
		},
	}
	if veleroBackupName != "" {
		b.Status.Artefacts.Velero = &pmbackupv1alpha1.VeleroArtefact{BackupName: veleroBackupName}
	}
	return b
}

func fakeVeleroRestore(name, namespace string, phase velerov1.RestorePhase) *velerov1.Restore {
	r := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	r.Status.Phase = phase
	return r
}

func newVeleroRestoreSub() *restore.VeleroRestoreSubroutine {
	return restore.NewVeleroRestoreSubroutine(veleroRestoreNS).
		WithPollIntervals(1*time.Millisecond, 5*time.Second)
}

// TestVeleroRestore_WrongObjectType verifies an error for non-PlatformRestore objects.
func TestVeleroRestore_WrongObjectType(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(newVeleroRestoreScheme(t)).Build()
	bkp := &pmbackupv1alpha1.PlatformBackup{ObjectMeta: metav1.ObjectMeta{Name: "b"}}
	_, err := newVeleroRestoreSub().Process(ctxWithClient(cl), bkp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected object type")
}

// TestVeleroRestore_BackupNotFound verifies StopWithRequeue when source backup is missing.
func TestVeleroRestore_BackupNotFound(t *testing.T) {
	rst := fakePlatformRestoreCNPG("r", "nonexistent")
	cl := fake.NewClientBuilder().WithScheme(newVeleroRestoreScheme(t)).WithObjects(rst).Build()

	result, err := newVeleroRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsStopWithRequeue())
}

// TestVeleroRestore_NoVeleroArtefacts verifies OK when backup has no Velero artefacts.
func TestVeleroRestore_NoVeleroArtefacts(t *testing.T) {
	bkp := fakePlatformBackupWithVelero("bkp", "")
	rst := fakePlatformRestoreCNPG("r", "bkp")
	cl := fake.NewClientBuilder().WithScheme(newVeleroRestoreScheme(t)).WithObjects(bkp, rst).Build()

	result, err := newVeleroRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
}

// TestVeleroRestore_Idempotent verifies re-reconcile is a no-op when VeleroRestored=True.
func TestVeleroRestore_Idempotent(t *testing.T) {
	bkp := fakePlatformBackupWithVelero("bkp", "bkp")
	rst := fakePlatformRestoreCNPG("r", "bkp")
	rst.Status.Conditions = []metav1.Condition{{
		Type:               restore.ConditionVeleroRestored,
		Status:             metav1.ConditionTrue,
		Reason:             "Complete",
		LastTransitionTime: metav1.Now(),
	}}
	cl := fake.NewClientBuilder().WithScheme(newVeleroRestoreScheme(t)).WithObjects(bkp).Build()

	result, err := newVeleroRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
}

// TestVeleroRestore_Success verifies OK when the Velero Restore completes.
func TestVeleroRestore_Success(t *testing.T) {
	bkp := fakePlatformBackupWithVelero("bkp", "bkp")
	rst := fakePlatformRestoreCNPG("r", "bkp")
	vr := fakeVeleroRestore("r", veleroRestoreNS, velerov1.RestorePhaseCompleted)

	cl := fake.NewClientBuilder().WithScheme(newVeleroRestoreScheme(t)).
		WithObjects(bkp, rst, vr).WithStatusSubresource(&velerov1.Restore{}).Build()

	result, err := newVeleroRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
}

// TestVeleroRestore_Failed verifies an error when the Velero Restore fails.
func TestVeleroRestore_Failed(t *testing.T) {
	bkp := fakePlatformBackupWithVelero("bkp", "bkp")
	rst := fakePlatformRestoreCNPG("r", "bkp")
	vr := fakeVeleroRestore("r", veleroRestoreNS, velerov1.RestorePhaseFailed)

	cl := fake.NewClientBuilder().WithScheme(newVeleroRestoreScheme(t)).
		WithObjects(bkp, rst, vr).WithStatusSubresource(&velerov1.Restore{}).Build()

	_, err := newVeleroRestoreSub().Process(ctxWithClient(cl), rst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed")
}

// TestVeleroRestore_StaysPending verifies Pending is returned when the Restore CR is not yet complete.
func TestVeleroRestore_StaysPending(t *testing.T) {
	bkp := fakePlatformBackupWithVelero("bkp", "bkp")
	rst := fakePlatformRestoreCNPG("r", "bkp")
	vr := fakeVeleroRestore("r", veleroRestoreNS, velerov1.RestorePhaseInProgress)

	cl := fake.NewClientBuilder().WithScheme(newVeleroRestoreScheme(t)).
		WithObjects(bkp, rst, vr).WithStatusSubresource(&velerov1.Restore{}).Build()

	sub := restore.NewVeleroRestoreSubroutine(veleroRestoreNS).
		WithPollIntervals(1*time.Millisecond, 50*time.Millisecond)

	result, err := sub.Process(ctxWithClient(cl), rst)
	require.NoError(t, err, "non-blocking: pending restore must not return an error")
	assert.True(t, result.IsPending(), "expected Pending while restore is in progress")
}

// TestVeleroRestore_GetName verifies the subroutine returns the expected condition name.
func TestVeleroRestore_GetName(t *testing.T) {
	assert.Equal(t, restore.ConditionVeleroRestored, newVeleroRestoreSub().GetName())
}

// TestVeleroRestore_GetBackupGenericError verifies that a non-NotFound error on Get for PlatformBackup
// is surfaced as an error containing "fetching source backup".
func TestVeleroRestore_GetBackupGenericError(t *testing.T) {
	rst := fakePlatformRestoreCNPG("r", "some-backup")

	cl := fake.NewClientBuilder().
		WithScheme(newVeleroRestoreScheme(t)).
		WithObjects(rst).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c ctrlruntimeclient.WithWatch, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
				if _, ok := obj.(*pmbackupv1alpha1.PlatformBackup); ok {
					return fmt.Errorf("internal server error: etcd timeout")
				}
				return c.Get(ctx, key, obj, opts...)
			},
		}).
		Build()

	result, err := newVeleroRestoreSub().Process(ctxWithClient(cl), rst)
	assert.True(t, result.IsContinue())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetching source backup")
}

// TestVeleroRestore_EnsureBSLFails verifies that when creating the BackupStorageLocation fails,
// Process returns an error containing "ensuring BackupStorageLocation".
func TestVeleroRestore_EnsureBSLFails(t *testing.T) {
	bkp := fakePlatformBackupWithVelero("bkp", "bkp")
	rst := fakePlatformRestoreCNPG("r", "bkp")

	cl := fake.NewClientBuilder().
		WithScheme(newVeleroRestoreScheme(t)).
		WithObjects(bkp, rst).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.CreateOption) error {
				if _, ok := obj.(*velerov1.BackupStorageLocation); ok {
					return fmt.Errorf("storage class not available")
				}
				return c.Create(ctx, obj, opts...)
			},
		}).
		Build()

	_, err := newVeleroRestoreSub().Process(ctxWithClient(cl), rst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ensuring BackupStorageLocation")
}

// TestVeleroRestore_GetAfterCreateFails verifies that when the Restore CR is created successfully
// but the subsequent Get returns an error, Process returns an error containing "polling Velero Restore".
func TestVeleroRestore_GetAfterCreateFails(t *testing.T) {
	bkp := fakePlatformBackupWithVelero("bkp", "bkp")
	rst := fakePlatformRestoreCNPG("r", "bkp")

	restoreCreated := false
	cl := fake.NewClientBuilder().
		WithScheme(newVeleroRestoreScheme(t)).
		WithObjects(bkp, rst).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.CreateOption) error {
				if _, ok := obj.(*velerov1.Restore); ok {
					restoreCreated = true
				}
				return c.Create(ctx, obj, opts...)
			},
			Get: func(ctx context.Context, c ctrlruntimeclient.WithWatch, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
				if _, ok := obj.(*velerov1.Restore); ok && restoreCreated {
					return fmt.Errorf("etcd connection reset by peer")
				}
				return c.Get(ctx, key, obj, opts...)
			},
		}).
		Build()

	_, err := newVeleroRestoreSub().Process(ctxWithClient(cl), rst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "polling Velero Restore")
}

// TestVeleroRestore_FailedValidation verifies that a Restore CR with phase RestorePhaseFailedValidation
// causes Process to return an error.
func TestVeleroRestore_FailedValidation(t *testing.T) {
	bkp := fakePlatformBackupWithVelero("bkp", "bkp")
	rst := fakePlatformRestoreCNPG("r", "bkp")
	vr := fakeVeleroRestore("r", veleroRestoreNS, velerov1.RestorePhaseFailedValidation)

	cl := fake.NewClientBuilder().WithScheme(newVeleroRestoreScheme(t)).
		WithObjects(bkp, rst, vr).WithStatusSubresource(&velerov1.Restore{}).Build()

	_, err := newVeleroRestoreSub().Process(ctxWithClient(cl), rst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FailedValidation")
}
