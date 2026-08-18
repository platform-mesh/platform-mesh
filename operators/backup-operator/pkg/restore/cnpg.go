package restore

import (
	"context"
	"fmt"
	"path"
	"time"

	"go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	cnpgRestoreSubroutineName          = "cnpg-restore"
	cnpgNamespace                      = "platform-mesh-system"
	cnpgClusterName                    = "platform-mesh-pg"
	cnpgSourceName                     = "platform-mesh-pg"
	cnpgSecretName                     = "cnpg-backup-s3"
	cnpgBackupBucket                   = "cnpg-backups"
	cnpgAccessKeyIDSecretKey           = "ACCESS_KEY_ID"
	cnpgSecretAccessKeySecretKey       = "SECRET_ACCESS_KEY"
	cnpgRestoreCleanupStartedCondition = "CNPGRestoreCleanupStarted"
	cnpgClusterLabel                   = "cnpg.io/cluster"
	cnpgRestoreCompletedCondition      = "CNPGRestoreCompleted"
)

type CNPGRestoreSubroutine struct {
	name   string
	client ctrlruntimeclient.Client
}

func NewCNPGRestoreSubroutine(c ctrlruntimeclient.Client) *CNPGRestoreSubroutine {
	return &CNPGRestoreSubroutine{
		name:   cnpgRestoreSubroutineName,
		client: c,
	}
}

func (c *CNPGRestoreSubroutine) GetName() string {
	return c.name
}

func (c *CNPGRestoreSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, bool, error) {
	platformRestore, ok := obj.(*v1alpha1.PlatformRestore)
	if !ok {
		return subroutines.OK(), false, fmt.Errorf("expected a v1alpha1.PlatformRestore, got %T", obj)
	}

	if restoreTerminal(platformRestore) {
		return subroutines.OK(), false, nil
	}

	if cnpgRestoreCompleted(platformRestore) {
		return subroutines.OK(), false, nil
	}

	log := logger.LoadLoggerFromContext(ctx)

	statusChanged := setPhase(platformRestore, v1alpha1.RestorePhaseRestoringCNPG)

	if !cnpgRestoreCleanupStarted(platformRestore) {
		if err := c.deleteCNPGRuntimeObjects(ctx); err != nil {
			return subroutines.OK(), statusChanged, err
		}

		meta.SetStatusCondition(&platformRestore.Status.Conditions, metav1.Condition{
			Type:               cnpgRestoreCleanupStartedCondition,
			Status:             metav1.ConditionTrue,
			Reason:             "AutomatedCNPGRestoreCleanupStarted",
			Message:            "Deleted existing CNPG cluster, pods, and PVCs before restore",
			ObservedGeneration: platformRestore.Generation,
		})
		statusChanged = true

		log.Info().
			Str("subroutine", cnpgRestoreSubroutineName).
			Str("platformRestore", platformRestore.Name).
			Str("cluster", cnpgClusterName).
			Msg("started automated CNPG restore cleanup")

		return subroutines.StopWithRequeue(
			10*time.Second,
			fmt.Sprintf("waiting for CNPG cleanup before restoring %s/%s", cnpgNamespace, cnpgClusterName),
		), statusChanged, nil
	}

	cluster := cnpgClusterObject()

	err := c.client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: cnpgNamespace, Name: cnpgClusterName}, cluster)
	if err == nil {
		if cluster.GetDeletionTimestamp() != nil {
			if err := c.deleteCNPGPodsAndPVCs(ctx); err != nil {
				return subroutines.OK(), statusChanged, err
			}

			log.Info().
				Str("subroutine", cnpgRestoreSubroutineName).
				Str("platformRestore", platformRestore.Name).
				Str("cluster", cnpgClusterName).
				Msg("CNPG Cluster is still deleting")

			return subroutines.StopWithRequeue(
				10*time.Second,
				fmt.Sprintf("waiting for CNPG Cluster %s/%s to be deleted", cnpgNamespace, cnpgClusterName),
			), statusChanged, nil
		}

		if cnpgClusterReady(cluster) {
			log.Info().
				Str("subroutine", cnpgRestoreSubroutineName).
				Str("platformRestore", platformRestore.Name).
				Str("cluster", cnpgClusterName).
				Msg("CNPG Cluster is ready")

			if markCNPGRestoreCompleted(platformRestore) {
				statusChanged = true
			}

			return subroutines.OK(), statusChanged, nil
		}

		log.Info().
			Str("subroutine", cnpgRestoreSubroutineName).
			Str("platformRestore", platformRestore.Name).
			Str("cluster", cnpgClusterName).
			Msg("CNPG Cluster exists but is not ready yet")

		return subroutines.StopWithRequeue(
			10*time.Second,
			fmt.Sprintf("waiting for CNPG Cluster %s/%s", cnpgNamespace, cnpgClusterName),
		), statusChanged, nil
	}

	if !apierrors.IsNotFound(err) {
		return subroutines.OK(), statusChanged, fmt.Errorf("failed to get CNPG Cluster %s/%s: %w", cnpgNamespace, cnpgClusterName, err)
	}

	gone, err := c.cnpgPodsAndPVCsGone(ctx)
	if err != nil {
		return subroutines.OK(), statusChanged, err
	}

	if !gone {
		return subroutines.StopWithRequeue(
			10*time.Second,
			fmt.Sprintf("waiting for CNPG pods/PVCs for %s/%s to be deleted", cnpgNamespace, cnpgClusterName),
		), statusChanged, nil
	}

	cluster = cnpgClusterObject()
	cluster.Object["spec"] = cnpgRecoverySpec(platformRestore)

	if err := c.client.Create(ctx, cluster); err != nil {
		return subroutines.OK(), statusChanged, fmt.Errorf("failed to create CNPG restore Cluster %s/%s: %w", cnpgNamespace, cnpgClusterName, err)
	}

	log.Info().
		Str("subroutine", cnpgRestoreSubroutineName).
		Str("platformRestore", platformRestore.Name).
		Str("cluster", cnpgClusterName).
		Msg("created CNPG restore Cluster")

	return subroutines.StopWithRequeue(10*time.Second, fmt.Sprintf("waiting for CNPG Cluster %s/%s", cnpgNamespace, cnpgClusterName)), statusChanged, nil
}

