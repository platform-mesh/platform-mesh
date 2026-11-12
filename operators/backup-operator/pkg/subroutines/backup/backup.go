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
	"fmt"
	"time"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/crypto"
	"go.platform-mesh.io/backup-operator/pkg/storage"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const waitInterval = 5 * time.Second

// Target is where the archive of a backup is written, with its encryption key.
type Target struct {
	Store  storage.Store
	Prefix string
	Key    []byte
}

// Destination finds the Target of a Backup. The backup command sets Fixed, the controller
// reads the BackupStorage of spec.storageRef.
type Destination struct {
	Fixed    *Target
	cl       ctrlruntimeclient.Client
	newStore func(storage.S3Config) (storage.Store, error)
}

// FromBackupStorage reads spec.storageRef. newStore is storage.NewS3, or a memory store in tests.
func FromBackupStorage(cl ctrlruntimeclient.Client, newStore func(storage.S3Config) (storage.Store, error)) *Destination {
	return &Destination{cl: cl, newStore: newStore}
}

// invalidStorage is a problem in the BackupStorage that a retry does not fix.
type invalidStorage struct{ msg string }

func (e invalidStorage) Error() string { return e.msg }

// For returns the Target of b. Errors of the API server are returned as they are, so they are retried.
func (d *Destination) For(ctx context.Context, b *pmbackupv1alpha1.Backup) (*Target, error) {
	if d.Fixed != nil {
		return d.Fixed, nil
	}
	bs := &pmbackupv1alpha1.BackupStorage{}
	if err := d.get(ctx, b.Namespace, b.Spec.StorageRef.Name, bs, "backupstorage"); err != nil {
		return nil, err
	}
	creds := &corev1.Secret{}
	if err := d.get(ctx, b.Namespace, bs.Spec.CredentialsSecretRef.Name, creds, "credentials secret"); err != nil {
		return nil, err
	}
	keySecret := &corev1.Secret{}
	if err := d.get(ctx, b.Namespace, bs.Spec.EncryptionKeySecretRef.Name, keySecret, "encryption key secret"); err != nil {
		return nil, err
	}
	key := keySecret.Data["key"]
	if len(key) != crypto.KeySize {
		return nil, invalidStorage{fmt.Sprintf("encryption key secret %s: key must be %d bytes, got %d", keySecret.Name, crypto.KeySize, len(key))}
	}
	s, err := d.newStore(storage.S3Config{
		Endpoint:        bs.Spec.Endpoint,
		Region:          bs.Spec.Region,
		Bucket:          bs.Spec.Bucket,
		AccessKeyID:     string(creds.Data["accessKeyID"]),
		SecretAccessKey: string(creds.Data["secretAccessKey"]),
		ForcePathStyle:  bs.Spec.ForcePathStyle,
	})
	if err != nil {
		return nil, invalidStorage{fmt.Sprintf("backupstorage %s: %v", bs.Name, err)}
	}
	return &Target{Store: s, Prefix: bs.Spec.Prefix, Key: key}, nil
}

func (d *Destination) get(ctx context.Context, namespace, name string, obj ctrlruntimeclient.Object, what string) error {
	err := d.cl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, obj)
	if apierrors.IsNotFound(err) {
		return invalidStorage{fmt.Sprintf("%s %s not found", what, name)}
	}
	if err != nil {
		return fmt.Errorf("getting %s %s: %w", what, name, err)
	}
	return nil
}

// New returns the subroutines of the Backup controller.
func New(cl ctrlruntimeclient.Client, stores storage.Factory, dest *Destination) []subroutines.Subroutine {
	return []subroutines.Subroutine{
		&Start{},
		&Preflight{cl: cl, dest: dest},
		&EtcdSnapshot{cl: cl, stores: stores},
		&Archive{cl: cl, stores: stores, dest: dest},
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
