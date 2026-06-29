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
	"sort"
	"time"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"
	"go.platform-mesh.io/golang-commons/logger"
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

	// LabelKeyComponent and LabelComponentKCPShard are re-exported from the backup package
	// so e2e tests and other restore callers can reference them without importing backup.
	LabelKeyComponent      = backup.LabelKeyComponent
	LabelComponentKCPShard = backup.LabelComponentKCPShard
)

// EtcdRestoreSubroutine deletes and recreates each kcp-shard Etcd CR from the snapshot
// keys recorded in the source PlatformBackup, then waits for each Etcd CR to become ready.
//
// The subroutine is non-blocking: each reconcile does one unit of work per shard and
// returns Pending if any shard is still in progress. No goroutine is held between
// reconciles; the Etcd CR's own state (DeletionTimestamp, annotations, status.ready)
// drives the state machine.
type EtcdRestoreSubroutine struct {
	namespace string
}

func NewEtcdRestoreSubroutine(namespace string) *EtcdRestoreSubroutine {
	return &EtcdRestoreSubroutine{namespace: namespace}
}

func (s *EtcdRestoreSubroutine) GetName() string { return ConditionEtcdRestored }

// Process implements subroutines.Subroutine. It drives the restore state machine for
// all shards in a single non-blocking pass:
//
//  1. Shard has the correct restore annotation AND is ready → done
//  2. Shard has the annotation but is not ready yet → Pending(10s)
//  3. Shard is terminating (DeletionTimestamp set) → strip finalizers, Pending(5s)
//  4. Shard exists without annotation → save spec, strip finalizers, delete, recreate
//     in the same pass (Create may get AlreadyExists if deletion is still in flight —
//     handled by patching annotation+label onto the terminating CR) → Pending(5s)
//  5. Shard is absent → recreate with annotation → Pending(5s)
//
// When all shards are in state 1, returns OK() and EtcdRestored=True is committed.
//
// Case ordering: DeletionTimestamp is checked before the annotation to ensure
// finalizer-stripping always runs on terminating CRs, even those that already
// carry the restore annotation (e.g. from the AlreadyExists path).
func (s *EtcdRestoreSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	rst, ok := obj.(*pmbackupv1alpha1.PlatformRestore)
	if !ok {
		return subroutines.OK(), fmt.Errorf("unexpected object type %T", obj)
	}

	if apimeta.IsStatusConditionTrue(rst.Status.Conditions, ConditionEtcdRestored) {
		return subroutines.OK(), nil
	}

	cl := subroutines.MustClientFromContext(ctx)
	log := logger.LoadLoggerFromContext(ctx)

	// PlatformBackup is cluster-scoped, so no namespace in the key.
	var bkp pmbackupv1alpha1.PlatformBackup
	if err := cl.Get(ctx, types.NamespacedName{Name: rst.Spec.Source.BackupID}, &bkp); err != nil {
		if apierrors.IsNotFound(err) {
			return subroutines.StopWithRequeue(30*time.Second,
				fmt.Sprintf("source backup %q not found, requeueing", rst.Spec.Source.BackupID)), nil
		}
		return subroutines.OK(), fmt.Errorf("fetching source backup %q: %w", rst.Spec.Source.BackupID, err)
	}

	if bkp.Status.Artefacts.Etcd == nil || len(bkp.Status.Artefacts.Etcd.Shards) == 0 {
		return subroutines.OK(), nil
	}

	shards := bkp.Status.Artefacts.Etcd.Shards

	// Validate all snapshot keys up-front before touching any CR.
	for shardName, artefact := range shards {
		if artefact.SnapshotKey == "" {
			return subroutines.OK(), fmt.Errorf("shard %q has empty snapshot key; backup artefact may be corrupt", shardName)
		}
	}

	log.Info().Str("restore", rst.Name).Str("backup", rst.Spec.Source.BackupID).
		Int("shardCount", len(shards)).Msg("starting etcd restore")

	// Process shards in deterministic order so log output and error messages are stable.
	shardNames := make([]string, 0, len(shards))
	for name := range shards {
		shardNames = append(shardNames, name)
	}
	sort.Strings(shardNames)

	pendingMsg := ""
	var errs []error

	for _, shardName := range shardNames {
		snapshotKey := shards[shardName].SnapshotKey

		var etcd druidv1alpha1.Etcd
		err := cl.Get(ctx, types.NamespacedName{Name: shardName, Namespace: s.namespace}, &etcd)

		switch {
		case err != nil && !apierrors.IsNotFound(err):
			errs = append(errs, fmt.Errorf("fetching Etcd CR %q: %w", shardName, err))

		case apierrors.IsNotFound(err):
			// CR is absent — the delete succeeded but recreate has not run yet, or
			// the CR was never present. We cannot recreate without the original spec.
			// Surface as an error requiring manual intervention (e.g. re-create the
			// PlatformRestore so a fresh reconcile can read the spec from the live CR).
			errs = append(errs, fmt.Errorf("etcd CR %q not found in namespace %q — "+
				"unable to recreate without spec; delete and re-create the PlatformRestore to retry",
				shardName, s.namespace))

		case etcd.DeletionTimestamp != nil:
			// Deletion in flight — strip any re-added finalizers. This case is evaluated
			// BEFORE the annotation case so that a terminating CR carrying the restore
			// annotation still has its finalizers stripped (the AlreadyExists path may
			// have patched the annotation onto the old terminating CR).
			s.stripFinalizers(ctx, cl, &etcd, log)
			if pendingMsg == "" {
				pendingMsg = fmt.Sprintf("waiting for Etcd %q deletion to complete", shardName)
			}

		case etcd.Annotations != nil && etcd.Annotations[AnnotationKeyRestoredFromSnapshot] == snapshotKey:
			// Annotation matches — shard was restored. Check readiness.
			if etcd.Status.Ready != nil && *etcd.Status.Ready {
				// Shard fully done — continue to next.
				continue
			}
			if pendingMsg == "" {
				pendingMsg = fmt.Sprintf("waiting for Etcd %q to become ready", shardName)
			}

		default:
			// Shard exists but not yet restored. Save spec+labels, delete, recreate
			// all in one pass so we never lose the spec between reconciles.
			savedSpec := *etcd.Spec.DeepCopy()
			savedLabels := make(map[string]string, len(etcd.Labels)+1)
			for k, v := range etcd.Labels {
				savedLabels[k] = v
			}
			savedLabels[backup.LabelKeyComponent] = backup.LabelComponentKCPShard

			log.Info().Str("shard", shardName).Str("snapshotKey", snapshotKey).
				Msg("deleting Etcd CR for restore")

			s.stripFinalizers(ctx, cl, &etcd, log)
			gracePeriod := int64(0)
			if delErr := cl.Delete(ctx, &etcd, &ctrlruntimeclient.DeleteOptions{GracePeriodSeconds: &gracePeriod}); delErr != nil && !apierrors.IsNotFound(delErr) {
				errs = append(errs, fmt.Errorf("deleting Etcd CR %q: %w", shardName, delErr))
				break
			}

			// Immediately attempt to recreate. The old CR may still be terminating,
			// in which case Create returns AlreadyExists — we patch the annotation
			// onto it so the next reconcile sees it as "already restored, check ready".
			if createErr := s.recreate(ctx, cl, shardName, snapshotKey, savedSpec, savedLabels); createErr != nil {
				errs = append(errs, fmt.Errorf("recreating Etcd CR %q: %w", shardName, createErr))
				break
			}
			if pendingMsg == "" {
				pendingMsg = fmt.Sprintf("restoring shard %q", shardName)
			}
		}
	}

	if len(errs) > 0 {
		return subroutines.OK(), combineErrors(errs)
	}
	if pendingMsg != "" {
		return subroutines.Pending(5*time.Second, pendingMsg), nil
	}

	log.Info().Str("restore", rst.Name).Int("shardCount", len(shards)).Msg("etcd restore complete")
	return subroutines.OK(), nil
}