func cnpgClusterObject() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion("postgresql.cnpg.io/v1")
	obj.SetKind("Cluster")
	obj.SetNamespace(cnpgNamespace)
	obj.SetName(cnpgClusterName)

	obj.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": "platform-mesh-backup-operator",
		"app.kubernetes.io/part-of":    "platform-mesh",
	})

	return obj
}

func cnpgRecoverySpec(restore *v1alpha1.PlatformRestore) map[string]any {
	endpoint := restore.Spec.Source.Storage.S3.Endpoint
	// CNPG capture snapshots the completed base backup and WAL archive under
	// this PlatformBackup-specific prefix. Never recover from the live source
	// archive, whose WAL objects can be changed by a later database system.
	destinationPath := fmt.Sprintf(
		"s3://%s/%s",
		cnpgBackupBucket,
		cnpgArchiveRoot(restore.Spec.Source.BackupID),
	)

	return map[string]any{
		"instances": int64(2),
		"storage": map[string]any{
			"size": "1Gi",
		},
		"bootstrap": map[string]any{
			"recovery": map[string]any{
				"source": cnpgSourceName,
			},
		},
		"externalClusters": []any{
			map[string]any{
				"name": cnpgSourceName,
				"barmanObjectStore": map[string]any{
					"destinationPath": destinationPath,
					"endpointURL":     endpoint,
					"serverName":      cnpgSourceName,
					"s3Credentials": map[string]any{
						"accessKeyId": map[string]any{
							"name": cnpgSecretName,
							"key":  cnpgAccessKeyIDSecretKey,
						},
						"secretAccessKey": map[string]any{
							"name": cnpgSecretName,
							"key":  cnpgSecretAccessKeySecretKey,
						},
					},
				},
			},
		},
	}
}

func cnpgArchiveRoot(platformBackupName string) string {
	return path.Join("platform-mesh", "cnpg", platformBackupName)
}

func cnpgClusterReady(cluster *unstructured.Unstructured) bool {
	conditions, found, err := unstructured.NestedSlice(cluster.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}

	for _, item := range conditions {
		condition, ok := item.(map[string]any)
		if !ok {
			continue
		}

		if condition["type"] == "Ready" && condition["status"] == "True" {
			return true
		}
	}

	return false
}

