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

package restore

import (
	"context"
	"fmt"
	"sort"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ConditionCNPGRestored is set on PlatformRestore when all CNPG clusters have been restored.
	ConditionCNPGRestored = "CNPGRestored"

	// annotationRestoredFromCNPGBackup records which CNPG Backup CR was used to restore this Cluster.
	annotationRestoredFromCNPGBackup = "backup.platform-mesh.io/restored-from-cnpg-backup"
)

// CNPGRestoreSubroutine recreates each CNPG Cluster with bootstrap.recovery pointing at
// the backup recorded in the source PlatformBackup artefacts. The subroutine is non-blocking:
// each reconcile does one unit of work per cluster and returns Pending(requeueInterval) until all clusters
// are healthy.
type CNPGRestoreSubroutine struct {
	cnpgNamespace   string // namespace where Cluster CRs live
	requeueInterval time.Duration
}

func NewCNPGRestoreSubroutine(_, cnpgNamespace string) *CNPGRestoreSubroutine {
	return &CNPGRestoreSubroutine{
		cnpgNamespace:   cnpgNamespace,
		requeueInterval: 10 * time.Second,
	}
}

// WithPollIntervals overrides the requeue interval (for tests).
func (s *CNPGRestoreSubroutine) WithPollIntervals(interval, _ time.Duration) *CNPGRestoreSubroutine {
	s.requeueInterval = interval
	return s
}

func (s *CNPGRestoreSubroutine) GetName() string { return ConditionCNPGRestored }

