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
	"errors"
	"fmt"
	"sync"
	"time"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// AnnotationKeyRestoredFromSnapshot records the snapshot key from which this Etcd CR
	// was restored. etcdbr automatically restores from the latest snapshot found at
	// Spec.Backup.Store.Prefix when the pod starts; this annotation makes the specific
	// snapshot used by that restore traceable and drives the idempotency check so a
	// completed shard is not deleted and recreated again on the next reconcile.
	AnnotationKeyRestoredFromSnapshot = "backup.platform-mesh.io/restored-from-snapshot"

	// ConditionEtcdRestored is set on PlatformRestore when all shards have been restored.
	ConditionEtcdRestored = "EtcdRestored"
)

// EtcdRestoreSubroutine deletes and recreates each KCP-shard Etcd CR from the snapshot
// keys recorded in the source PlatformBackup, then waits for each Etcd CR to become ready.
// Shards are processed concurrently to minimise total restore wall-clock time.
type EtcdRestoreSubroutine struct {
	namespace     string
	deletePollInt time.Duration
	deleteTimeout time.Duration
	readyPollInt  time.Duration
	readyTimeout  time.Duration
}

// NewEtcdRestoreSubroutine returns an EtcdRestoreSubroutine for the given namespace.
func NewEtcdRestoreSubroutine(namespace string) *EtcdRestoreSubroutine {
	return &EtcdRestoreSubroutine{
		namespace:     namespace,
		deletePollInt: 5 * time.Second,
		deleteTimeout: 5 * time.Minute,
		readyPollInt:  10 * time.Second,
		readyTimeout:  20 * time.Minute,
	}
}

// WithPollIntervals overrides poll intervals and timeouts (for testing).
func (s *EtcdRestoreSubroutine) WithPollIntervals(deletePoll, deleteTO, readyPoll, readyTO time.Duration) *EtcdRestoreSubroutine {
	s.deletePollInt = deletePoll
	s.deleteTimeout = deleteTO
	s.readyPollInt = readyPoll
	s.readyTimeout = readyTO
	return s
}

func (s *EtcdRestoreSubroutine) GetName() string { return ConditionEtcdRestored }

// Process implements subroutines.Subroutine.
func (s *EtcdRestoreSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	rst, ok := obj.(*pmbackupv1alpha1.PlatformRestore)
	if !ok {
		return subroutines.OK(), fmt.Errorf("unexpected object type %T", obj)
	}

	// Idempotency: the lifecycle re-runs Process on every reconcile; skip once all
	// shards have been successfully restored (condition set to True in a prior run).
	if apimeta.IsStatusConditionTrue(rst.Status.Conditions, ConditionEtcdRestored) {
		return subroutines.OK(), nil
	}

	cl := subroutines.MustClientFromContext(ctx)

	// PlatformBackup is cluster-scoped, so no namespace in the key.
	var bkp pmbackupv1alpha1.PlatformBackup
	if err := cl.Get(ctx, types.NamespacedName{Name: rst.Spec.Source.BackupID}, &bkp); err != nil {
		if apierrors.IsNotFound(err) {
			// Halt the subroutine chain and requeue — source may not have propagated yet.
			return subroutines.StopWithRequeue(30*time.Second,
				fmt.Sprintf("source backup %q not found, requeueing", rst.Spec.Source.BackupID)), nil
		}
		return subroutines.OK(), fmt.Errorf("fetching source backup %q: %w", rst.Spec.Source.BackupID, err)
	}

	if bkp.Status.Artefacts.Etcd == nil || len(bkp.Status.Artefacts.Etcd.Shards) == 0 {
		// Source backup had etcd disabled or captured no shards — nothing to restore.
		return subroutines.OK(), nil
	}

	return subroutines.OK(), s.fanOutRestore(ctx, cl, bkp.Status.Artefacts.Etcd.Shards)
}

// shardRestoreResult holds the outcome of a single shard's restore operation.
type shardRestoreResult struct {
	shardName string
	err       error
}

