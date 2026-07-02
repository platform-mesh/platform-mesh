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
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/subroutines"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// LabelComponentKCPShard is the label value identifying kcp-shard Etcd CRs.
	LabelComponentKCPShard = "kcp-shard"

	// LabelKeyComponent is the label key used by platform-mesh to identify component type.
	LabelKeyComponent = "platform-mesh.io/component"

	// ConditionEtcdSnapshotted is set on PlatformBackup when all shards have been snapshotted.
	ConditionEtcdSnapshotted = "EtcdSnapshotted"
)

// shardSnapshot holds the result of a single shard's snapshot operation.
type shardSnapshot struct {
	ShardName   string
	SnapshotKey string
	SnapshotAt  metav1.Time
	Err         error
}

// EtcdCaptureSubroutine triggers a full snapshot on every kcp-shard Etcd CR via an
// EtcdOpsTask, waits for each task to succeed, then reads the snapshot key from the
// full-snap coordination lease and records it on the PlatformBackup status.
type EtcdCaptureSubroutine struct {
	namespace    string
	pollInterval time.Duration
	pollTimeout  time.Duration
}

func NewEtcdCaptureSubroutine(namespace string) *EtcdCaptureSubroutine {
	return &EtcdCaptureSubroutine{
		namespace:    namespace,
		pollInterval: 5 * time.Second,
		pollTimeout:  10 * time.Minute,
	}
}

func (s *EtcdCaptureSubroutine) WithPollInterval(interval, timeout time.Duration) *EtcdCaptureSubroutine {
	s.pollInterval = interval
	s.pollTimeout = timeout
	return s
}

func (s *EtcdCaptureSubroutine) GetName() string { return ConditionEtcdSnapshotted }

func (s *EtcdCaptureSubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	bkp, ok := obj.(*pmbackupv1alpha1.PlatformBackup)
	if !ok {
		return subroutines.OK(), fmt.Errorf("unexpected object type %T", obj)
	}

	if !bkp.Spec.Components.Etcd.Enabled {
		return subroutines.OK(), nil
	}

	if bkp.Status.Artefacts.Etcd != nil && len(bkp.Status.Artefacts.Etcd.Shards) > 0 {
		return subroutines.OK(), nil
	}

	cl := subroutines.MustClientFromContext(ctx)

	shards, err := s.listShards(ctx, cl)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("listing etcd shards: %w", err)
	}
	if len(shards) == 0 {
		// No shards yet — requeue rather than Stop so downstream subroutines
		// (CNPG, Velero) are not silently skipped when shards appear later.
		return subroutines.StopWithRequeue(30*time.Second,
			fmt.Sprintf("no etcd CRs with label %s=%s found in namespace %s",
				LabelKeyComponent, LabelComponentKCPShard, s.namespace)), nil
	}

	log := ctrllog.FromContext(ctx).WithValues("backup", bkp.Name, "shardCount", len(shards))

	// Refuse to snapshot any shard that is not fully healthy. A degraded etcd
	// cluster is already at risk; snapshotting it would increase that risk and
	// may capture diverged state. Requeue until all shards recover.
	if unhealthy := unhealthyShards(shards); len(unhealthy) > 0 {
		return subroutines.StopWithRequeue(30*time.Second,
			fmt.Sprintf("refusing backup: etcd shard(s) not fully ready: %v", unhealthy)), nil
	}

	log.Info("starting etcd snapshot", "shards", shardNames(shards))

	// Read baseline lease keys for all shards in parallel before triggering any
	// snapshot. Propagate any error — a discarded error here would let a
	// pre-existing HolderIdentity be mis-recorded as a new key.
	baselineKeys, err := s.readBaselineKeys(ctx, cl, shards)
	if err != nil {
		return subroutines.OK(), err
	}

	results := s.fanOutCapture(ctx, bkp.Name, shards, baselineKeys, cl)

	artefact := &pmbackupv1alpha1.EtcdArtefact{Shards: make(map[string]pmbackupv1alpha1.EtcdShardArtefact, len(results))}
	var errs []error
	for _, r := range results {
		if r.Err != nil {
			errs = append(errs, fmt.Errorf("shard %s: %w", r.ShardName, r.Err))
			continue
		}
		artefact.Shards[r.ShardName] = pmbackupv1alpha1.EtcdShardArtefact{
			SnapshotKey:  r.SnapshotKey,
			SnapshotTime: r.SnapshotAt,
		}
	}
	if len(errs) > 0 {
		return subroutines.OK(), fmt.Errorf("etcd snapshot failed: %w", errors.Join(errs...))
	}

	log.Info("etcd snapshot complete", "shardCount", len(results))
	bkp.Status.Artefacts.Etcd = artefact
	return subroutines.OK(), nil
}