func (s *CNPGRestoreSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	rst, ok := obj.(*pmbackupv1alpha1.PlatformRestore)
	if !ok {
		return subroutines.OK(), fmt.Errorf("unexpected object type %T", obj)
	}

	if apimeta.IsStatusConditionTrue(rst.Status.Conditions, ConditionCNPGRestored) {
		return subroutines.OK(), nil
	}

	cl := subroutines.MustClientFromContext(ctx)
	log := logger.LoadLoggerFromContext(ctx)

	var bkp pmbackupv1alpha1.PlatformBackup
	if err := cl.Get(ctx, types.NamespacedName{Name: rst.Spec.Source.BackupID}, &bkp); err != nil {
		if apierrors.IsNotFound(err) {
			return subroutines.StopWithRequeue(30*time.Second,
				fmt.Sprintf("source backup %q not found, requeueing", rst.Spec.Source.BackupID)), nil
		}
		return subroutines.OK(), fmt.Errorf("fetching source backup %q: %w", rst.Spec.Source.BackupID, err)
	}

	if bkp.Status.Artefacts.CNPG == nil || len(bkp.Status.Artefacts.CNPG.Backups) == 0 {
		return subroutines.OK(), nil
	}

	log.Info().Str("restore", rst.Name).Str("backup", bkp.Name).
		Int("clusterCount", len(bkp.Status.Artefacts.CNPG.Backups)).Msg("starting cnpg restore")

	// Process in deterministic order.
	clusterNames := make([]string, 0, len(bkp.Status.Artefacts.CNPG.Backups))
	for name := range bkp.Status.Artefacts.CNPG.Backups {
		clusterNames = append(clusterNames, name)
	}
	sort.Strings(clusterNames)

	pendingMsg := ""
	var errs []error

	for _, clusterName := range clusterNames {
		backupCRName := bkp.Status.Artefacts.CNPG.Backups[clusterName]

		var cluster cnpgv1.Cluster
		err := cl.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: s.cnpgNamespace}, &cluster)

		switch {
		case err != nil && !apierrors.IsNotFound(err):
			errs = append(errs, fmt.Errorf("fetching Cluster %q: %w", clusterName, err))

		case apierrors.IsNotFound(err):
			// The cluster may be transiently absent immediately after a delete+create
			// cycle before the API server reflects the new object. Requeue instead of
			// surfacing a permanent error that would require manual intervention.
			if pendingMsg == "" {
				pendingMsg = fmt.Sprintf("cluster %q not yet visible — waiting for it to appear", clusterName)
			}

		case cluster.DeletionTimestamp != nil:
			s.stripClusterFinalizers(ctx, cl, &cluster, log)
			if pendingMsg == "" {
				pendingMsg = fmt.Sprintf("waiting for Cluster %q deletion to complete", clusterName)
			}

		case cluster.Annotations != nil && cluster.Annotations[annotationRestoredFromCNPGBackup] == backupCRName:
			if cluster.Status.ReadyInstances > 0 && cluster.Status.ReadyInstances == cluster.Spec.Instances {
				continue // done
			}
			if pendingMsg == "" {
				pendingMsg = fmt.Sprintf("waiting for Cluster %q to become ready (%d/%d instances ready)",
					clusterName, cluster.Status.ReadyInstances, cluster.Spec.Instances)
			}

		default:
			savedSpec := *cluster.Spec.DeepCopy()
			savedLabels := cluster.Labels

			log.Info().Str("cluster", clusterName).Str("backupCR", backupCRName).
				Msg("deleting Cluster CR for restore")

			s.stripClusterFinalizers(ctx, cl, &cluster, log)
			gracePeriod := int64(0)
			if delErr := cl.Delete(ctx, &cluster, &ctrlruntimeclient.DeleteOptions{GracePeriodSeconds: &gracePeriod}); delErr != nil && !apierrors.IsNotFound(delErr) {
				errs = append(errs, fmt.Errorf("deleting Cluster %q: %w", clusterName, delErr))
				break
			}

			// Clear the backup configuration so the recovered cluster does not try
			// to WAL-archive into the same S3 path as the source backup. CNPG's
			// barman-cloud-check-wal-archive rejects a non-empty archive path,
			// causing the recovery job to fail with "Expected empty archive".
			savedSpec.Backup = nil

			savedSpec.Bootstrap = &cnpgv1.BootstrapConfiguration{
				Recovery: &cnpgv1.BootstrapRecovery{
					Backup: &cnpgv1.BackupSource{
						LocalObjectReference: cnpgv1.LocalObjectReference{Name: backupCRName},
					},
				},
			}

			annotations := map[string]string{annotationRestoredFromCNPGBackup: backupCRName}
			newCluster := &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:        clusterName,
					Namespace:   s.cnpgNamespace,
					Labels:      savedLabels,
					Annotations: annotations,
				},
				Spec: savedSpec,
			}

			if createErr := cl.Create(ctx, newCluster); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
				errs = append(errs, fmt.Errorf("recreating Cluster %q: %w", clusterName, createErr))
				break
			} else if apierrors.IsAlreadyExists(createErr) {
				// old cluster still terminating — patch annotation so next reconcile tracks it
				var existing cnpgv1.Cluster
				if getErr := cl.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: s.cnpgNamespace}, &existing); getErr == nil && existing.DeletionTimestamp == nil {
					patch := ctrlruntimeclient.MergeFrom(existing.DeepCopy())
					if existing.Annotations == nil {
						existing.Annotations = make(map[string]string)
					}
					existing.Annotations[annotationRestoredFromCNPGBackup] = backupCRName
					if patchErr := cl.Patch(ctx, &existing, patch); patchErr != nil {
						errs = append(errs, fmt.Errorf("patching annotation on terminating Cluster %q: %w", clusterName, patchErr))
						break
					}
				}
			}

			if pendingMsg == "" {
				pendingMsg = fmt.Sprintf("restoring Cluster %q", clusterName)
			}
		}
	}

	if len(errs) > 0 {
		return subroutines.OK(), combineErrors(errs)
	}
	if pendingMsg != "" {
		return subroutines.Pending(s.requeueInterval, pendingMsg), nil
	}

	log.Info().Str("restore", rst.Name).Int("clusterCount", len(clusterNames)).Msg("cnpg restore complete")
	return subroutines.OK(), nil
}

func (s *CNPGRestoreSubroutine) stripClusterFinalizers(ctx context.Context, cl ctrlruntimeclient.Client, cluster *cnpgv1.Cluster, log *logger.Logger) {
	if len(cluster.Finalizers) == 0 {
		return
	}
	patch := ctrlruntimeclient.MergeFrom(cluster.DeepCopy())
	cluster.Finalizers = nil
	if err := cl.Patch(ctx, cluster, patch); err != nil && !apierrors.IsNotFound(err) {
		log.Warn().Str("cluster", cluster.Name).Err(err).Msg("failed to strip finalizers from Cluster CR")
	}
}