// fanOutRestore restores all shards concurrently and returns a combined error if any fail.
// All-or-nothing semantics: EtcdRestored=True is only written to status when this
// function returns nil (all shards succeeded). A failure on any shard surfaces as a
// structured error on the PlatformRestore CR and no partial success is committed.
func (s *EtcdRestoreSubroutine) fanOutRestore(
	ctx context.Context,
	cl ctrlruntimeclient.Client,
	shards map[string]pmbackupv1alpha1.EtcdShardArtefact,
) error {
	// Validate all keys up-front before spawning any goroutines. An early return
	// after some goroutines are already running would leave them deleting and
	// recreating live Etcd CRs while the caller already reported failure.
	for shardName, artefact := range shards {
		if artefact.SnapshotKey == "" {
			return fmt.Errorf("shard %q has empty snapshot key; backup artefact may be corrupt", shardName)
		}
	}

	results := make(chan shardRestoreResult, len(shards))
	var wg sync.WaitGroup
	for shardName, artefact := range shards {
		wg.Add(1)
		go func(name, key string) {
			defer wg.Done()
			results <- shardRestoreResult{
				shardName: name,
				err:       s.restoreShard(ctx, cl, name, key),
			}
		}(shardName, artefact.SnapshotKey)
	}
	wg.Wait()
	close(results)

	var errs []error
	for r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Errorf("restoring shard %q: %w", r.shardName, r.err))
		}
	}
	return errors.Join(errs...)
}

// restoreShard deletes the existing Etcd CR, waits for deletion, then recreates it
// with the original spec and the restore-traceability annotation, then waits for ready.
//
// Restore mechanism: recreating the Etcd CR with the same Spec.Backup.Store.Prefix
// causes the etcdbr sidecar to automatically restore from the latest snapshot found at
// that prefix on pod startup. The snapshotKey annotation records which snapshot was
// captured so the idempotency check can recognise a completed restore on retry.
//
// Idempotency: if the CR already exists and carries the correct snapshot annotation it was
// restored in a previous (partial) reconcile — skip the delete+recreate cycle and only
// wait for ready. This prevents re-deleting a live shard on retry after another shard fails.
func (s *EtcdRestoreSubroutine) restoreShard(ctx context.Context, cl ctrlruntimeclient.Client, shardName, snapshotKey string) error {
	var existing druidv1alpha1.Etcd
	if err := cl.Get(ctx, types.NamespacedName{Name: shardName, Namespace: s.namespace}, &existing); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("etcd CR %q not found in namespace %q, cannot recreate without existing spec",
				shardName, s.namespace)
		}
		return fmt.Errorf("fetching Etcd CR: %w", err)
	}

	// Already restored in a prior reconcile — just wait for ready.
	// Guard against a nil Annotations map: safe map-read returns "" for a missing key,
	// which is always != snapshotKey (non-empty, enforced in fanOutRestore).
	if existing.Annotations != nil && existing.Annotations[AnnotationKeyRestoredFromSnapshot] == snapshotKey {
		return s.waitForReady(ctx, cl, shardName)
	}

	savedSpec := *existing.Spec.DeepCopy()
	// Deep-copy the labels map so the new CR always has an independent copy.
	// Always inject the kcp-shard label regardless of what the existing CR carries —
	// etcd-druid may strip unknown labels, and a recreated CR without this label
	// would be silently omitted from future listShards-based backups.
	savedLabels := make(map[string]string, len(existing.Labels)+1)
	for k, v := range existing.Labels {
		savedLabels[k] = v
	}
	savedLabels[backup.LabelKeyComponent] = backup.LabelComponentKCPShard

	if err := cl.Delete(ctx, &existing); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting Etcd CR: %w", err)
	}
	if err := s.waitForDeletion(ctx, cl, shardName); err != nil {
		return fmt.Errorf("waiting for Etcd CR deletion: %w", err)
	}

	return s.createAndWait(ctx, cl, shardName, savedSpec, savedLabels, snapshotKey)
}