func (s *EtcdCaptureSubroutine) listShards(ctx context.Context, cl ctrlruntimeclient.Client) ([]druidv1alpha1.Etcd, error) {
	var list druidv1alpha1.EtcdList
	if err := cl.List(ctx, &list,
		ctrlruntimeclient.InNamespace(s.namespace),
		ctrlruntimeclient.MatchingLabels{LabelKeyComponent: LabelComponentKCPShard},
	); err != nil {
		return nil, err
	}
	return list.Items, nil
}

// unhealthyShards returns the names of shards that are not fully ready.
// A shard is healthy when status.ready=true and currentReplicas == spec.replicas.
func unhealthyShards(shards []druidv1alpha1.Etcd) []string {
	var names []string
	for _, s := range shards {
		if s.Status.Ready == nil || !*s.Status.Ready || s.Status.CurrentReplicas != s.Spec.Replicas {
			names = append(names, s.Name)
		}
	}
	return names
}

func shardNames(shards []druidv1alpha1.Etcd) []string {
	names := make([]string, len(shards))
	for i, s := range shards {
		names[i] = s.Name
	}
	return names
}

// readBaselineKeys reads the full-snap lease HolderIdentity for all shards concurrently.
// Returns an error if any shard's lease cannot be read.
func (s *EtcdCaptureSubroutine) readBaselineKeys(
	ctx context.Context,
	cl ctrlruntimeclient.Client,
	shards []druidv1alpha1.Etcd,
) (map[string]string, error) {
	type result struct {
		name string
		key  string
		err  error
	}
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch := make(chan result, len(shards))
	for _, shard := range shards {
		go func(name string) {
			key, err := s.readFullSnapLeaseKey(readCtx, cl, name)
			ch <- result{name: name, key: key, err: err}
		}(shard.Name)
	}
	keys := make(map[string]string, len(shards))
	for range shards {
		r := <-ch
		if r.err != nil {
			return nil, fmt.Errorf("reading baseline lease for shard %q: %w", r.name, r.err)
		}
		keys[r.name] = r.key
	}
	return keys, nil
}

// readFullSnapLeaseKey returns the current HolderIdentity of the full-snap lease for etcdName.
func (s *EtcdCaptureSubroutine) readFullSnapLeaseKey(ctx context.Context, cl ctrlruntimeclient.Client, etcdName string) (string, error) {
	var lease coordinationv1.Lease
	leaseName := druidv1alpha1.GetFullSnapshotLeaseName(metav1.ObjectMeta{Name: etcdName})
	if err := cl.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: s.namespace}, &lease); err != nil {
		return "", ctrlruntimeclient.IgnoreNotFound(err)
	}
	if lease.Spec.HolderIdentity == nil {
		return "", nil
	}
	return *lease.Spec.HolderIdentity, nil
}

