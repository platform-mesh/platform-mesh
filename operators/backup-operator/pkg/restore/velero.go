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
	"time"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/velero"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ConditionVeleroRestored is set on PlatformRestore when Velero has completed a restore.
	ConditionVeleroRestored = "VeleroRestored"
)

// VeleroRestoreSubroutine creates a Velero Restore CR referencing the backup recorded in
// the source PlatformBackup artefacts and returns Pending until it reaches the Completed
// phase. Non-blocking: each reconcile does one unit of work and returns
// Pending(requeueInterval) so the reconciler goroutine is never held.
type VeleroRestoreSubroutine struct {
	namespace       string
	requeueInterval time.Duration
}

func NewVeleroRestoreSubroutine(namespace string) *VeleroRestoreSubroutine {
	return &VeleroRestoreSubroutine{
		namespace:       namespace,
		requeueInterval: 10 * time.Second,
	}
}

// WithPollIntervals overrides the requeue interval (for tests). The timeout parameter is ignored.
func (s *VeleroRestoreSubroutine) WithPollIntervals(interval, _ time.Duration) *VeleroRestoreSubroutine {
	s.requeueInterval = interval
	return s
}

func (s *VeleroRestoreSubroutine) GetName() string { return ConditionVeleroRestored }

func (s *VeleroRestoreSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	rst, ok := obj.(*pmbackupv1alpha1.PlatformRestore)
	if !ok {
		return subroutines.OK(), fmt.Errorf("unexpected object type %T", obj)
	}

	if apimeta.IsStatusConditionTrue(rst.Status.Conditions, ConditionVeleroRestored) {
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

	if bkp.Status.Artefacts.Velero == nil || bkp.Status.Artefacts.Velero.BackupName == "" {
		return subroutines.OK(), nil
	}

	s3 := rst.Spec.Source.Storage.S3
	if err := velero.EnsureBSL(ctx, cl, s.namespace, s3.Endpoint, s3.Bucket, s3.CredentialsRef.Name); err != nil {
		return subroutines.OK(), fmt.Errorf("ensuring BackupStorageLocation: %w", err)
	}

	backupName := bkp.Status.Artefacts.Velero.BackupName

	// Ensure the Velero Restore CR exists.
	vRestore := &velerov1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rst.Name,
			Namespace: s.namespace,
		},
		Spec: velerov1.RestoreSpec{
			BackupName: backupName,
		},
	}
	if err := cl.Create(ctx, vRestore); err != nil && !apierrors.IsAlreadyExists(err) {
		return subroutines.OK(), fmt.Errorf("creating Velero Restore: %w", err)
	}

	// Check the current phase.
	var r velerov1.Restore
	if err := cl.Get(ctx, types.NamespacedName{Name: rst.Name, Namespace: s.namespace}, &r); err != nil {
		return subroutines.OK(), fmt.Errorf("polling Velero Restore: %w", err)
	}

	switch r.Status.Phase {
	case velerov1.RestorePhaseCompleted:
		log.Info().Str("restore", rst.Name).Msg("Velero restore complete")
		return subroutines.OK(), nil
	case velerov1.RestorePhaseFailed, velerov1.RestorePhaseFailedValidation:
		return subroutines.OK(), fmt.Errorf("velero Restore %q failed with phase %q", rst.Name, r.Status.Phase)
	default:
		return subroutines.Pending(s.requeueInterval,
			fmt.Sprintf("waiting for Velero Restore %q to complete (phase=%q)", rst.Name, r.Status.Phase)), nil
	}
}
