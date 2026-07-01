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
	"time"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/velero"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ConditionVeleroBackedUp is set on PlatformBackup when Velero has completed a backup.
	ConditionVeleroBackedUp = "VeleroBackedUp"
)

// VeleroCaptureSubroutine ensures a BackupStorageLocation exists and creates a Velero Backup CR,
// returning Pending until it reaches the Completed phase. Non-blocking: each reconcile does one
// unit of work and returns Pending(requeueInterval) so the reconciler goroutine is never held.
// The backup name is recorded in PlatformBackup.Status.Artefacts.Velero.
type VeleroCaptureSubroutine struct {
	namespace       string
	requeueInterval time.Duration
}

func NewVeleroCaptureSubroutine(namespace string) *VeleroCaptureSubroutine {
	return &VeleroCaptureSubroutine{
		namespace:       namespace,
		requeueInterval: 10 * time.Second,
	}
}

// WithPollIntervals overrides the requeue interval (for tests). The timeout parameter is ignored.
func (s *VeleroCaptureSubroutine) WithPollIntervals(interval, _ time.Duration) *VeleroCaptureSubroutine {
	s.requeueInterval = interval
	return s
}

func (s *VeleroCaptureSubroutine) GetName() string { return ConditionVeleroBackedUp }

func (s *VeleroCaptureSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	bkp, ok := obj.(*pmbackupv1alpha1.PlatformBackup)
	if !ok {
		return subroutines.OK(), fmt.Errorf("unexpected object type %T", obj)
	}

	if !bkp.Spec.Components.Velero.Enabled {
		return subroutines.OK(), nil
	}

	if bkp.Status.Artefacts.Velero != nil && bkp.Status.Artefacts.Velero.BackupName != "" {
		return subroutines.OK(), nil
	}

	cl := subroutines.MustClientFromContext(ctx)
	log := logger.LoadLoggerFromContext(ctx)

	s3 := bkp.Spec.Storage.S3
	if err := velero.EnsureBSL(ctx, cl, s.namespace, s3.Endpoint, s3.Bucket, s3.CredentialsRef.Name); err != nil {
		return subroutines.OK(), fmt.Errorf("ensuring BackupStorageLocation: %w", err)
	}

	// Ensure the Velero Backup CR exists.
	vBackup := &velerov1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bkp.Name,
			Namespace: s.namespace,
		},
		Spec: velerov1.BackupSpec{
			StorageLocation: velero.BSLName(),
		},
	}
	if err := cl.Create(ctx, vBackup); err != nil && !apierrors.IsAlreadyExists(err) {
		return subroutines.OK(), fmt.Errorf("creating Velero Backup: %w", err)
	}

	// Check the current phase.
	var b velerov1.Backup
	if err := cl.Get(ctx, types.NamespacedName{Name: bkp.Name, Namespace: s.namespace}, &b); err != nil {
		return subroutines.OK(), fmt.Errorf("polling Velero Backup: %w", err)
	}

	switch b.Status.Phase {
	case velerov1.BackupPhaseCompleted:
		bkp.Status.Artefacts.Velero = &pmbackupv1alpha1.VeleroArtefact{BackupName: bkp.Name}
		log.Info().Str("backup", bkp.Name).Msg("Velero backup complete")
		return subroutines.OK(), nil
	case velerov1.BackupPhaseFailed, velerov1.BackupPhaseFailedValidation:
		_ = cl.Delete(ctx, &b)
		return subroutines.OK(), fmt.Errorf("velero Backup %q failed with phase %q", bkp.Name, b.Status.Phase)
	default:
		return subroutines.Pending(s.requeueInterval,
			fmt.Sprintf("waiting for Velero Backup %q to complete (phase=%q)", bkp.Name, b.Status.Phase)), nil
	}
}
