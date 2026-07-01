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

package backup

import (
	"context"
	"fmt"
	"strings"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ConditionCNPGSnapshotted is set on PlatformBackup when all CNPG clusters have been snapshotted.
	ConditionCNPGSnapshotted = "CNPGSnapshotted"
)

// CNPGCaptureSubroutine creates an on-demand cnpg Backup CR for every configured
// Postgres cluster and returns Pending until each reaches the Completed phase.
// Snapshot names are recorded in PlatformBackup.Status.Artefacts.CNPG.Backups
// keyed by cluster name. Each reconcile does one unit of work and returns
// Pending(requeueInterval) so the reconciler goroutine is never held.
//
// When clusters is nil or empty the subroutine discovers all CNPG Cluster CRs in
// cnpgNamespace at reconcile time instead of using a static list.
type CNPGCaptureSubroutine struct {
	cnpgNamespace   string // namespace where Cluster CRs live
	clusters        []string
	requeueInterval time.Duration
}

func NewCNPGCaptureSubroutine(cnpgNamespace string, clusters []string) *CNPGCaptureSubroutine {
	return &CNPGCaptureSubroutine{
		cnpgNamespace:   cnpgNamespace,
		clusters:        clusters,
		requeueInterval: 5 * time.Second,
	}
}

// WithPollIntervals overrides the requeue interval (for tests).
// Only the first argument (interval) is used; timeout is ignored.
func (s *CNPGCaptureSubroutine) WithPollIntervals(interval, _ time.Duration) *CNPGCaptureSubroutine {
	s.requeueInterval = interval
	return s
}

func (s *CNPGCaptureSubroutine) GetName() string { return ConditionCNPGSnapshotted }

func (s *CNPGCaptureSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	bkp, ok := obj.(*pmbackupv1alpha1.PlatformBackup)
	if !ok {
		return subroutines.OK(), fmt.Errorf("unexpected object type %T", obj)
	}

	if !bkp.Spec.Components.CNPG.Enabled {
		return subroutines.OK(), nil
	}

	if bkp.Status.Artefacts.CNPG != nil && len(bkp.Status.Artefacts.CNPG.Backups) > 0 {
		return subroutines.OK(), nil
	}

	cl := subroutines.MustClientFromContext(ctx)
	log := logger.LoadLoggerFromContext(ctx)

	clusters := s.clusters
	if len(clusters) == 0 {
		var clusterList cnpgv1.ClusterList
		if err := cl.List(ctx, &clusterList, ctrlruntimeclient.InNamespace(s.cnpgNamespace)); err != nil {
			return subroutines.OK(), fmt.Errorf("listing CNPG clusters in %q: %w", s.cnpgNamespace, err)
		}
		if len(clusterList.Items) == 0 {
			return subroutines.StopWithRequeue(30*time.Second, "no CNPG Cluster CRs found in namespace "+s.cnpgNamespace), nil
		}
		for i := range clusterList.Items {
			clusters = append(clusters, clusterList.Items[i].Name)
		}
	}

	log.Info().Str("backup", bkp.Name).Strs("clusters", clusters).Msg("starting cnpg snapshot")

	backups := make(map[string]string, len(clusters))
	pendingMsg := ""
	var errs []string

	for _, clusterName := range clusters {
		backupCRName := cnpgBackupName(bkp.Name, clusterName)

		var b cnpgv1.Backup
		err := cl.Get(ctx, types.NamespacedName{Name: backupCRName, Namespace: s.cnpgNamespace}, &b)

		switch {
		case apierrors.IsNotFound(err):
			cnpgBackup := &cnpgv1.Backup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      backupCRName,
					Namespace: s.cnpgNamespace,
				},
				Spec: cnpgv1.BackupSpec{
					Cluster: cnpgv1.LocalObjectReference{Name: clusterName},
					Method:  cnpgv1.BackupMethodBarmanObjectStore,
				},
			}
			if createErr := cl.Create(ctx, cnpgBackup); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
				errs = append(errs, fmt.Sprintf("cluster %q: creating Backup CR: %v", clusterName, createErr))
				continue
			}
			if pendingMsg == "" {
				pendingMsg = fmt.Sprintf("waiting for Backup CR %q to complete", backupCRName)
			}

		case err != nil:
			errs = append(errs, fmt.Sprintf("cluster %q: polling Backup CR: %v", clusterName, err))

		case b.Status.Phase == cnpgv1.BackupPhaseCompleted:
			backups[clusterName] = backupCRName

		case b.Status.Phase == cnpgv1.BackupPhaseFailed:
			_ = cl.Delete(ctx, &b)
			errs = append(errs, fmt.Sprintf("cluster %q: backup CR %q failed: %s", clusterName, backupCRName, b.Status.Error))

		default:
			if pendingMsg == "" {
				pendingMsg = fmt.Sprintf("waiting for Backup CR %q to complete (phase=%q)", backupCRName, b.Status.Phase)
			}
		}
	}

	if len(errs) > 0 {
		return subroutines.OK(), fmt.Errorf("cnpg snapshot failed: %s", strings.Join(errs, "; "))
	}
	if pendingMsg != "" {
		return subroutines.Pending(s.requeueInterval, pendingMsg), nil
	}

	bkp.Status.Artefacts.CNPG = &pmbackupv1alpha1.CNPGArtefact{Backups: backups}
	log.Info().Str("backup", bkp.Name).Int("clusterCount", len(backups)).Msg("cnpg snapshot complete")
	return subroutines.OK(), nil
}

// cnpgBackupName produces a deterministic Backup CR name from the PlatformBackup and cluster names.
func cnpgBackupName(backupName, clusterName string) string {
	name := backupName + "-" + clusterName
	if len(name) > 253 {
		name = strings.TrimRight(name[:253], "-")
	}
	return name
}
