package backup

import (
	"context"
	"testing"

	"go.platform-mesh.io/apis/backup/v1alpha1"
	backupvelero "go.platform-mesh.io/backup-operator/pkg/velero"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlruntimefake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestVeleroCaptureWaitsForStorageLocationAvailability(t *testing.T) {
	t.Parallel()

	client := newVeleroTestClient(t)
	platformBackup := testPlatformBackup()

	result, changed, err := NewVeleroCaptureSubroutine("platform-mesh-system", client).Process(context.Background(), platformBackup)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if changed {
		t.Fatal("Process() changed status while storage location was unavailable")
	}
	if result.Requeue() == 0 {
		t.Fatal("Process() did not requeue while storage location was unavailable")
	}

	var backup velerov1.Backup
	err = client.Get(context.Background(), types.NamespacedName{
		Namespace: backupvelero.DefaultNamespace,
		Name:      platformBackup.Name + "-platform-mesh",
	}, &backup)
	if err == nil {
		t.Fatal("Process() created a Velero Backup before the storage location was available")
	}
	if ctrlruntimeclient.IgnoreNotFound(err) != nil {
		t.Fatalf("get Velero Backup: %v", err)
	}
}

func TestVeleroCaptureDeletesFailedValidationBackup(t *testing.T) {
	t.Parallel()

	platformBackup := testPlatformBackup()
	location := testStorageLocation(platformBackup)
	failedBackup := &velerov1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: backupvelero.DefaultNamespace,
			Name:      platformBackup.Name + "-platform-mesh",
		},
		Status: velerov1.BackupStatus{
			Phase:            velerov1.BackupPhaseFailedValidation,
			ValidationErrors: []string{"storage location unavailable"},
		},
	}
	client := newVeleroTestClient(t, location, failedBackup)

	result, changed, err := NewVeleroCaptureSubroutine("platform-mesh-system", client).Process(context.Background(), platformBackup)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if changed {
		t.Fatal("Process() changed status after deleting a failed-validation Backup")
	}
	if result.Requeue() == 0 {
		t.Fatal("Process() did not requeue after deleting a failed-validation Backup")
	}

	var backup velerov1.Backup
	err = client.Get(context.Background(), types.NamespacedName{
		Namespace: backupvelero.DefaultNamespace,
		Name:      failedBackup.Name,
	}, &backup)
	if ctrlruntimeclient.IgnoreNotFound(err) != nil {
		t.Fatalf("failed-validation Velero Backup was not deleted: %v", err)
	}
}

func newVeleroTestClient(t *testing.T, objects ...ctrlruntimeclient.Object) ctrlruntimeclient.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := velerov1.AddToScheme(scheme); err != nil {
		t.Fatalf("add Velero scheme: %v", err)
	}
	return ctrlruntimefake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func testPlatformBackup() *v1alpha1.PlatformBackup {
	return &v1alpha1.PlatformBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "backup"},
		Spec: v1alpha1.PlatformBackupSpec{
			Storage: v1alpha1.StorageSpec{S3: v1alpha1.S3StorageSpec{
				Endpoint:       "http://minio:9000",
				Bucket:         "backups",
				Region:         "us-east-1",
				CredentialsRef: corev1.LocalObjectReference{Name: "credentials"},
			}},
			Components: v1alpha1.ComponentsSpec{Velero: v1alpha1.VeleroSpec{Enabled: true}},
		},
	}
}

func testStorageLocation(backup *v1alpha1.PlatformBackup) *velerov1.BackupStorageLocation {
	return &velerov1.BackupStorageLocation{
		ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: backupvelero.DefaultNamespace},
		Spec: velerov1.BackupStorageLocationSpec{
			Provider: "aws",
			Credential: &corev1.SecretKeySelector{
				LocalObjectReference: backup.Spec.Storage.S3.CredentialsRef,
				Key:                  "cloud",
			},
			StorageType: velerov1.StorageType{ObjectStorage: &velerov1.ObjectStorageLocation{Bucket: backup.Spec.Storage.S3.Bucket}},
			Config: map[string]string{
				"s3Url":                 backup.Spec.Storage.S3.Endpoint,
				"region":                backup.Spec.Storage.S3.Region,
				"s3ForcePathStyle":      "true",
				"insecureSkipTLSVerify": "true",
			},
		},
		Status: velerov1.BackupStorageLocationStatus{Phase: velerov1.BackupStorageLocationPhaseAvailable},
	}
}
