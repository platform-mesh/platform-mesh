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

// Package backup has the subroutines of the Backup controller. They run in order:
// Start, Preflight, EtcdSnapshot, Archive. The backup command runs them too.
package backup

import (
	"context"
	"time"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/storage"
	"go.platform-mesh.io/subroutines"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const waitInterval = 5 * time.Second

// Open returns the bucket and the encryption key for the archive of a Backup.
type Open func(ctx context.Context, b *pmbackupv1alpha1.Backup) (*storage.Opened, error)

// FromBackupStorage opens the BackupStorage of spec.storageRef.
func FromBackupStorage(cl ctrlruntimeclient.Client, stores storage.Factory) Open {
	return func(ctx context.Context, b *pmbackupv1alpha1.Backup) (*storage.Opened, error) {
		return storage.OpenBackupStorage(ctx, cl, stores, b.Namespace, b.Spec.StorageRef.Name)
	}
}

// New returns the subroutines of the Backup controller.
func New(cl ctrlruntimeclient.Client, stores storage.Factory, open Open) []subroutines.Subroutine {
	return []subroutines.Subroutine{
		&Start{},
		&Preflight{cl: cl, open: open},
		&EtcdSnapshot{cl: cl, stores: stores},
		&Archive{cl: cl, stores: stores, open: open},
	}
}

// fail ends the backup. It is not retried.
func fail(b *pmbackupv1alpha1.Backup, msg string) (subroutines.Result, error) {
	now := metav1.Now()
	b.Status.Phase = pmbackupv1alpha1.PhaseFailed
	b.Status.Message = msg
	b.Status.CompletionTime = &now
	return subroutines.Stop(msg), nil
}

// Start marks the backup as running, and skips everything when it is finished.
type Start struct{}

func (s *Start) GetName() string { return "Start" }

func (s *Start) Process(_ context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	b := obj.(*pmbackupv1alpha1.Backup)
	if b.Status.Phase.IsFinished() {
		return subroutines.SkipAll(b.Status.Phase == pmbackupv1alpha1.PhaseCompleted, b.Status.Message), nil
	}
	if b.Status.Phase == "" {
		now := metav1.Now()
		b.Status.Phase = pmbackupv1alpha1.PhaseRunning
		b.Status.StartTime = &now
		b.Status.Message = "backup started"
	}
	return subroutines.OK(), nil
}