// stripFinalizers removes all finalizers from an Etcd CR. Errors are logged at warn level
// since etcd-druid may re-add them and the caller will retry on the next reconcile.
func (s *EtcdRestoreSubroutine) stripFinalizers(ctx context.Context, cl ctrlruntimeclient.Client, etcd *druidv1alpha1.Etcd, log *logger.Logger) {
	if len(etcd.Finalizers) == 0 {
		return
	}
	patch := ctrlruntimeclient.MergeFrom(etcd.DeepCopy())
	etcd.Finalizers = nil
	if err := cl.Patch(ctx, etcd, patch); err != nil && !apierrors.IsNotFound(err) {
		log.Warn().Str("shard", etcd.Name).Err(err).Msg("failed to strip finalizers from Etcd CR")
	}
}

// recreate creates a new Etcd CR with the restore annotation. If the CR already exists
// (etcd-druid raced to recreate it between delete and Create), the annotation and
// kcp-shard label are patched onto the existing CR instead.
func (s *EtcdRestoreSubroutine) recreate(
	ctx context.Context,
	cl ctrlruntimeclient.Client,
	name, snapshotKey string,
	spec druidv1alpha1.EtcdSpec,
	labels map[string]string,
) error {
	annotations := map[string]string{AnnotationKeyRestoredFromSnapshot: snapshotKey}
	newCR := &druidv1alpha1.Etcd{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   s.namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: spec,
	}

	if err := cl.Create(ctx, newCR); err == nil {
		return nil
	} else if !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating Etcd CR: %w", err)
	}

	// AlreadyExists: etcd-druid recreated the CR after we deleted it. Patch our
	// annotation + kcp-shard label onto it — but only if the CR is not itself
	// terminating (which would mean the old CR is still being GC'd, not a fresh
	// one). Patching onto a terminating CR is useless: it will be deleted, and the
	// next reconcile will hit the NotFound error branch.
	var existing druidv1alpha1.Etcd
	if err := cl.Get(ctx, types.NamespacedName{Name: name, Namespace: s.namespace}, &existing); err != nil {
		return fmt.Errorf("fetching raced Etcd CR: %w", err)
	}
	if existing.DeletionTimestamp != nil {
		// Old CR is still terminating — nothing useful to patch. The next reconcile
		// will enter the DeletionTimestamp case, strip any finalizers, and wait.
		return nil
	}
	patch := ctrlruntimeclient.MergeFrom(existing.DeepCopy())
	if existing.Annotations == nil {
		existing.Annotations = make(map[string]string)
	}
	existing.Annotations[AnnotationKeyRestoredFromSnapshot] = snapshotKey
	if existing.Labels == nil {
		existing.Labels = make(map[string]string)
	}
	existing.Labels[backup.LabelKeyComponent] = backup.LabelComponentKCPShard
	if err := cl.Patch(ctx, &existing, patch); err != nil {
		return fmt.Errorf("patching annotation on raced Etcd CR: %w", err)
	}
	return nil
}

// combineErrors joins multiple errors into a single error, capping the message length.
func combineErrors(errs []error) error {
	if len(errs) == 1 {
		return errs[0]
	}
	const maxMessageBytes = 4096
	joined := errors.Join(errs...).Error()
	if len(joined) <= maxMessageBytes {
		return errors.Join(errs...)
	}
	suffix := fmt.Sprintf(" (+%d more shard errors truncated)", len(errs)-1)
	budget := maxMessageBytes - len(suffix)
	if budget < 1 {
		budget = 1
	}
	first := errs[0].Error()
	if len(first) > budget {
		first = first[:budget]
	}
	return fmt.Errorf("%s%s", first, suffix)
}
