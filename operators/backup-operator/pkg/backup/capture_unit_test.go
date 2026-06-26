//go:build !integration

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

package backup_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	druidapicommon "github.com/gardener/etcd-druid/api/common"
	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	backupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"
	"go.platform-mesh.io/subroutines"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const unitTestNamespace = "default"

func newFakeScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, backupv1alpha1.AddToScheme(s))
	require.NoError(t, druidv1alpha1.AddToScheme(s))
	require.NoError(t, coordinationv1.AddToScheme(s))
	return s
}

func newCaptureSub() *backup.EtcdCaptureSubroutine {
	return backup.NewEtcdCaptureSubroutine(unitTestNamespace).
		WithPollInterval(1*time.Millisecond, 5*time.Second)
}

func ctxWithClient(cl client.Client) context.Context {
	return subroutines.WithClient(context.Background(), cl)
}

// fakeEtcdShard builds a minimal Etcd CR with the kcp-shard label.
func fakeEtcdShard(name string) *druidv1alpha1.Etcd {
	localProvider := druidv1alpha1.StorageProvider("Local")
	return &druidv1alpha1.Etcd{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: unitTestNamespace,
			Labels:    map[string]string{backup.LabelKeyComponent: backup.LabelComponentKCPShard},
		},
		Spec: druidv1alpha1.EtcdSpec{
			Replicas: 1,
			Labels:   map[string]string{"app": name},
			Backup: druidv1alpha1.BackupSpec{
				Store: &druidv1alpha1.StoreSpec{
					Prefix:   name,
					Provider: &localProvider,
				},
			},
		},
	}
}

// fakeFullSnapLease builds a full-snap coordination lease for the given etcd shard.
func fakeFullSnapLease(etcdName, holderIdentity string) *coordinationv1.Lease {
	return &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-full-snap", etcdName),
			Namespace: unitTestNamespace,
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity: ptr.To(holderIdentity),
		},
	}
}

// fakeBackup builds a minimal PlatformBackup with etcd enabled.
func fakeBackup(name string) *backupv1alpha1.PlatformBackup {
	return &backupv1alpha1.PlatformBackup{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: backupv1alpha1.PlatformBackupSpec{
			Storage: backupv1alpha1.StorageSpec{
				S3: backupv1alpha1.S3StorageSpec{
					Endpoint:       "http://minio:9000",
					Bucket:         "backups",
					CredentialsRef: corev1.LocalObjectReference{Name: "s3-credentials"},
				},
			},
			Components: backupv1alpha1.ComponentsSpec{
				Etcd: backupv1alpha1.EtcdSpec{Enabled: true},
			},
		},
	}
}

// fakeTask builds an EtcdOpsTask in the given terminal state.
func fakeTask(name, namespace string, state druidv1alpha1.TaskState, errs ...druidapicommon.LastError) *druidv1alpha1.EtcdOpsTask {
	t := &druidv1alpha1.EtcdOpsTask{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status:     druidv1alpha1.EtcdOpsTaskStatus{State: ptr.To(state)},
	}
	if len(errs) > 0 {
		t.Status.LastErrors = errs
	}
	return t
}

// newCaptureClientWithLeases builds a fake client where full-snap leases return NotFound on
// the first Get per shard (baseline read) and the provided holderIdentity on subsequent Gets.
// leases maps etcdName → holderIdentity.
func newCaptureClientWithLeases(t *testing.T, objs []client.Object, leases map[string]string) client.Client {
	t.Helper()

	leaseGVR := schema.GroupResource{Group: "coordination.k8s.io", Resource: "leases"}
	leaseObjs := make(map[string]*coordinationv1.Lease, len(leases))
	getCounts := make(map[string]*atomic.Int32, len(leases))
	for etcdName, key := range leases {
		leaseName := fmt.Sprintf("%s-full-snap", etcdName)
		leaseObjs[leaseName] = fakeFullSnapLease(etcdName, key)
		getCounts[leaseName] = new(atomic.Int32)
	}

	return fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(objs...).
		WithStatusSubresource(&druidv1alpha1.EtcdOpsTask{}).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*coordinationv1.Lease); ok {
					if counter, known := getCounts[key.Name]; known {
						if counter.Add(1) == 1 {
							return apierrors.NewNotFound(leaseGVR, key.Name)
						}
						leaseObjs[key.Name].DeepCopyInto(obj.(*coordinationv1.Lease))
						return nil
					}
				}
				return cl.Get(ctx, key, obj, opts...)
			},
		}).
		Build()
}

