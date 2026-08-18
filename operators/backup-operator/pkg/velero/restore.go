package velero

import (
	"context"
	"fmt"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"go.platform-mesh.io/apis/backup/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type Restore struct {
	client ctrlruntimeclient.Client
}

func NewRestore(client ctrlruntimeclient.Client) *Restore {
	return &Restore{
		client: client,
	}
}

func (r *Restore) Ensure(ctx context.Context, restore v1alpha1.PlatformRestore, backupName string) (*velerov1.Restore, error) {
	restoreName := fmt.Sprintf("%s-platform-mesh", restore.Name)

	current := &velerov1.Restore{}
	err := r.client.Get(ctx, ctrlruntimeclient.ObjectKey{
		Namespace: DefaultNamespace,
		Name:      restoreName,
	}, current)
	if err == nil {
		return current, nil
	}

	if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to get Velero Restore %s/%s: %w", DefaultNamespace, restoreName, err)
	}

	includeClusterResources := false

	current = &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      restoreName,
			Namespace: DefaultNamespace,
			Labels:    objectLabels(),
			Annotations: map[string]string{
				"backup.platform-mesh.io/platformrestore": restore.Name,
			},
		},
		Spec: velerov1.RestoreSpec{
			BackupName:              backupName,
			IncludeClusterResources: &includeClusterResources,
			ExcludedNamespaces: []string{
				"platform-mesh-system",
				"platform-mesh-velero",
				"platform-mesh-backup-operator",
				"kcp-operator",
				"cert-manager",
			},
		},
	}

	if err := r.client.Create(ctx, current); err != nil {
		return nil, fmt.Errorf("failed to create Velero Restore %s/%s: %w", current.Namespace, current.Name, err)
	}

	return current, nil
}
