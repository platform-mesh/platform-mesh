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
	"strings"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/druid"
	"go.platform-mesh.io/backup-operator/pkg/storage"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// EtcdSnapshot asks druid for a full snapshot of every etcd and finds it in the druid store.
// All tasks are created in the first run, then each run checks them and never blocks.
// druid does not report the name of the snapshot, so the task keeps the latest full snapshot
// from before it, and the snapshot of the task is the latest one after it.
type EtcdSnapshot struct {
	cl     ctrlruntimeclient.Client
	stores storage.Factory
}

func (e *EtcdSnapshot) GetName() string { return "EtcdSnapshot" }

const previousSnapshotAnnotation = "backup.platform-mesh.io/previous-snapshot"

// TaskName is the EtcdOpsTask of one etcd in a backup. A long etcd name keeps its end, the etcds of
// one namespace differ mostly there, like etcd-kcp-root and etcd-kcp-triton.
func TaskName(backup, etcd string) string {
	etcd = strings.TrimLeft(etcd[max(0, len(etcd)-30):], "-")
	backup = strings.TrimSuffix(backup[:min(len(backup), 62-len(etcd))], "-")
	return backup + "-" + etcd
}

func (e *EtcdSnapshot) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	b := obj.(*pmbackupv1alpha1.Backup)

	waiting := 0
	for i := range b.Status.Etcds {
		snap := &b.Status.Etcds[i]
		if snap.Snapshot != "" {
			continue
		}
		task, err := e.ensureTask(ctx, b, snap)
		if err != nil {
			return subroutines.OK(), err
		}
		if task.Status.State == nil || !task.IsCompleted() {
			waiting++
			continue
		}
		if *task.Status.State != druidv1alpha1.TaskStateSucceeded {
			return fail(b, fmt.Sprintf("snapshot of etcd %s: task %s is %s: %s", snap.Name, task.Name, *task.Status.State, taskError(task)))
		}
		if err := e.findSnapshot(ctx, b, snap, task); err != nil {
			return subroutines.OK(), err
		}
	}
	if waiting > 0 {
		return subroutines.StopWithRequeue(waitInterval, fmt.Sprintf("waiting for %d etcd snapshots", waiting)), nil
	}
	return subroutines.OK(), nil
}

func (e *EtcdSnapshot) ensureTask(ctx context.Context, b *pmbackupv1alpha1.Backup, snap *pmbackupv1alpha1.EtcdSnapshot) (*druidv1alpha1.EtcdOpsTask, error) {
	task := &druidv1alpha1.EtcdOpsTask{}
	err := e.cl.Get(ctx, types.NamespacedName{Namespace: b.Namespace, Name: snap.Task}, task)
	if err == nil {
		return task, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, err
	}
	previous, _, err := e.latestFull(ctx, b, snap)
	if err != nil {
		return nil, err
	}
	task = &druidv1alpha1.EtcdOpsTask{
		ObjectMeta: metav1.ObjectMeta{
			Name:        snap.Task,
			Namespace:   b.Namespace,
			Annotations: map[string]string{previousSnapshotAnnotation: previous.Object.Key},
		},
		Spec: druidv1alpha1.EtcdOpsTaskSpec{
			EtcdName:                ptr.To(snap.Name),
			TTLSecondsAfterFinished: ptr.To[int32](3600),
			Config: druidv1alpha1.EtcdOpsTaskConfig{
				OnDemandSnapshot: &druidv1alpha1.OnDemandSnapshotConfig{Type: druidv1alpha1.OnDemandSnapshotTypeFull},
			},
		},
	}
	// a Backup of the CLI is not stored in the cluster, the task ttl cleans up then
	if b.UID != "" {
		if err := controllerutil.SetControllerReference(b, task, e.cl.Scheme()); err != nil {
			return nil, err
		}
	}
	if err := e.cl.Create(ctx, task); err != nil {
		return nil, fmt.Errorf("creating etcdopstask %s: %w", snap.Task, err)
	}
	return task, nil
}

func (e *EtcdSnapshot) findSnapshot(ctx context.Context, b *pmbackupv1alpha1.Backup, snap *pmbackupv1alpha1.EtcdSnapshot, task *druidv1alpha1.EtcdOpsTask) error {
	full, ok, err := e.latestFull(ctx, b, snap)
	if err != nil {
		return err
	}
	if !ok || full.Object.Key == task.Annotations[previousSnapshotAnnotation] {
		return fmt.Errorf("no full snapshot of etcd %s found after task %s", snap.Name, task.Name)
	}
	snap.Snapshot, snap.Size = full.Object.Key, full.Object.Size
	return nil
}

func (e *EtcdSnapshot) latestFull(ctx context.Context, b *pmbackupv1alpha1.Backup, snap *pmbackupv1alpha1.EtcdSnapshot) (druid.Snapshot, bool, error) {
	etcd := &druidv1alpha1.Etcd{}
	if err := e.cl.Get(ctx, types.NamespacedName{Namespace: b.Namespace, Name: snap.Name}, etcd); err != nil {
		return druid.Snapshot{}, false, err
	}
	src, prefix, err := e.stores.ForEtcd(ctx, e.cl, etcd)
	if err != nil {
		return druid.Snapshot{}, false, err
	}
	objects, err := src.List(ctx, druid.SnapshotDir(prefix))
	if err != nil {
		return druid.Snapshot{}, false, fmt.Errorf("listing snapshots of %s: %w", snap.Name, err)
	}
	full, ok := druid.LatestFull(objects)
	return full, ok, nil
}

func taskError(task *druidv1alpha1.EtcdOpsTask) string {
	if n := len(task.Status.LastErrors); n > 0 {
		return task.Status.LastErrors[n-1].Description
	}
	if task.Status.LastOperation != nil {
		return task.Status.LastOperation.Description
	}
	return "unknown error"
}