// createAndWait creates a new Etcd CR with the given spec and labels, then waits for it
// to become ready. If the CR already exists (race with etcd-druid recreating it), both
// the restore annotation and the kcp-shard label are patched onto it before waiting —
// etcd-druid does not preserve platform-mesh labels, so a CR it recreated would otherwise
// be invisible to future listShards-based backups.
func (s *EtcdRestoreSubroutine) createAndWait(
	ctx context.Context,
	cl ctrlruntimeclient.Client,
	name string,
	spec druidv1alpha1.EtcdSpec,
	labels map[string]string,
	snapshotKey string,
) error {
	etcd := &druidv1alpha1.Etcd{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: s.namespace,
			Labels:    labels,
			Annotations: map[string]string{
				AnnotationKeyRestoredFromSnapshot: snapshotKey,
			},
		},
		Spec: spec,
	}
	if err := cl.Create(ctx, etcd); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("creating Etcd CR: %w", err)
		}
		// CR already exists (etcd-druid recreated it between waitForDeletion and Create).
		// Patch both the restore annotation and the kcp-shard label: etcd-druid does not
		// know about platform-mesh labels, so a CR it recreated won't carry them, making
		// it invisible to future listShards-based backups.
		var existing druidv1alpha1.Etcd
		if err := cl.Get(ctx, types.NamespacedName{Name: name, Namespace: s.namespace}, &existing); err != nil {
			return fmt.Errorf("fetching pre-existing Etcd CR after AlreadyExists: %w", err)
		}
		patch := ctrlruntimeclient.MergeFrom(existing.DeepCopy())
		if existing.Annotations == nil {
			existing.Annotations = map[string]string{}
		}
		existing.Annotations[AnnotationKeyRestoredFromSnapshot] = snapshotKey
		if existing.Labels == nil {
			existing.Labels = map[string]string{}
		}
		existing.Labels[backup.LabelKeyComponent] = backup.LabelComponentKCPShard
		if err := cl.Patch(ctx, &existing, patch); err != nil {
			return fmt.Errorf("patching restore annotation on pre-existing Etcd CR: %w", err)
		}
	}
	return s.waitForReady(ctx, cl, name)
}

// waitForDeletion polls until the Etcd CR is gone. Checks state before sleeping to
// avoid an unnecessary full-interval delay when the CR is already deleted.
func (s *EtcdRestoreSubroutine) waitForDeletion(ctx context.Context, cl ctrlruntimeclient.Client, name string) error {
	pollCtx, cancel := context.WithTimeout(ctx, s.deleteTimeout)
	defer cancel()

	for {
		var etcd druidv1alpha1.Etcd
		err := cl.Get(pollCtx, types.NamespacedName{Name: name, Namespace: s.namespace}, &etcd)
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("polling Etcd CR deletion: %w", err)
		}

		timer := time.NewTimer(s.deletePollInt)
		select {
		case <-pollCtx.Done():
			timer.Stop()
			return fmt.Errorf("timed out waiting for Etcd CR %q to be deleted: %w", name, pollCtx.Err())
		case <-timer.C:
		}
	}
}

// waitForReady polls until Etcd.status.ready = true. Checks state before sleeping.
// A transient NotFound (e.g. etcd-druid briefly recreating the CR) is treated as
// not-yet-ready rather than a hard failure, so the poll continues.
func (s *EtcdRestoreSubroutine) waitForReady(ctx context.Context, cl ctrlruntimeclient.Client, name string) error {
	pollCtx, cancel := context.WithTimeout(ctx, s.readyTimeout)
	defer cancel()

	for {
		var etcd druidv1alpha1.Etcd
		if err := cl.Get(pollCtx, types.NamespacedName{Name: name, Namespace: s.namespace}, &etcd); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("polling Etcd CR readiness: %w", err)
			}
			// CR transiently absent — etcd-druid may be recreating it. Fall through
			// to the sleep so we retry on the next tick.
		} else if etcd.Status.Ready != nil && *etcd.Status.Ready {
			return nil
		}

		timer := time.NewTimer(s.readyPollInt)
		select {
		case <-pollCtx.Done():
			timer.Stop()
			return fmt.Errorf("timed out waiting for Etcd CR %q to become ready: %w", name, pollCtx.Err())
		case <-timer.C:
		}
	}
}

// Ensure the backup package's label constants are accessible to restore callers
// via the restore package, avoiding duplication. Re-exported here so external
// tests and the e2e suite can reference restore.LabelKeyComponent without
// importing both packages.
const (
	LabelKeyComponent      = backup.LabelKeyComponent
	LabelComponentKCPShard = backup.LabelComponentKCPShard
)
