//go:build e2e

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

package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// startSimulators launches goroutines that stand in for etcd-druid and the etcd
// StatefulSet controller. They run for the lifetime of ctx and are safe to call
// from multiple tests concurrently because every test uses unique resource names.
//
// taskSimulator: watches for EtcdOpsTask objects not yet in a terminal state,
// writes a new HolderIdentity to the full-snap lease, then marks the task Succeeded.
//
// readySimulator: watches for Etcd CRs whose status.ready is not true and sets it.
// This mirrors what the etcd StatefulSet controller does once etcd pods are Running.
func startSimulators(ctx context.Context, t *testing.T) {
	t.Helper()
	startTaskSimulator(ctx, t)
	startReadySimulator(ctx, t)
}

// startTaskSimulator watches for EtcdOpsTask CRs in e2eNS and immediately
// transitions any non-terminal task to Succeeded, racing ahead of etcd-druid's
// admit check. A Watch (not a poll) is used so the reaction is in milliseconds
// rather than up to 50ms, giving the simulator a much better chance of winning
// before etcd-druid's controller sets the state to Rejected.
func startTaskSimulator(ctx context.Context, t *testing.T) {
	t.Helper()
	counter := 0
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}

			// Open a watch on EtcdOpsTask in e2eNS.
			watcher, err := cl.Watch(ctx, &druidv1alpha1.EtcdOpsTaskList{},
				client.InNamespace(e2eNS))
			if err != nil {
				// Watch failed (e.g. context cancelled) — fall back to a short sleep.
				select {
				case <-ctx.Done():
					return
				case <-time.After(200 * time.Millisecond):
				}
				continue
			}

			for {
				select {
				case <-ctx.Done():
					watcher.Stop()
					return
				case event, ok := <-watcher.ResultChan():
					if !ok {
						// Channel closed — restart the watch.
						goto restart
					}
					if event.Type != watch.Added && event.Type != watch.Modified {
						continue
					}
					task, ok := event.Object.(*druidv1alpha1.EtcdOpsTask)
					if !ok {
						continue
					}
					if isTerminal(task.Status.State) {
						continue
					}
					if !strings.HasPrefix(task.Name, "e2e-") {
						continue
					}

					etcdName := ""
					if task.Spec.EtcdName != nil {
						etcdName = *task.Spec.EtcdName
					}

					// Write a unique snapshot key to the full-snap lease.
					counter++
					key := fmt.Sprintf("sim-snap-%s-%d", etcdName, counter)
					if err := upsertFullSnapLease(ctx, etcdName, key); err != nil {
						t.Logf("simulator: upsertFullSnapLease(%s): %v", etcdName, err)
						continue
					}

					// Transition to Succeeded.
					succeeded := druidv1alpha1.TaskStateSucceeded
					patch := client.MergeFrom(task.DeepCopy())
					task.Status.State = &succeeded
					if err := cl.Status().Patch(ctx, task, patch); err != nil {
						t.Logf("simulator: patch task %s Succeeded: %v", task.Name, err)
					}
				}
			}
		restart:
			watcher.Stop()
		}
	}()
}

// startReadySimulator sets status.ready=true on Etcd CRs that are not yet ready.
// Each CR is only patched once per UID — repeated patches would trigger etcd-druid
// status reconciles which overwrite ready=false, creating an event storm.
// The operator retries until it wins a window where etcd-druid has not yet overwritten.
func startReadySimulator(ctx context.Context, t *testing.T) {
	t.Helper()
	patched := map[types.UID]bool{}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}

			var list druidv1alpha1.EtcdList
			if err := cl.List(ctx, &list, client.InNamespace(e2eNS)); err != nil {
				continue
			}
			for i := range list.Items {
				etcd := &list.Items[i]
				if patched[etcd.UID] {
					continue
				}
				if etcd.Status.Ready != nil && *etcd.Status.Ready {
					patched[etcd.UID] = true
					continue
				}
				patch := client.MergeFrom(etcd.DeepCopy())
				etcd.Status.Ready = ptr.To(true)
				if err := cl.Status().Patch(ctx, etcd, patch); err != nil {
					t.Logf("readySimulator: patch %s: %v", etcd.Name, err)
				} else {
					patched[etcd.UID] = true
				}
			}
		}
	}()
}

// upsertFullSnapLease creates or updates the full-snap coordination lease for etcdName.
func upsertFullSnapLease(ctx context.Context, etcdName, key string) error {
	leaseName := druidv1alpha1.GetFullSnapshotLeaseName(metav1.ObjectMeta{Name: etcdName})
	nn := types.NamespacedName{Name: leaseName, Namespace: e2eNS}

	var existing coordinationv1.Lease
	if err := cl.Get(ctx, nn, &existing); err != nil {
		// Create fresh.
		lease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{Name: leaseName, Namespace: e2eNS},
			Spec:       coordinationv1.LeaseSpec{HolderIdentity: ptr.To(key)},
		}
		return client.IgnoreAlreadyExists(cl.Create(ctx, lease))
	}
	patch := client.MergeFrom(existing.DeepCopy())
	existing.Spec.HolderIdentity = ptr.To(key)
	return cl.Patch(ctx, &existing, patch)
}

// ensureEtcdReady sets status.ready=true on the named Etcd CR if it isn't already.
func ensureEtcdReady(ctx context.Context, etcdName string) error {
	var etcd druidv1alpha1.Etcd
	if err := cl.Get(ctx, types.NamespacedName{Name: etcdName, Namespace: e2eNS}, &etcd); err != nil {
		return err
	}
	if etcd.Status.Ready != nil && *etcd.Status.Ready {
		return nil
	}
	patch := client.MergeFrom(etcd.DeepCopy())
	etcd.Status.Ready = ptr.To(true)
	return cl.Status().Patch(ctx, &etcd, patch)
}

func isTerminal(state *druidv1alpha1.TaskState) bool {
	if state == nil {
		return false
	}
	switch *state {
	case druidv1alpha1.TaskStateSucceeded,
		druidv1alpha1.TaskStateFailed,
		druidv1alpha1.TaskStateRejected:
		return true
	}
	return false
}