// TestCapture_EtcdDisabled verifies Process is a no-op when etcd is disabled in spec.
func TestCapture_EtcdDisabled(t *testing.T) {
	bkp := fakeBackup("b")
	bkp.Spec.Components.Etcd.Enabled = false

	cl := fake.NewClientBuilder().WithScheme(newFakeScheme(t)).Build()
	result, err := newCaptureSub().Process(ctxWithClient(cl), bkp)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
	assert.Nil(t, bkp.Status.Artefacts.Etcd)
}

// TestCapture_AlreadyCaptured verifies idempotency: if Etcd artefacts are already set, Process skips.
func TestCapture_AlreadyCaptured(t *testing.T) {
	bkp := fakeBackup("b")
	bkp.Status.Artefacts.Etcd = &backupv1alpha1.EtcdArtefact{
		Shards: map[string]backupv1alpha1.EtcdShardArtefact{
			"shard-x": {SnapshotKey: "rev-99"},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(newFakeScheme(t)).Build()
	result, err := newCaptureSub().Process(ctxWithClient(cl), bkp)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
	assert.Equal(t, "rev-99", bkp.Status.Artefacts.Etcd.Shards["shard-x"].SnapshotKey)
}

// TestCapture_WrongObjectType verifies that passing a non-PlatformBackup returns an error.
func TestCapture_WrongObjectType(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(newFakeScheme(t)).Build()
	restore := &backupv1alpha1.PlatformRestore{}
	_, err := newCaptureSub().Process(ctxWithClient(cl), restore)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected object type")
}

// TestCapture_NoShards verifies that the operator stops (not errors) when no
// kcp-shard Etcd CRs exist — so it does not spam the reconcile queue.
func TestCapture_NoShards(t *testing.T) {
	bkp := fakeBackup("b")
	cl := fake.NewClientBuilder().WithScheme(newFakeScheme(t)).Build()

	result, err := newCaptureSub().Process(ctxWithClient(cl), bkp)
	require.NoError(t, err)
	assert.False(t, result.IsContinue(), "expected Stop result when no shards found")
}

// TestCapture_SingleShard_Success verifies the happy path: task succeeds, lease key is recorded.
// The interceptor returns NotFound on the baseline lease read and the new key on subsequent reads.
func TestCapture_SingleShard_Success(t *testing.T) {
	bkp := fakeBackup("backup-1")
	shard := fakeEtcdShard("shard-a")
	task := fakeTask("backup-1-shard-a", unitTestNamespace, druidv1alpha1.TaskStateSucceeded)

	cl := newCaptureClientWithLeases(t,
		[]client.Object{shard, task},
		map[string]string{"shard-a": "rev-100"},
	)

	result, err := newCaptureSub().Process(ctxWithClient(cl), bkp)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
	require.NotNil(t, bkp.Status.Artefacts.Etcd)
	assert.Equal(t, "rev-100", bkp.Status.Artefacts.Etcd.Shards["shard-a"].SnapshotKey)
	assert.False(t, bkp.Status.Artefacts.Etcd.Shards["shard-a"].SnapshotTime.Time.IsZero())
}

// TestCapture_MultiShard_Success verifies all shards are captured and all keys recorded.
func TestCapture_MultiShard_Success(t *testing.T) {
	bkp := fakeBackup("backup-multi")
	shardA := fakeEtcdShard("shard-a")
	shardB := fakeEtcdShard("shard-b")
	taskA := fakeTask("backup-multi-shard-a", unitTestNamespace, druidv1alpha1.TaskStateSucceeded)
	taskB := fakeTask("backup-multi-shard-b", unitTestNamespace, druidv1alpha1.TaskStateSucceeded)

	cl := newCaptureClientWithLeases(t,
		[]client.Object{shardA, shardB, taskA, taskB},
		map[string]string{"shard-a": "rev-1", "shard-b": "rev-2"},
	)

	result, err := newCaptureSub().Process(ctxWithClient(cl), bkp)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
	require.NotNil(t, bkp.Status.Artefacts.Etcd)
	assert.Len(t, bkp.Status.Artefacts.Etcd.Shards, 2)
	assert.Equal(t, "rev-1", bkp.Status.Artefacts.Etcd.Shards["shard-a"].SnapshotKey)
	assert.Equal(t, "rev-2", bkp.Status.Artefacts.Etcd.Shards["shard-b"].SnapshotKey)
}

// TestCapture_TaskFailed_PropagatesError verifies that a Failed task surfaces its last error description.
func TestCapture_TaskFailed_PropagatesError(t *testing.T) {
	bkp := fakeBackup("backup-f")
	shard := fakeEtcdShard("shard-fail")
	task := fakeTask("backup-f-shard-fail", unitTestNamespace, druidv1alpha1.TaskStateFailed,
		druidapicommon.LastError{Code: "ERR_SNAPSHOT", Description: "disk full"},
	)

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(shard, task).
		WithStatusSubresource(&druidv1alpha1.EtcdOpsTask{}).
		Build()

	_, err := newCaptureSub().Process(ctxWithClient(cl), bkp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
}

// TestCapture_TaskRejected_PropagatesError verifies that a Rejected task surfaces as an error.
func TestCapture_TaskRejected_PropagatesError(t *testing.T) {
	bkp := fakeBackup("backup-r")
	shard := fakeEtcdShard("shard-rej")
	task := fakeTask("backup-r-shard-rej", unitTestNamespace, druidv1alpha1.TaskStateRejected,
		druidapicommon.LastError{Code: "ERR_BACKUP_NOT_ENABLED", Description: "backup is not enabled for etcd"},
	)

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(shard, task).
		WithStatusSubresource(&druidv1alpha1.EtcdOpsTask{}).
		Build()

	_, err := newCaptureSub().Process(ctxWithClient(cl), bkp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup is not enabled for etcd")
}

// TestCapture_TaskFailed_NoLastError verifies fallback to state name when LastErrors is empty.
func TestCapture_TaskFailed_NoLastError(t *testing.T) {
	bkp := fakeBackup("backup-noerr")
	shard := fakeEtcdShard("shard-noerr")
	task := fakeTask("backup-noerr-shard-noerr", unitTestNamespace, druidv1alpha1.TaskStateFailed)

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(shard, task).
		WithStatusSubresource(&druidv1alpha1.EtcdOpsTask{}).
		Build()

	_, err := newCaptureSub().Process(ctxWithClient(cl), bkp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), string(druidv1alpha1.TaskStateFailed))
}

// TestCapture_LeaseNotUpdated verifies an error when the lease key is empty after task success.
func TestCapture_LeaseNotUpdated(t *testing.T) {
	bkp := fakeBackup("backup-stale")
	shard := fakeEtcdShard("shard-stale")
	task := fakeTask("backup-stale-shard-stale", unitTestNamespace, druidv1alpha1.TaskStateSucceeded)
	lease := fakeFullSnapLease("shard-stale", "")
	lease.Spec.HolderIdentity = nil

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(shard, task, lease).
		WithStatusSubresource(&druidv1alpha1.EtcdOpsTask{}).
		Build()

	_, err := newCaptureSub().Process(ctxWithClient(cl), bkp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is empty")
}

// TestCapture_LeaseKeyMatchesBaseline verifies an error when the new key equals the baseline.
func TestCapture_LeaseKeyMatchesBaseline(t *testing.T) {
	bkp := fakeBackup("backup-same")
	shard := fakeEtcdShard("shard-same")
	task := fakeTask("backup-same-shard-same", unitTestNamespace, druidv1alpha1.TaskStateSucceeded)
	lease := fakeFullSnapLease("shard-same", "rev-old")

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(shard, task, lease).
		WithStatusSubresource(&druidv1alpha1.EtcdOpsTask{}).
		Build()

	_, err := newCaptureSub().Process(ctxWithClient(cl), bkp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not updated")
}

// TestCapture_PollTimeout verifies that a context timeout surfaces as a timeout error.
func TestCapture_PollTimeout(t *testing.T) {
	bkp := fakeBackup("backup-to")
	shard := fakeEtcdShard("shard-to")
	// Task exists but has no state — poll will spin until the context times out.
	task := &druidv1alpha1.EtcdOpsTask{
		ObjectMeta: metav1.ObjectMeta{Name: "backup-to-shard-to", Namespace: unitTestNamespace},
	}

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(shard, task).
		WithStatusSubresource(&druidv1alpha1.EtcdOpsTask{}).
		Build()

	sub := backup.NewEtcdCaptureSubroutine(unitTestNamespace).
		WithPollInterval(1*time.Millisecond, 50*time.Millisecond)

	_, err := sub.Process(ctxWithClient(cl), bkp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

// TestCapture_GetName verifies the subroutine returns the expected condition name.
func TestCapture_GetName(t *testing.T) {
	sub := backup.NewEtcdCaptureSubroutine("ns")
	assert.Equal(t, backup.ConditionEtcdSnapshotted, sub.GetName())
}

// TestCapture_TaskDeletedAfterSuccess verifies the EtcdOpsTask is deleted once it reaches Succeeded.
func TestCapture_TaskDeletedAfterSuccess(t *testing.T) {
	bkp := fakeBackup("backup-del")
	shard := fakeEtcdShard("shard-del")
	task := fakeTask("backup-del-shard-del", unitTestNamespace, druidv1alpha1.TaskStateSucceeded)

	cl := newCaptureClientWithLeases(t,
		[]client.Object{shard, task},
		map[string]string{"shard-del": "rev-1"},
	)

	_, err := newCaptureSub().Process(ctxWithClient(cl), bkp)
	require.NoError(t, err)

	var remaining druidv1alpha1.EtcdOpsTask
	getErr := cl.Get(context.Background(), types.NamespacedName{Name: "backup-del-shard-del", Namespace: unitTestNamespace}, &remaining)
	assert.True(t, apierrors.IsNotFound(getErr), "EtcdOpsTask should be deleted after success")
}

// TestCapture_TaskDeletedAfterFailure verifies the EtcdOpsTask is deleted once it reaches Failed,
// so that a subsequent reconcile can create a fresh task and recover.
func TestCapture_TaskDeletedAfterFailure(t *testing.T) {
	bkp := fakeBackup("backup-delf")
	shard := fakeEtcdShard("shard-delf")
	task := fakeTask("backup-delf-shard-delf", unitTestNamespace, druidv1alpha1.TaskStateFailed,
		druidapicommon.LastError{Code: "ERR", Description: "disk full"},
	)

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(shard, task).
		WithStatusSubresource(&druidv1alpha1.EtcdOpsTask{}).
		Build()

	_, err := newCaptureSub().Process(ctxWithClient(cl), bkp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")

	var remaining druidv1alpha1.EtcdOpsTask
	getErr := cl.Get(context.Background(), types.NamespacedName{Name: "backup-delf-shard-delf", Namespace: unitTestNamespace}, &remaining)
	assert.True(t, apierrors.IsNotFound(getErr), "EtcdOpsTask should be deleted after failure to allow retry")
}

// TestCapture_TaskNotFound_LeaseUpdated verifies that when the EtcdOpsTask is missing at poll
// time but the full-snap lease already carries a key different from the baseline, the operator
// treats the snapshot as complete (a prior reconcile already processed it).
func TestCapture_TaskNotFound_LeaseUpdated(t *testing.T) {
	bkp := fakeBackup("backup-gone")
	shard := fakeEtcdShard("shard-gone")

	taskGVR := schema.GroupResource{Group: "druid.gardener.cloud", Resource: "etcdopstasks"}
	leaseGVR := schema.GroupResource{Group: "coordination.k8s.io", Resource: "leases"}

	leaseGetCount := new(atomic.Int32)
	lease := fakeFullSnapLease("shard-gone", "rev-already")

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(shard).
		WithStatusSubresource(&druidv1alpha1.EtcdOpsTask{}).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*druidv1alpha1.EtcdOpsTask); ok {
					return apierrors.NewAlreadyExists(taskGVR, obj.GetName())
				}
				return c.Create(ctx, obj, opts...)
			},
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*druidv1alpha1.EtcdOpsTask); ok {
					return apierrors.NewNotFound(taskGVR, key.Name)
				}
				if _, ok := obj.(*coordinationv1.Lease); ok {
					// First call is the baseline read → return NotFound so baseline="".
					if leaseGetCount.Add(1) == 1 {
						return apierrors.NewNotFound(leaseGVR, key.Name)
					}
					lease.DeepCopyInto(obj.(*coordinationv1.Lease))
					return nil
				}
				return c.Get(ctx, key, obj, opts...)
			},
		}).
		Build()

	_, err := newCaptureSub().Process(ctxWithClient(cl), bkp)
	require.NoError(t, err)
	require.NotNil(t, bkp.Status.Artefacts.Etcd)
	assert.Equal(t, "rev-already", bkp.Status.Artefacts.Etcd.Shards["shard-gone"].SnapshotKey)
}

// TestCapture_TaskNotFound_LeaseNotUpdated verifies that when the task is gone and the lease
// has not been updated, an error is returned so the reconcile retries with a fresh task.
func TestCapture_TaskNotFound_LeaseNotUpdated(t *testing.T) {
	bkp := fakeBackup("backup-gone2")
	shard := fakeEtcdShard("shard-gone2")

	taskGVR := schema.GroupResource{Group: "druid.gardener.cloud", Resource: "etcdopstasks"}
	leaseGVR := schema.GroupResource{Group: "coordination.k8s.io", Resource: "leases"}

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(shard).
		WithStatusSubresource(&druidv1alpha1.EtcdOpsTask{}).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*druidv1alpha1.EtcdOpsTask); ok {
					return apierrors.NewAlreadyExists(taskGVR, obj.GetName())
				}
				return c.Create(ctx, obj, opts...)
			},
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*druidv1alpha1.EtcdOpsTask); ok {
					return apierrors.NewNotFound(taskGVR, key.Name)
				}
				if _, ok := obj.(*coordinationv1.Lease); ok {
					return apierrors.NewNotFound(leaseGVR, key.Name)
				}
				return c.Get(ctx, key, obj, opts...)
			},
		}).
		Build()

	_, err := newCaptureSub().Process(ctxWithClient(cl), bkp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found and lease not yet updated")
}
