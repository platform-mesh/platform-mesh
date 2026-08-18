package velero

import (
	"context"
	"fmt"

	"go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultBackupStorageLocationName = "default"
)

type Backup struct {
	client ctrlruntimeclient.Client
}

func NewBackup(client ctrlruntimeclient.Client) *Backup {
	return &Backup{
		client: client,
	}
}

func (b *Backup) Ensure(ctx context.Context, backup v1alpha1.PlatformBackup, includedNamespaces []string) (*velerov1.Backup, error) {
	log := logger.LoadLoggerFromContext(ctx)
	name := fmt.Sprintf("%s-%s", backup.Name, "platform-mesh")
	desired := &velerov1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: DefaultNamespace,
			Labels:    objectLabels(),
			Annotations: map[string]string{
				"backup.platform-mesh.io/platformbackup": backup.Name,
			},
		},
		Spec: velerov1.BackupSpec{
			StorageLocation:    defaultBackupStorageLocationName,
			IncludedNamespaces: includedNamespaces,
		},
	}

	var current velerov1.Backup
	err := b.client.Get(ctx, types.NamespacedName{Name: name, Namespace: DefaultNamespace}, &current)
	if apierrors.IsNotFound(err) {
		if createErr := b.client.Create(ctx, desired); createErr != nil {
			return nil, fmt.Errorf("failed to create Velero Backup: %w", createErr)
		}

		log.Info().
			Str("subroutine", "velero-backup").
			Str("namespace", DefaultNamespace).
			Str("name", name).
			Strs("includedNamespaces", includedNamespaces).
			Msg("created Velero Backup")

		return desired, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get Velero Backup: %w", err)
	}

	return &current, nil
}