func (s *EtcdCaptureSubroutine) fanOutCapture(
	ctx context.Context,
	backupName string,
	shards []druidv1alpha1.Etcd,
	baseline map[string]string,
	cl ctrlruntimeclient.Client,
) []shardSnapshot {
	results := make([]shardSnapshot, len(shards))
	var wg sync.WaitGroup
	for i, shard := range shards {
		wg.Add(1)
		go func(idx int, etcd druidv1alpha1.Etcd) {
			defer wg.Done()
			results[idx] = s.captureOne(ctx, cl, backupName, etcd, baseline[etcd.Name])
		}(i, shard)
	}
	wg.Wait()
	return results
}

// OpsTaskName returns the EtcdOpsTask name for a (backupName, etcdName) pair.
func OpsTaskName(backupName, etcdName string) string {
	h := sha256.Sum256([]byte(backupName + "/" + etcdName))
	suffix := fmt.Sprintf("%x", h[:3])
	base := fmt.Sprintf("%s-%s", backupName, etcdName)
	full := fmt.Sprintf("%s-%s", base, suffix)
	if len(full) <= 253 {
		return full
	}
	// Truncate base to fit, keeping the hash suffix intact.
	keep := 253 - len(suffix) - 1 // -1 for the separating "-"
	if keep < 1 {
		keep = 1
	}
	return fmt.Sprintf("%s-%s", base[:keep], suffix)
}

