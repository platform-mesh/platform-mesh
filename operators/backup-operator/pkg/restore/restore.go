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

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	"go.platform-mesh.io/subroutines"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	backupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
)

const (
	// AnnotationKeyRestoredFromSnapshot is set on a recreated Etcd CR to record the
	// snapshot revision from which it was restored (traceability only).
	AnnotationKeyRestoredFromSnapshot = "backup.platform-mesh.io/restored-from-snapshot"

	// ConditionEtcdRestored is set on PlatformRestore when all shards have been restored.
	ConditionEtcdRestored = "EtcdRestored"

	// LabelKeyComponent and LabelComponentKCPShard are used to identify kcp-shard Etcd CRs
	// when verifying labels are preserved on the recreated CR.
	LabelKeyComponent      = "platform-mesh.io/component"
	LabelComponentKCPShard = "kcp-shard"
)

// EtcdRestoreSubroutine deletes and recreates each KCP-shard Etcd CR from the snapshot
// keys recorded in the source PlatformBackup, then waits for each Etcd CR to become ready.
// Shards are processed sequentially (sorted by name) to avoid overwhelming etcd-druid.
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
func (s *EtcdRestoreSubroutine) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	rst, ok := obj.(*backupv1alpha1.PlatformRestore)
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
	var bkp backupv1alpha1.PlatformBackup
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

	// Sort shard names for deterministic sequential ordering.
	shardNames := make([]string, 0, len(bkp.Status.Artefacts.Etcd.Shards))
	for name := range bkp.Status.Artefacts.Etcd.Shards {
		shardNames = append(shardNames, name)
	}
	sort.Strings(shardNames)

	for _, shardName := range shardNames {
		artefact := bkp.Status.Artefacts.Etcd.Shards[shardName]
		if artefact.SnapshotKey == "" {
			return subroutines.OK(), fmt.Errorf("shard %q has empty snapshot key in backup %q; backup artefact may be corrupt",
				shardName, bkp.Name)
		}
		if err := s.restoreShard(ctx, cl, shardName, artefact.SnapshotKey); err != nil {
			return subroutines.OK(), fmt.Errorf("restoring shard %q: %w", shardName, err)
		}
	}

	return subroutines.OK(), nil
}

// restoreShard deletes the existing Etcd CR, waits for deletion, then recreates it with
// the restore-traceability annotation and waits for it to become ready.
//
// Idempotency: if the CR already exists and carries the correct snapshot annotation it was
// restored in a previous (partial) reconcile — skip the delete+recreate cycle and only
// wait for ready. This prevents re-deleting a live shard on retry after another shard fails.
func (s *EtcdRestoreSubroutine) restoreShard(ctx context.Context, cl client.Client, shardName, snapshotKey string) error {
	var existing druidv1alpha1.Etcd
	if err := cl.Get(ctx, types.NamespacedName{Name: shardName, Namespace: s.namespace}, &existing); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("Etcd CR %q not found in namespace %q, cannot recreate without existing spec",
				shardName, s.namespace)
		}
		return fmt.Errorf("fetching Etcd CR: %w", err)
	}

	// Already restored in a prior reconcile — just wait for ready.
	// The snapshotKey non-empty guard in Process() ensures this comparison is only ever
	// between two non-empty strings, preventing a nil-annotations false-positive match.
	if existing.Annotations[AnnotationKeyRestoredFromSnapshot] == snapshotKey {
		return s.waitForReady(ctx, cl, shardName)
	}

	savedSpec := *existing.Spec.DeepCopy()
	// Deep-copy the labels map so the new CR always has an independent copy. A nil map
	// is safe to assign to ObjectMeta.Labels, but the caller must guarantee snapshotKey
	// is non-empty (enforced in Process) to avoid a false idempotency match above.
	savedLabels := make(map[string]string, len(existing.Labels))
	for k, v := range existing.Labels {
		savedLabels[k] = v
	}

	if err := cl.Delete(ctx, &existing); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting Etcd CR: %w", err)
	}
	if err := s.waitForDeletion(ctx, cl, shardName); err != nil {
		return fmt.Errorf("waiting for Etcd CR deletion: %w", err)
	}

	return s.createAndWait(ctx, cl, shardName, savedSpec, savedLabels, snapshotKey)
}

// createAndWait creates a new Etcd CR with the given spec and labels, then waits for it
// to become ready. If the CR already exists (race with etcd-druid recreating it), the
// restore annotation is patched onto it before waiting — ensuring traceability is never
// silently skipped.
func (s *EtcdRestoreSubroutine) createAndWait(
	ctx context.Context,
	cl client.Client,
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
		// Patch the restore annotation so traceability is preserved.
		var existing druidv1alpha1.Etcd
		if err := cl.Get(ctx, types.NamespacedName{Name: name, Namespace: s.namespace}, &existing); err != nil {
			return fmt.Errorf("fetching pre-existing Etcd CR after AlreadyExists: %w", err)
		}
		patch := client.MergeFrom(existing.DeepCopy())
		if existing.Annotations == nil {
			existing.Annotations = map[string]string{}
		}
		existing.Annotations[AnnotationKeyRestoredFromSnapshot] = snapshotKey
		if err := cl.Patch(ctx, &existing, patch); err != nil {
			return fmt.Errorf("patching restore annotation on pre-existing Etcd CR: %w", err)
		}
	}
	return s.waitForReady(ctx, cl, name)
}

// waitForDeletion polls until the Etcd CR is gone. Checks state before sleeping to
// avoid an unnecessary full-interval delay when the CR is already deleted.
func (s *EtcdRestoreSubroutine) waitForDeletion(ctx context.Context, cl client.Client, name string) error {
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

		select {
		case <-pollCtx.Done():
			return fmt.Errorf("timed out waiting for Etcd CR %q to be deleted: %w", name, pollCtx.Err())
		case <-time.After(s.deletePollInt):
		}
	}
}

// waitForReady polls until Etcd.status.ready = true. Checks state before sleeping.
func (s *EtcdRestoreSubroutine) waitForReady(ctx context.Context, cl client.Client, name string) error {
	pollCtx, cancel := context.WithTimeout(ctx, s.readyTimeout)
	defer cancel()

	for {
		var etcd druidv1alpha1.Etcd
		if err := cl.Get(pollCtx, types.NamespacedName{Name: name, Namespace: s.namespace}, &etcd); err != nil {
			return fmt.Errorf("polling Etcd CR readiness: %w", err)
		}
		if etcd.Status.Ready != nil && *etcd.Status.Ready {
			return nil
		}

		select {
		case <-pollCtx.Done():
			return fmt.Errorf("timed out waiting for Etcd CR %q to become ready: %w", name, pollCtx.Err())
		case <-time.After(s.readyPollInt):
		}
	}
}