func (c *CNPGRestoreSubroutine) deleteCNPGRuntimeObjects(ctx context.Context) error {
	cluster := cnpgClusterObject()

	err := c.client.Get(ctx, ctrlruntimeclient.ObjectKey{
		Namespace: cnpgNamespace,
		Name:      cnpgClusterName,
	}, cluster)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get CNPG Cluster %s/%s before cleanup: %w", cnpgNamespace, cnpgClusterName, err)
	}

	if err == nil {
		if deleteErr := c.client.Delete(ctx, cluster); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
			return fmt.Errorf("failed to delete CNPG Cluster %s/%s: %w", cnpgNamespace, cnpgClusterName, deleteErr)
		}
	}

	if err := c.deleteCNPGPodsAndPVCs(ctx); err != nil {
		return err
	}

	return nil
}

func (c *CNPGRestoreSubroutine) deleteCNPGPodsAndPVCs(ctx context.Context) error {
	grace := int64(0)

	pods := &corev1.PodList{}
	if err := c.client.List(ctx, pods,
		ctrlruntimeclient.InNamespace(cnpgNamespace),
		ctrlruntimeclient.MatchingLabels{cnpgClusterLabel: cnpgClusterName},
	); err != nil {
		return fmt.Errorf("failed to list CNPG pods in namespace %s: %w", cnpgNamespace, err)
	}

	for i := range pods.Items {
		pod := &pods.Items[i]

		if err := c.client.Delete(ctx, pod, &ctrlruntimeclient.DeleteOptions{
			GracePeriodSeconds: &grace,
		}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete CNPG pod %s/%s: %w", pod.Namespace, pod.Name, err)
		}
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	if err := c.client.List(ctx, pvcs,
		ctrlruntimeclient.InNamespace(cnpgNamespace),
		ctrlruntimeclient.MatchingLabels{cnpgClusterLabel: cnpgClusterName},
	); err != nil {
		return fmt.Errorf("failed to list CNPG PVCs in namespace %s: %w", cnpgNamespace, err)
	}

	for i := range pvcs.Items {
		pvc := &pvcs.Items[i]

		if err := c.client.Delete(ctx, pvc); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete CNPG PVC %s/%s: %w", pvc.Namespace, pvc.Name, err)
		}
	}

	return nil
}

func cnpgRestoreCleanupStarted(platformRestore *v1alpha1.PlatformRestore) bool {
	condition := meta.FindStatusCondition(platformRestore.Status.Conditions, cnpgRestoreCleanupStartedCondition)
	return condition != nil &&
		condition.Status == metav1.ConditionTrue &&
		condition.ObservedGeneration == platformRestore.Generation
}

func (c *CNPGRestoreSubroutine) cnpgPodsAndPVCsGone(ctx context.Context) (bool, error) {
	pods := &corev1.PodList{}
	if err := c.client.List(ctx, pods,
		ctrlruntimeclient.InNamespace(cnpgNamespace),
		ctrlruntimeclient.MatchingLabels{cnpgClusterLabel: cnpgClusterName},
	); err != nil {
		return false, fmt.Errorf("failed to list CNPG pods in namespace %s: %w", cnpgNamespace, err)
	}

	if len(pods.Items) > 0 {
		return false, nil
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	if err := c.client.List(ctx, pvcs,
		ctrlruntimeclient.InNamespace(cnpgNamespace),
		ctrlruntimeclient.MatchingLabels{cnpgClusterLabel: cnpgClusterName},
	); err != nil {
		return false, fmt.Errorf("failed to list CNPG PVCs in namespace %s: %w", cnpgNamespace, err)
	}

	return len(pvcs.Items) == 0, nil
}

func cnpgRestoreCompleted(platformRestore *v1alpha1.PlatformRestore) bool {
	condition := meta.FindStatusCondition(
		platformRestore.Status.Conditions,
		cnpgRestoreCompletedCondition,
	)

	return condition != nil &&
		condition.Status == metav1.ConditionTrue &&
		condition.ObservedGeneration == platformRestore.Generation
}

func markCNPGRestoreCompleted(platformRestore *v1alpha1.PlatformRestore) bool {
	statusChanged := setPhase(platformRestore, v1alpha1.RestorePhaseRestoringCNPG)

	if markPhaseReady(
		platformRestore,
		cnpgRestoreCompletedCondition,
		"AutomatedCNPGRestoreCompleted",
		"CNPG cluster was restored and is ready",
	) {
		statusChanged = true
	}

	return statusChanged
}