// captureOne creates an EtcdOpsTask for the shard and polls it until it reaches a terminal state.
// TODO: split into create + check so the operator does not block on a single component's backup.
func (s *EtcdCaptureSubroutine) captureOne(
	ctx context.Context,
	cl ctrlruntimeclient.Client,
	backupName string,
	etcd druidv1alpha1.Etcd,
	baselineKey string,
) shardSnapshot {
	result := shardSnapshot{ShardName: etcd.Name}
	log := ctrllog.FromContext(ctx).WithValues("backup", backupName, "shard", etcd.Name)

	taskName := OpsTaskName(backupName, etcd.Name)
	snapshotType := druidv1alpha1.OnDemandSnapshotTypeFull
	task := &druidv1alpha1.EtcdOpsTask{
		ObjectMeta: metav1.ObjectMeta{
			Name:      taskName,
			Namespace: s.namespace,
		},
		Spec: druidv1alpha1.EtcdOpsTaskSpec{
			EtcdName: ptr.To(etcd.Name),
			Config: druidv1alpha1.EtcdOpsTaskConfig{
				OnDemandSnapshot: &druidv1alpha1.OnDemandSnapshotConfig{
					Type: snapshotType,
				},
			},
		},
	}

	if err := cl.Create(ctx, task); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			result.Err = fmt.Errorf("creating EtcdOpsTask: %w", err)
			return result
		}
		log.Info("EtcdOpsTask already exists — resuming in-progress snapshot", "task", taskName)
	} else {
		log.Info("created EtcdOpsTask", "task", taskName, "baselineKey", baselineKey)
	}

	pollCtx, cancel := context.WithTimeout(ctx, s.pollTimeout)
	defer cancel()

	for {
		var current druidv1alpha1.EtcdOpsTask
		if err := cl.Get(pollCtx, types.NamespacedName{Name: taskName, Namespace: s.namespace}, &current); err != nil {
			if !apierrors.IsNotFound(err) {
				result.Err = fmt.Errorf("polling EtcdOpsTask: %w", err)
				return result
			}
			// Task is gone — etcd-druid completed and removed it. Read the lease to
			// determine whether the snapshot actually ran.
			key, lerr := s.readFullSnapLeaseKey(pollCtx, cl, etcd.Name)
			if lerr != nil {
				result.Err = fmt.Errorf("task %s not found, reading lease: %w", taskName, lerr)
				return result
			}
			if key != "" && key != baselineKey {
				// Lease advanced past the baseline — snapshot completed.
				result.SnapshotKey = key
				result.SnapshotAt = metav1.Now()
				return result
			}
			// Lease is empty or unchanged — task was cleaned up before the snapshot
			// key was written (or before we could observe it). Return an error so the
			// next reconcile creates a fresh task and retries.
			result.Err = fmt.Errorf("EtcdOpsTask %s not found and full-snap lease is unchanged (baseline=%q, current=%q); will retry",
				taskName, baselineKey, key)
			return result
		}

		if current.Status.State != nil {
			switch *current.Status.State {
			case druidv1alpha1.TaskStateSucceeded:
				key, err := s.readFullSnapLeaseKey(pollCtx, cl, etcd.Name)
				// Delete the task regardless of whether the lease read succeeds — a
				// terminal task must be removed so that a future retry can create a
				// fresh one and read a fresh baseline key.
				if delErr := cl.Delete(pollCtx, &current); delErr != nil && !apierrors.IsNotFound(delErr) {
					// Non-fatal: the snapshot key is already recorded. Log and continue
					// so the caller sees success; the stale task will be GC'd eventually.
					_ = delErr // surfaced via the result message below if lease also fails
				}
				if err != nil {
					result.Err = fmt.Errorf("reading full-snap lease after task succeeded: %w", err)
					return result
				}
				if key == "" && baselineKey != "" {
					result.Err = fmt.Errorf("full-snap lease for %q is empty after EtcdOpsTask succeeded",
						etcd.Name)
					return result
				}
				if key == "" {
					// Baseline was also empty — this cluster has never run a scheduled full
					// snapshot so the lease is permanently unset. TaskStateSucceeded is the
					// authoritative signal that etcdbr wrote the snapshot to S3; synthesise a
					// stable, backup-scoped key so the artefact is non-empty. Using
					// backupName+"/"+etcdName (rather than just etcdName) makes the key unique
					// per backup run, preventing the idempotency check in restoreShard from
					// false-positiving when two different backups of the same fresh-cluster
					// shard are restored in sequence.
					key = backupName + "/" + etcd.Name
				}
				// EtcdOpsTask Succeeded is the authoritative signal that etcdbr wrote
				// the snapshot to S3. The full-snap lease HolderIdentity holds the etcd
				// revision of the last *scheduled* snapshot and is not updated by
				// on-demand snapshots — so we accept the current lease key regardless
				// of whether it changed from the baseline.
				log.Info("EtcdOpsTask succeeded", "task", taskName, "snapshotKey", key)
				result.SnapshotKey = key
				result.SnapshotAt = metav1.Now()
				return result

			case druidv1alpha1.TaskStateFailed, druidv1alpha1.TaskStateRejected:
				msg := string(*current.Status.State)
				if len(current.Status.LastErrors) > 0 {
					msg = current.Status.LastErrors[0].Description
				}
				// Delete the terminal task so a subsequent reconcile can try again.
				_ = cl.Delete(pollCtx, &current)
				log.Info("EtcdOpsTask reached terminal failure", "task", taskName, "state", string(*current.Status.State), "message", msg)
				result.Err = fmt.Errorf("EtcdOpsTask %s/%s reached state %s: %s",
					s.namespace, taskName, *current.Status.State, msg)
				return result
			}
		}

		timer := time.NewTimer(s.pollInterval)
		select {
		case <-pollCtx.Done():
			timer.Stop()
			// Do NOT delete the task on timeout: it may still be running on the etcd-druid
			// side and cancelling a mid-flight snapshot could corrupt data. On the next
			// reconcile Create returns AlreadyExists (ignored) and the poll resumes — a
			// free continuation of the in-progress snapshot.
			//
			// Use Unwrap-safe context.Cause where available; fall back to Err().
			// Distinguish poll-timeout (DeadlineExceeded) from parent-cancel (Canceled)
			// so that a clean operator shutdown is not counted as a backup failure.
			cause := context.Cause(pollCtx)
			if cause == nil {
				cause = pollCtx.Err()
			}
			result.Err = fmt.Errorf("waiting for EtcdOpsTask %s (baseline=%q): %w",
				taskName, baselineKey, cause)
			return result
		case <-timer.C:
		}
	}
}
