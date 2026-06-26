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
	"errors"
	"fmt"
	"sync"
	"time"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	"go.platform-mesh.io/subroutines"
	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	backupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
)

const (
	// LabelComponentKCPShard is the label value identifying KCP-shard Etcd CRs.
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

// EtcdCaptureSubroutine triggers a full snapshot on every KCP-shard Etcd CR via an
// EtcdOpsTask, waits for each task to succeed, then reads the snapshot key from the
// full-snap coordination lease and records it on the PlatformBackup status.
type EtcdCaptureSubroutine struct {
	namespace    string
	pollInterval time.Duration
	pollTimeout  time.Duration
}

// NewEtcdCaptureSubroutine returns an EtcdCaptureSubroutine for the given namespace.
func NewEtcdCaptureSubroutine(namespace string) *EtcdCaptureSubroutine {
	return &EtcdCaptureSubroutine{
		namespace:    namespace,
		pollInterval: 5 * time.Second,
		pollTimeout:  10 * time.Minute,
	}
}

// WithPollInterval overrides poll interval and timeout (for testing).
func (s *EtcdCaptureSubroutine) WithPollInterval(interval, timeout time.Duration) *EtcdCaptureSubroutine {
	s.pollInterval = interval
	s.pollTimeout = timeout
	return s
}

func (s *EtcdCaptureSubroutine) GetName() string { return ConditionEtcdSnapshotted }

// Process implements subroutines.Subroutine.
func (s *EtcdCaptureSubroutine) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	bkp, ok := obj.(*backupv1alpha1.PlatformBackup)
	if !ok {
		return subroutines.OK(), fmt.Errorf("unexpected object type %T", obj)
	}

	if !bkp.Spec.Components.Etcd.Enabled {
		return subroutines.OK(), nil
	}

	// Idempotency: skip if already captured.
	if bkp.Status.Artefacts.Etcd != nil && len(bkp.Status.Artefacts.Etcd.Shards) > 0 {
		return subroutines.OK(), nil
	}

	cl := subroutines.MustClientFromContext(ctx)

	shards, err := s.listShards(ctx, cl)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("listing etcd shards: %w", err)
	}
	if len(shards) == 0 {
		return subroutines.Stop(fmt.Sprintf("no etcd CRs with label %s=%s found in namespace %s",
			LabelKeyComponent, LabelComponentKCPShard, s.namespace)), nil
	}

	// Read baseline lease keys before triggering. Propagate any error — a discarded
	// error here would let a pre-existing HolderIdentity be mis-recorded as a new key.
	baselineKeys := make(map[string]string, len(shards))
	for _, shard := range shards {
		key, err := s.readFullSnapLeaseKey(ctx, cl, shard.Name)
		if err != nil {
			return subroutines.OK(), fmt.Errorf("reading baseline lease for shard %q: %w", shard.Name, err)
		}
		baselineKeys[shard.Name] = key
	}

	results := s.fanOutCapture(ctx, bkp.Name, shards, baselineKeys, cl)

	artefact := &backupv1alpha1.EtcdArtefact{Shards: make(map[string]backupv1alpha1.EtcdShardArtefact, len(results))}
	var errs []error
	for _, r := range results {
		if r.Err != nil {
			errs = append(errs, fmt.Errorf("shard %s: %w", r.ShardName, r.Err))
			continue
		}
		artefact.Shards[r.ShardName] = backupv1alpha1.EtcdShardArtefact{
			SnapshotKey:  r.SnapshotKey,
			SnapshotTime: r.SnapshotAt,
		}
	}
	if len(errs) > 0 {
		return subroutines.OK(), fmt.Errorf("etcd snapshot failed: %w", errors.Join(errs...))
	}

	bkp.Status.Artefacts.Etcd = artefact
	return subroutines.OK(), nil
}

// listShards returns all Etcd CRs in the namespace carrying the kcp-shard component label.
func (s *EtcdCaptureSubroutine) listShards(ctx context.Context, cl client.Client) ([]druidv1alpha1.Etcd, error) {
	var list druidv1alpha1.EtcdList
	if err := cl.List(ctx, &list,
		client.InNamespace(s.namespace),
		client.MatchingLabels{LabelKeyComponent: LabelComponentKCPShard},
	); err != nil {
		return nil, err
	}
	return list.Items, nil
}

