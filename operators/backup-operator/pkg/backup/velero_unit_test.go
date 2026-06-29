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

package backup_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const veleroTestNS = "default"

func newVeleroScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, pmbackupv1alpha1.AddToScheme(s))
	require.NoError(t, velerov1.SchemeBuilder.AddToScheme(s))
	return s
}

func fakePlatformBackupVelero(name string, veleroEnabled bool) *pmbackupv1alpha1.PlatformBackup {
	return &pmbackupv1alpha1.PlatformBackup{
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
				Etcd:   pmbackupv1alpha1.EtcdSpec{Enabled: false},
				Velero: pmbackupv1alpha1.VeleroSpec{Enabled: veleroEnabled},
			},
		},
	}
}

func fakeVeleroBackup(name, namespace string, phase velerov1.BackupPhase) *velerov1.Backup {
	b := &velerov1.Backup{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	b.Status.Phase = phase
	return b
}

func newVeleroCaptureSub() *backup.VeleroCaptureSubroutine {
	return backup.NewVeleroCaptureSubroutine(veleroTestNS).
		WithPollIntervals(1*time.Millisecond, 5*time.Second)
}

// TestVeleroCapture_Disabled verifies Process is a no-op when Velero is disabled.
func TestVeleroCapture_Disabled(t *testing.T) {
	bkp := fakePlatformBackupVelero("bkp", false)
	cl := fake.NewClientBuilder().WithScheme(newVeleroScheme(t)).WithObjects(bkp).Build()

	result, err := newVeleroCaptureSub().Process(cnpgCtxWithClient(cl), bkp)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
}

// TestVeleroCapture_Idempotent verifies a second call is a no-op when artefacts are recorded.
func TestVeleroCapture_Idempotent(t *testing.T) {
	bkp := fakePlatformBackupVelero("bkp", true)
	bkp.Status.Artefacts.Velero = &pmbackupv1alpha1.VeleroArtefact{BackupName: "bkp"}
	cl := fake.NewClientBuilder().WithScheme(newVeleroScheme(t)).WithObjects(bkp).Build()

	result, err := newVeleroCaptureSub().Process(cnpgCtxWithClient(cl), bkp)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
}

// TestVeleroCapture_WrongObjectType verifies an error for non-PlatformBackup objects.
func TestVeleroCapture_WrongObjectType(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(newVeleroScheme(t)).Build()
	rst := &pmbackupv1alpha1.PlatformRestore{ObjectMeta: metav1.ObjectMeta{Name: "r"}}
	_, err := newVeleroCaptureSub().Process(cnpgCtxWithClient(cl), rst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected object type")
}

// TestVeleroCapture_Success verifies a completed Velero Backup is recorded in artefacts.
func TestVeleroCapture_Success(t *testing.T) {
	bkp := fakePlatformBackupVelero("bkp", true)
	vb := fakeVeleroBackup("bkp", veleroTestNS, velerov1.BackupPhaseCompleted)

	cl := fake.NewClientBuilder().WithScheme(newVeleroScheme(t)).
		WithObjects(bkp, vb).WithStatusSubresource(&velerov1.Backup{}).Build()

	result, err := newVeleroCaptureSub().Process(cnpgCtxWithClient(cl), bkp)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
	require.NotNil(t, bkp.Status.Artefacts.Velero)
	assert.Equal(t, "bkp", bkp.Status.Artefacts.Velero.BackupName)
}

// TestVeleroCapture_Failed verifies a failed Velero Backup surfaces as an error.
func TestVeleroCapture_Failed(t *testing.T) {
	bkp := fakePlatformBackupVelero("bkp", true)
	vb := fakeVeleroBackup("bkp", veleroTestNS, velerov1.BackupPhaseFailed)

	cl := fake.NewClientBuilder().WithScheme(newVeleroScheme(t)).
		WithObjects(bkp, vb).WithStatusSubresource(&velerov1.Backup{}).Build()

	_, err := newVeleroCaptureSub().Process(cnpgCtxWithClient(cl), bkp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed")
}

// TestVeleroCapture_Timeout verifies a timeout error when the Backup CR stays pending.
func TestVeleroCapture_Timeout(t *testing.T) {
	bkp := fakePlatformBackupVelero("bkp", true)
	vb := fakeVeleroBackup("bkp", veleroTestNS, velerov1.BackupPhaseInProgress)

	cl := fake.NewClientBuilder().WithScheme(newVeleroScheme(t)).
		WithObjects(bkp, vb).WithStatusSubresource(&velerov1.Backup{}).Build()

	sub := backup.NewVeleroCaptureSubroutine(veleroTestNS).
		WithPollIntervals(1*time.Millisecond, 50*time.Millisecond)

	_, err := sub.Process(cnpgCtxWithClient(cl), bkp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

// TestVeleroCapture_BackupCreatedWhenAbsent verifies a Backup CR is created when absent.
func TestVeleroCapture_BackupCreatedWhenAbsent(t *testing.T) {
	bkp := fakePlatformBackupVelero("bkp", true)

	created := false
	cl := fake.NewClientBuilder().WithScheme(newVeleroScheme(t)).
		WithObjects(bkp).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.CreateOption) error {
				if vb, ok := obj.(*velerov1.Backup); ok && vb.Name == "bkp" {
					created = true
					vb.Status.Phase = velerov1.BackupPhaseCompleted
					return c.Create(ctx, vb, opts...)
				}
				return c.Create(ctx, obj, opts...)
			},
		}).Build()

	sub := backup.NewVeleroCaptureSubroutine(veleroTestNS).
		WithPollIntervals(1*time.Millisecond, 5*time.Second)
	_, _ = sub.Process(cnpgCtxWithClient(cl), bkp)
	assert.True(t, created)

	var vb velerov1.Backup
	err := cl.Get(t.Context(), types.NamespacedName{Name: "bkp", Namespace: veleroTestNS}, &vb)
	assert.NoError(t, err)
}

// TestVeleroCapture_GetName verifies the subroutine returns the expected condition name.
func TestVeleroCapture_GetName(t *testing.T) {
	assert.Equal(t, backup.ConditionVeleroBackedUp, newVeleroCaptureSub().GetName())
}
