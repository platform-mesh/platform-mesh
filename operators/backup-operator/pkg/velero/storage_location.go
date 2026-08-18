package velero

import (
	"context"
	"fmt"

	"go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultBucketName = "default"
	defaultProvider   = "aws"
	defaultSecretKey  = "cloud"
)

type StorageLocation struct {
	client ctrlruntimeclient.Client
}

func NewStorageLocation(client ctrlruntimeclient.Client) *StorageLocation {
	return &StorageLocation{
		client: client,
	}
}

func (s *StorageLocation) EnsureForBackup(ctx context.Context, backup v1alpha1.PlatformBackup) error {
	return s.ensure(ctx, backup.Spec.Storage)
}

// EnsureAvailableForBackup ensures the storage location is configured, then
// reports whether Velero has validated it and can accept a Backup. A newly
// created or updated location has no status until Velero reconciles it.
func (s *StorageLocation) EnsureAvailableForBackup(ctx context.Context, backup v1alpha1.PlatformBackup) (bool, error) {
	if err := s.EnsureForBackup(ctx, backup); err != nil {
		return false, err
	}

	var location velerov1.BackupStorageLocation
	if err := s.client.Get(ctx, types.NamespacedName{Name: defaultBucketName, Namespace: DefaultNamespace}, &location); err != nil {
		return false, fmt.Errorf("get BackupStorageLocation after ensuring it: %w", err)
	}
	return location.Status.Phase == velerov1.BackupStorageLocationPhaseAvailable, nil
}

func (s *StorageLocation) EnsureForRestore(ctx context.Context, restore v1alpha1.PlatformRestore) error {
	return s.ensure(ctx, restore.Spec.Source.Storage)
}

func (s *StorageLocation) ensure(ctx context.Context, storage v1alpha1.StorageSpec) error {
	log := logger.LoadLoggerFromContext(ctx)

	desired := &velerov1.BackupStorageLocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultBucketName,
			Namespace: DefaultNamespace,
		},
		Spec: velerov1.BackupStorageLocationSpec{
			Provider: defaultProvider,
			// velero expects the secret to have a key named "cloud" containing an AWS credentials file.
			Credential: &corev1.SecretKeySelector{
				LocalObjectReference: storage.S3.CredentialsRef,
				Key:                  defaultSecretKey,
			},
			StorageType: velerov1.StorageType{
				ObjectStorage: &velerov1.ObjectStorageLocation{
					Bucket: storage.S3.Bucket,
				},
			},
			Config: map[string]string{
				"s3Url":                 storage.S3.Endpoint,
				"region":                storage.S3.Region,
				"s3ForcePathStyle":      "true",
				"insecureSkipTLSVerify": "true",
			},
		},
	}

	var current velerov1.BackupStorageLocation
	err := s.client.Get(ctx, types.NamespacedName{Name: defaultBucketName, Namespace: DefaultNamespace}, &current)
	if apierrors.IsNotFound(err) {
		if createErr := s.client.Create(ctx, desired); createErr != nil {
			return fmt.Errorf("failed to create BackupStorageLocation: %w", createErr)
		}
		log.Info().
			Str("component", "velero-storage-location").
			Str("namespace", DefaultNamespace).
			Str("storageLocation", defaultBucketName).
			Str("bucket", storage.S3.Bucket).
			Msg("created Velero BackupStorageLocation")
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get BackupStorageLocation: %w", err)
	}
	log.Info().
		Str("component", "velero-storage-location").
		Str("namespace", DefaultNamespace).
		Str("storageLocation", defaultBucketName).
		Str("bucket", storage.S3.Bucket).
		Msg("retrieved Velero BackupStorageLocation")

	// return if no diff
	if current.Spec.Config["s3Url"] == storage.S3.Endpoint &&
		current.Spec.ObjectStorage != nil &&
		current.Spec.ObjectStorage.Bucket == storage.S3.Bucket {
		return nil
	}

	patch := ctrlruntimeclient.MergeFrom(current.DeepCopy())
	current.Spec = desired.Spec
	if patchErr := s.client.Patch(ctx, &current, patch); patchErr != nil {
		return fmt.Errorf("failed to update BackupStorageLocation: %w", patchErr)
	}
	log.Info().
		Str("component", "velero-storage-location").
		Str("namespace", DefaultNamespace).
		Str("storageLocation", defaultBucketName).
		Str("bucket", storage.S3.Bucket).
		Msg("updated Velero BackupStorageLocation")
	return nil
}