// readFullSnapLeaseKey returns the current HolderIdentity of the full-snap lease for etcdName.
func (s *EtcdCaptureSubroutine) readFullSnapLeaseKey(ctx context.Context, cl client.Client, etcdName string) (string, error) {
	var lease coordinationv1.Lease
	leaseName := druidv1alpha1.GetFullSnapshotLeaseName(metav1.ObjectMeta{Name: etcdName})
	if err := cl.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: s.namespace}, &lease); err != nil {
		return "", client.IgnoreNotFound(err)
	}
	if lease.Spec.HolderIdentity == nil {
		return "", nil
	}
	return *lease.Spec.HolderIdentity, nil
}

// fanOutCapture triggers snapshot tasks for all shards concurrently and collects results.
func (s *EtcdCaptureSubroutine) fanOutCapture(
	ctx context.Context,
	backupName string,
	shards []druidv1alpha1.Etcd,
	baseline map[string]string,
	cl client.Client,
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

// captureOne creates an EtcdOpsTask to trigger a full snapshot, polls until it reaches a
// terminal state, then reads the updated snapshot key from the full-snap lease.
// State is checked immediately on entry; sleep only follows a non-terminal observation.
func (s *EtcdCaptureSubroutine) captureOne(
	ctx context.Context,
	cl client.Client,
	backupName string,
	etcd druidv1alpha1.Etcd,
	baselineKey string,
) shardSnapshot {
	result := shardSnapshot{ShardName: etcd.Name}

	taskName := fmt.Sprintf("%s-%s", backupName, etcd.Name)
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

	if err := cl.Create(ctx, task); err != nil && !apierrors.IsAlreadyExists(err) {
		result.Err = fmt.Errorf("creating EtcdOpsTask: %w", err)
		return result
	}

	pollCtx, cancel := context.WithTimeout(ctx, s.pollTimeout)
	defer cancel()

	for {
		// Check state before sleeping so we don't always incur one full interval delay.
		var current druidv1alpha1.EtcdOpsTask
		if err := cl.Get(pollCtx, types.NamespacedName{Name: taskName, Namespace: s.namespace}, &current); err != nil {
			if !apierrors.IsNotFound(err) {
				result.Err = fmt.Errorf("polling EtcdOpsTask: %w", err)
				return result
			}
			// Task was deleted (by a prior reconcile that already handled it, or by
			// the operator itself after a terminal state). Check whether the lease was
			// already updated — if so the snapshot completed and we can claim success.
			key, lerr := s.readFullSnapLeaseKey(pollCtx, cl, etcd.Name)
			if lerr != nil {
				result.Err = fmt.Errorf("task %s not found, reading lease: %w", taskName, lerr)
				return result
			}
			if key != "" && key != baselineKey {
				result.SnapshotKey = key
				result.SnapshotAt = metav1.Now()
				return result
			}
			// Lease not yet updated — task was cleaned up before we saw it succeed.
			// Return an error so the reconcile retries and creates a fresh task.
			result.Err = fmt.Errorf("EtcdOpsTask %s not found and lease not yet updated (baseline=%q, current=%q)",
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
				_ = cl.Delete(pollCtx, &current)
				if err != nil {
					result.Err = fmt.Errorf("reading full-snap lease after task succeeded: %w", err)
					return result
				}
				if key == "" {
					result.Err = fmt.Errorf("full-snap lease for %q is empty after EtcdOpsTask succeeded",
						etcd.Name)
					return result
				}
				if key == baselineKey {
					result.Err = fmt.Errorf("full-snap lease for %q not updated after EtcdOpsTask succeeded (baseline=%q, current=%q)",
						etcd.Name, baselineKey, key)
					return result
				}
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
				result.Err = fmt.Errorf("EtcdOpsTask %s/%s reached state %s: %s",
					s.namespace, taskName, *current.Status.State, msg)
				return result
			}
		}

		select {
		case <-pollCtx.Done():
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
		case <-time.After(s.pollInterval):
		}
	}
}
