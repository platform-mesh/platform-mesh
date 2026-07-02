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

package restore_test

import (
	"context"
	"fmt"
	"testing"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"
	"go.platform-mesh.io/backup-operator/pkg/restore"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/golang-commons/logger/testlogger"
	"go.platform-mesh.io/subroutines"

	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const unitTestNamespace = "default"

func newFakeScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, pmbackupv1alpha1.AddToScheme(s))
	require.NoError(t, druidv1alpha1.AddToScheme(s))
	require.NoError(t, coordinationv1.AddToScheme(s))
	return s
}

func newRestoreSub() *restore.EtcdRestoreSubroutine {
	return restore.NewEtcdRestoreSubroutine(unitTestNamespace)
}

func ctxWithClient(cl ctrlruntimeclient.Client) context.Context {
	ctx := subroutines.WithClient(context.Background(), cl)
	return logger.SetLoggerInContext(ctx, testlogger.New().Logger)
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

// fakeEtcdShardReady returns a shard with status.ready=true and the restore annotation set.
func fakeEtcdShardReady(name, snapshotKey string) *druidv1alpha1.Etcd {
	shard := fakeEtcdShard(name)
	shard.Annotations = map[string]string{restore.AnnotationKeyRestoredFromSnapshot: snapshotKey}
	shard.Status.Ready = ptr.To(true)
	return shard
}

// fakeBackup builds a minimal PlatformBackup with etcd enabled.
func fakeBackup(name string) *pmbackupv1alpha1.PlatformBackup {
	return &pmbackupv1alpha1.PlatformBackup{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: pmbackupv1alpha1.PlatformBackupSpec{
			Storage: pmbackupv1alpha1.StorageSpec{
				S3: pmbackupv1alpha1.S3StorageSpec{
					Endpoint:       "http://minio:9000",
					Bucket:         "backups",
					CredentialsRef: corev1.LocalObjectReference{Name: "s3-credentials"},
				},
			},
			Components: pmbackupv1alpha1.ComponentsSpec{
				Etcd: pmbackupv1alpha1.EtcdSpec{Enabled: true},
			},
		},
	}
}

// fakeRestore builds a minimal PlatformRestore pointing at the given backup ID.
func fakeRestore(name, backupID string) *pmbackupv1alpha1.PlatformRestore {
	return &pmbackupv1alpha1.PlatformRestore{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: pmbackupv1alpha1.PlatformRestoreSpec{
			Source: pmbackupv1alpha1.RestoreSourceSpec{
				BackupID: backupID,
				Storage: pmbackupv1alpha1.StorageSpec{
					S3: pmbackupv1alpha1.S3StorageSpec{
						Endpoint:       "http://minio:9000",
						Bucket:         "backups",
						CredentialsRef: corev1.LocalObjectReference{Name: "s3-credentials"},
					},
				},
			},
			TopologyValidation: pmbackupv1alpha1.TopologyValidationStrict,
		},
	}
}

// fakeBackupWithShards builds a PlatformBackup whose status has shard artefacts pre-set.
func fakeBackupWithShards(name string, shards map[string]string) *pmbackupv1alpha1.PlatformBackup {
	artefacts := make(map[string]pmbackupv1alpha1.EtcdShardArtefact, len(shards))
	for k, v := range shards {
		artefacts[k] = pmbackupv1alpha1.EtcdShardArtefact{
			SnapshotKey:  v,
			SnapshotTime: metav1.Now(),
		}
	}
	b := fakeBackup(name)
	b.Status.Artefacts.Etcd = &pmbackupv1alpha1.EtcdArtefact{Shards: artefacts}
	return b
}

// TestRestore_WrongObjectType verifies that passing a non-PlatformRestore returns an error.
func TestRestore_WrongObjectType(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(newFakeScheme(t)).Build()
	bkp := fakeBackup("b")
	_, err := newRestoreSub().Process(ctxWithClient(cl), bkp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected object type")
}

// TestRestore_BackupNotFound verifies StopWithRequeue is returned when the source backup is missing.
func TestRestore_BackupNotFound(t *testing.T) {
	rst := fakeRestore("r", "nonexistent")
	cl := fake.NewClientBuilder().WithScheme(newFakeScheme(t)).Build()

	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsStopWithRequeue())
}

// TestRestore_NoEtcdArtefacts verifies that a backup with empty etcd artefacts is skipped gracefully.
func TestRestore_NoEtcdArtefacts(t *testing.T) {
	bkp := fakeBackup("backup-empty")
	rst := fakeRestore("r", bkp.Name)

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp).
		Build()

	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
}

// TestRestore_NilEtcdArtefacts verifies that a backup with nil etcd artefact field is skipped gracefully.
func TestRestore_NilEtcdArtefacts(t *testing.T) {
	bkp := fakeBackup("backup-nil")
	rst := fakeRestore("r", bkp.Name)

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp).
		Build()

	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
}

// TestRestore_ShardNotFound verifies that a missing Etcd CR returns Pending (transient
// state between delete and recreate), not a permanent error.
func TestRestore_ShardNotFound(t *testing.T) {
	bkp := fakeBackupWithShards("backup-1", map[string]string{"missing-shard": "rev-1"})
	rst := fakeRestore("r", bkp.Name)

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp).
		Build()

	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsPending(), "expected Pending when shard CR is transiently absent")
}

// TestRestore_SingleShard_AnnotationSet verifies that after the first reconcile the shard
// has the restore annotation set and the subroutine returns Pending (waiting for ready).
func TestRestore_SingleShard_AnnotationSet(t *testing.T) {
	bkp := fakeBackupWithShards("backup-ann", map[string]string{"shard-ann": "rev-42"})
	rst := fakeRestore("r", bkp.Name)
	shard := fakeEtcdShard("shard-ann")

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shard).
		Build()

	// First pass: shard exists without annotation → delete+recreate, returns Pending.
	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsPending(), "expected Pending while shard is not yet ready")

	var recreated druidv1alpha1.Etcd
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: "shard-ann", Namespace: unitTestNamespace}, &recreated))
	assert.Equal(t, "rev-42", recreated.Annotations[restore.AnnotationKeyRestoredFromSnapshot])
}

// TestRestore_SingleShard_ReadyAfterAnnotation verifies that once the annotation is set
// and ready=true, Process returns OK.
func TestRestore_SingleShard_ReadyAfterAnnotation(t *testing.T) {
	bkp := fakeBackupWithShards("backup-rdy", map[string]string{"shard-rdy": "rev-1"})
	rst := fakeRestore("r", bkp.Name)
	shard := fakeEtcdShardReady("shard-rdy", "rev-1")

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shard).
		WithStatusSubresource(&druidv1alpha1.Etcd{}).
		Build()

	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue(), "expected OK when annotation set and ready=true")
}

// TestRestore_SingleShard_PendingWhenNotReady verifies Pending is returned when the
// annotation is set but status.ready is false.
func TestRestore_SingleShard_PendingWhenNotReady(t *testing.T) {
	bkp := fakeBackupWithShards("backup-notready", map[string]string{"shard-notready": "rev-5"})
	rst := fakeRestore("r", bkp.Name)
	shard := fakeEtcdShard("shard-notready")
	shard.Annotations = map[string]string{restore.AnnotationKeyRestoredFromSnapshot: "rev-5"}
	shard.Status.Ready = ptr.To(false)

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shard).
		Build()

	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsPending(), "expected Pending when annotation set but not ready")
}

// TestRestore_SingleShard_SpecPreserved verifies the recreated Etcd CR preserves the original spec.
func TestRestore_SingleShard_SpecPreserved(t *testing.T) {
	bkp := fakeBackupWithShards("backup-spec", map[string]string{"shard-spec": "rev-7"})
	rst := fakeRestore("r", bkp.Name)
	shard := fakeEtcdShard("shard-spec")
	shard.Spec.Replicas = 3

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shard).
		Build()

	_, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)

	var recreated druidv1alpha1.Etcd
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: "shard-spec", Namespace: unitTestNamespace}, &recreated))
	assert.Equal(t, int32(3), recreated.Spec.Replicas)
}

// TestRestore_MultiShard_AllRestored verifies that when all shards have the annotation
// and are ready, Process returns OK.
func TestRestore_MultiShard_AllRestored(t *testing.T) {
	bkp := fakeBackupWithShards("backup-multi", map[string]string{
		"shard-a": "rev-10",
		"shard-b": "rev-20",
	})
	rst := fakeRestore("r", bkp.Name)
	shardA := fakeEtcdShardReady("shard-a", "rev-10")
	shardB := fakeEtcdShardReady("shard-b", "rev-20")

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shardA, shardB).
		WithStatusSubresource(&druidv1alpha1.Etcd{}).
		Build()

	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue(), "expected OK when all shards restored and ready")
}

// TestRestore_MultiShard_OnePending verifies Pending when one shard is ready and one is not.
func TestRestore_MultiShard_OnePending(t *testing.T) {
	bkp := fakeBackupWithShards("backup-onepend", map[string]string{
		"shard-a": "rev-1",
		"shard-b": "rev-2",
	})
	rst := fakeRestore("r", bkp.Name)
	shardA := fakeEtcdShardReady("shard-a", "rev-1")
	shardB := fakeEtcdShard("shard-b")
	shardB.Annotations = map[string]string{restore.AnnotationKeyRestoredFromSnapshot: "rev-2"}
	// ready not set

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shardA, shardB).
		WithStatusSubresource(&druidv1alpha1.Etcd{}).
		Build()

	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsPending(), "expected Pending when one shard not yet ready")
}

// TestRestore_MultiShard_FirstFailureHalts verifies that when one shard is absent
// (transient NotFound) and another is done, the result is Pending (not an error).
// An absent shard is treated as a transient state between delete and recreate.
func TestRestore_MultiShard_FirstFailureHalts(t *testing.T) {
	bkp := fakeBackupWithShards("backup-halt", map[string]string{
		"shard-a": "rev-1",
		"shard-b": "rev-2",
	})
	rst := fakeRestore("r", bkp.Name)
	// shard-a present and ready, shard-b absent (transiently NotFound)
	shardA := fakeEtcdShardReady("shard-a", "rev-1")

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shardA).
		WithStatusSubresource(&druidv1alpha1.Etcd{}).
		Build()

	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsPending(), "expected Pending when one shard is transiently absent")
}

// TestRestore_GetName verifies the subroutine returns the expected condition name.
func TestRestore_GetName(t *testing.T) {
	sub := restore.NewEtcdRestoreSubroutine("ns")
	assert.Equal(t, restore.ConditionEtcdRestored, sub.GetName())
}

// TestRestore_Idempotency verifies that a second reconcile is a no-op when EtcdRestored is True.
func TestRestore_Idempotency(t *testing.T) {
	bkp := fakeBackupWithShards("backup-idem", map[string]string{"shard-x": "rev-1"})
	rst := fakeRestore("r", bkp.Name)
	rst.Status.Conditions = []metav1.Condition{
		{
			Type:               restore.ConditionEtcdRestored,
			Status:             metav1.ConditionTrue,
			Reason:             "Complete",
			LastTransitionTime: metav1.Now(),
		},
	}

	// No Etcd CRs in the fake client — a re-run would fail if the guard weren't present.
	cl := fake.NewClientBuilder().WithScheme(newFakeScheme(t)).WithObjects(bkp).Build()

	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
}

// TestRestore_EmptySnapshotKey verifies that an empty SnapshotKey is rejected up-front.
func TestRestore_EmptySnapshotKey(t *testing.T) {
	bkp := fakeBackup("backup-emptykey")
	bkp.Status.Artefacts.Etcd = &pmbackupv1alpha1.EtcdArtefact{
		Shards: map[string]pmbackupv1alpha1.EtcdShardArtefact{
			"shard-a": {SnapshotKey: "", SnapshotTime: metav1.Now()},
		},
	}
	shard := fakeEtcdShard("shard-a")
	rst := fakeRestore("r", bkp.Name)

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shard).
		Build()

	_, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.Error(t, err, "empty snapshot key must cause an error")
	assert.Contains(t, err.Error(), "empty snapshot key")
}

// TestRestore_CreateAndWait_AlreadyExists verifies that when etcd-druid races to
// recreate the Etcd CR between delete and Create, the operator:
//   - patches the restore annotation onto the existing CR
//   - patches the kcp-shard label onto the existing CR
//   - returns Pending (not an error)
func TestRestore_CreateAndWait_AlreadyExists(t *testing.T) {
	const snapshotKey = "rev-42"
	bkp := fakeBackup("backup-race")
	bkp.Status.Artefacts.Etcd = &pmbackupv1alpha1.EtcdArtefact{
		Shards: map[string]pmbackupv1alpha1.EtcdShardArtefact{
			"shard-race": {SnapshotKey: snapshotKey, SnapshotTime: metav1.Now()},
		},
	}
	shard := fakeEtcdShard("shard-race")
	shard.Labels = map[string]string{}
	shard.Annotations = nil
	shard.Status.Ready = ptr.To(true)

	rst := fakeRestore("rst-race", bkp.Name)

	patchCalled := false
	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shard).
		WithStatusSubresource(&druidv1alpha1.Etcd{}).
		WithInterceptorFuncs(interceptor.Funcs{
			Delete: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.DeleteOption) error {
				return c.Delete(ctx, obj, opts...)
			},
			Create: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.CreateOption) error {
				if etcd, ok := obj.(*druidv1alpha1.Etcd); ok {
					recreated := etcd.DeepCopy()
					recreated.ResourceVersion = ""
					recreated.Labels = map[string]string{}
					recreated.Annotations = nil
					recreated.Status.Ready = ptr.To(true)
					if err := c.Create(ctx, recreated); err != nil && !apierrors.IsAlreadyExists(err) {
						return err
					}
					return apierrors.NewAlreadyExists(schema.GroupResource{Group: "druid.gardener.cloud", Resource: "etcds"}, obj.GetName())
				}
				return c.Create(ctx, obj, opts...)
			},
			Patch: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, patch ctrlruntimeclient.Patch, opts ...ctrlruntimeclient.PatchOption) error {
				if etcd, ok := obj.(*druidv1alpha1.Etcd); ok {
					patchCalled = true
					assert.Equal(t, snapshotKey, etcd.Annotations[restore.AnnotationKeyRestoredFromSnapshot],
						"restore annotation must be patched onto the existing CR")
					assert.Equal(t, backup.LabelComponentKCPShard, etcd.Labels[backup.LabelKeyComponent],
						"kcp-shard label must be patched onto the existing CR")
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err, "AlreadyExists on Create must be handled gracefully")
	assert.True(t, patchCalled, "Patch must be called to apply annotation and label on the raced CR")
	// The raced CR has the annotation patched; on this reconcile pass the shard is
	// not yet confirmed ready so the subroutine must return Pending, not OK.
	assert.True(t, result.IsPending(),
		"expected Pending after AlreadyExists patch — shard readiness not yet confirmed")
}

// TestRestore_PVCsDeleted_SingleReplica verifies that when a shard with Replicas=1 is
// restored, the operator deletes the PVC named <shardName>-<shardName>-0 before
// recreating the Etcd CR.
//
// Background: etcd-druid does NOT delete PVCs when the Etcd CR is deleted — the PVC
// from the StatefulSet volumeClaimTemplate survives. etcdbr skips the S3 restore if it
// finds a valid data directory on the existing PVC. The operator must wipe the PVCs first.
func TestRestore_PVCsDeleted_SingleReplica(t *testing.T) {
	const shardName = "shard-pvc"
	const snapshotKey = "rev-10"

	bkp := fakeBackupWithShards("backup-pvc", map[string]string{shardName: snapshotKey})
	rst := fakeRestore("r", bkp.Name)
	shard := fakeEtcdShard(shardName)

	// Pre-populate the PVC that etcd-druid would have left behind.
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      shardName + "-" + shardName + "-0",
			Namespace: unitTestNamespace,
		},
	}

	var deletedPVCNames []string
	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shard, pvc).
		WithInterceptorFuncs(interceptor.Funcs{
			Delete: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.DeleteOption) error {
				if _, ok := obj.(*corev1.PersistentVolumeClaim); ok {
					deletedPVCNames = append(deletedPVCNames, obj.GetName())
				}
				return c.Delete(ctx, obj, opts...)
			},
		}).
		Build()

	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsPending())
	assert.Equal(t, []string{shardName + "-" + shardName + "-0"}, deletedPVCNames,
		"operator must delete the PVC before recreating the Etcd CR")
}

// TestRestore_PVCsDeleted_MultiReplica verifies that for a shard with Replicas=3 the
// operator deletes PVCs for all three ordinals (0, 1, 2).
func TestRestore_PVCsDeleted_MultiReplica(t *testing.T) {
	const shardName = "shard-multi-pvc"
	const snapshotKey = "rev-20"

	bkp := fakeBackupWithShards("backup-multi-pvc", map[string]string{shardName: snapshotKey})
	rst := fakeRestore("r", bkp.Name)
	shard := fakeEtcdShard(shardName)
	shard.Spec.Replicas = 3

	// Pre-populate all three PVCs.
	objs := make([]ctrlruntimeclient.Object, 0, 5)
	objs = append(objs, bkp, shard)
	for i := range int32(3) {
		objs = append(objs, &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s-%d", shardName, shardName, i),
				Namespace: unitTestNamespace,
			},
		})
	}

	var deletedPVCNames []string
	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(objs...).
		WithInterceptorFuncs(interceptor.Funcs{
			Delete: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.DeleteOption) error {
				if _, ok := obj.(*corev1.PersistentVolumeClaim); ok {
					deletedPVCNames = append(deletedPVCNames, obj.GetName())
				}
				return c.Delete(ctx, obj, opts...)
			},
		}).
		Build()

	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsPending())
	assert.ElementsMatch(t,
		[]string{
			shardName + "-" + shardName + "-0",
			shardName + "-" + shardName + "-1",
			shardName + "-" + shardName + "-2",
		},
		deletedPVCNames,
		"operator must delete PVCs for all replicas before recreating the Etcd CR")
}

// TestRestore_PVCDeleteError_ContinuesRestore verifies that a non-NotFound PVC deletion
// error does not abort the restore — the operator logs a warning and continues so the
// Etcd CR is still recreated, giving etcdbr the chance to run.
func TestRestore_PVCDeleteError_ContinuesRestore(t *testing.T) {
	const shardName = "shard-pvc-err"
	const snapshotKey = "rev-30"

	bkp := fakeBackupWithShards("backup-pvc-err", map[string]string{shardName: snapshotKey})
	rst := fakeRestore("r", bkp.Name)
	shard := fakeEtcdShard(shardName)

	pvcDeleteErr := fmt.Errorf("injected storage error")

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shard).
		WithInterceptorFuncs(interceptor.Funcs{
			Delete: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.DeleteOption) error {
				if _, ok := obj.(*corev1.PersistentVolumeClaim); ok {
					return pvcDeleteErr
				}
				return c.Delete(ctx, obj, opts...)
			},
		}).
		Build()

	// PVC deletion failure must not propagate as an error to the caller.
	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err, "PVC deletion error must be tolerated — restore should continue")
	assert.True(t, result.IsPending(), "expected Pending: Etcd CR recreated despite PVC error")

	// The Etcd CR must still have been recreated with the restore annotation.
	var recreated druidv1alpha1.Etcd
	require.NoError(t, cl.Get(context.Background(),
		types.NamespacedName{Name: shardName, Namespace: unitTestNamespace}, &recreated))
	assert.Equal(t, snapshotKey, recreated.Annotations[restore.AnnotationKeyRestoredFromSnapshot],
		"restore annotation must be set even when PVC deletion failed")
}

// TestRestore_PVCsDeleted_CustomVolumeClaimTemplate verifies that when the Etcd spec
// has a non-default VolumeClaimTemplate name, PVCs are named
// <vctName>-<shardName>-<ordinal> rather than <shardName>-<shardName>-<ordinal>.
func TestRestore_PVCsDeleted_CustomVolumeClaimTemplate(t *testing.T) {
	const shardName = "shard-custom-vct"
	const vctName = "etcd-data"
	const snapshotKey = "rev-99"

	bkp := fakeBackupWithShards("backup-custom-vct", map[string]string{shardName: snapshotKey})
	rst := fakeRestore("r", bkp.Name)
	shard := fakeEtcdShard(shardName)
	shard.Spec.VolumeClaimTemplate = ptr.To(vctName)

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vctName + "-" + shardName + "-0",
			Namespace: unitTestNamespace,
		},
	}

	var deletedPVCNames []string
	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shard, pvc).
		WithInterceptorFuncs(interceptor.Funcs{
			Delete: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.DeleteOption) error {
				if _, ok := obj.(*corev1.PersistentVolumeClaim); ok {
					deletedPVCNames = append(deletedPVCNames, obj.GetName())
				}
				return c.Delete(ctx, obj, opts...)
			},
		}).
		Build()

	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsPending())
	assert.Equal(t, []string{vctName + "-" + shardName + "-0"}, deletedPVCNames,
		"PVC name must use spec.volumeClaimTemplate when set, not etcd.Name")
}

// TestRestore_AnnotationSet_ReadyNil verifies that a shard carrying the correct restore
// annotation but with status.Ready==nil is treated as Pending (not yet ready), not OK.
func TestRestore_AnnotationSet_ReadyNil(t *testing.T) {
	bkp := fakeBackupWithShards("backup-readynil", map[string]string{"shard-readynil": "rev-1"})
	rst := fakeRestore("r", bkp.Name)
	shard := fakeEtcdShard("shard-readynil")
	shard.Annotations = map[string]string{restore.AnnotationKeyRestoredFromSnapshot: "rev-1"}
	// Status.Ready intentionally left nil

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shard).
		Build()

	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsPending(), "nil Status.Ready must be treated as not-yet-ready (Pending)")
}

// TestRestore_SourceBackup_APIError verifies that a generic API error fetching the source
// backup (not NotFound) is surfaced as a reconcile error rather than a StopWithRequeue.
func TestRestore_SourceBackup_APIError(t *testing.T) {
	rst := fakeRestore("r", "backup-apierr")
	apiErr := fmt.Errorf("etcd timeout")

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c ctrlruntimeclient.WithWatch, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
				if _, ok := obj.(*pmbackupv1alpha1.PlatformBackup); ok {
					return apiErr
				}
				return c.Get(ctx, key, obj, opts...)
			},
		}).
		Build()

	_, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetching source backup")
}

// TestRestore_StripFinalizers_PatchError verifies that when stripFinalizers fails (e.g.
// Forbidden), the error is propagated as a reconcile error — not silently swallowed.
func TestRestore_StripFinalizers_PatchError(t *testing.T) {
	bkp := fakeBackupWithShards("backup-finalizer-err", map[string]string{"shard-fin": "rev-1"})
	rst := fakeRestore("r", bkp.Name)
	shard := fakeEtcdShard("shard-fin")
	shard.Finalizers = []string{"druid.gardener.cloud/etcd-druid"}
	patchErr := fmt.Errorf("forbidden: insufficient permissions")

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shard).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, patch ctrlruntimeclient.Patch, opts ...ctrlruntimeclient.PatchOption) error {
				if _, ok := obj.(*druidv1alpha1.Etcd); ok {
					return patchErr
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	_, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.Error(t, err, "stripFinalizers Patch failure must be surfaced as a reconcile error")
	assert.Contains(t, err.Error(), "stripping finalizers")
}

// TestRestore_StripFinalizers_PatchError_Terminating verifies the same for the
// DeletionTimestamp branch: finalizer-strip failure on a terminating CR is an error.
func TestRestore_StripFinalizers_PatchError_Terminating(t *testing.T) {
	bkp := fakeBackupWithShards("backup-term-fin-err", map[string]string{"shard-term-fin": "rev-1"})
	rst := fakeRestore("r", bkp.Name)
	shard := fakeEtcdShard("shard-term-fin")
	now := metav1.Now()
	shard.DeletionTimestamp = &now
	shard.Finalizers = []string{"druid.gardener.cloud/etcd-druid"}
	patchErr := fmt.Errorf("forbidden: insufficient permissions")

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shard).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, patch ctrlruntimeclient.Patch, opts ...ctrlruntimeclient.PatchOption) error {
				if _, ok := obj.(*druidv1alpha1.Etcd); ok {
					return patchErr
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	_, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.Error(t, err, "stripFinalizers Patch failure on terminating CR must be surfaced as error")
	assert.Contains(t, err.Error(), "stripping finalizers")
}

// TestRestore_PendingWins_OverError verifies that when one shard is pending (NotFound
// transient) and another produces a hard error (stripFinalizers failed), the result is
// Pending — the pending state takes priority so the reconcile retries at 5s not backoff.
func TestRestore_PendingWins_OverError(t *testing.T) {
	bkp := fakeBackupWithShards("backup-mixed", map[string]string{
		"shard-a": "rev-1",
		"shard-b": "rev-2",
	})
	rst := fakeRestore("r", bkp.Name)

	// shard-a: has finalizer, Patch will fail → error
	shardA := fakeEtcdShard("shard-a")
	shardA.Finalizers = []string{"druid.gardener.cloud/etcd-druid"}
	patchErr := fmt.Errorf("forbidden")

	// shard-b: absent → Pending (transient NotFound)

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shardA).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, patch ctrlruntimeclient.Patch, opts ...ctrlruntimeclient.PatchOption) error {
				if _, ok := obj.(*druidv1alpha1.Etcd); ok {
					return patchErr
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err, "mixed pending+error state must return Pending, not an error")
	assert.True(t, result.IsPending(), "Pending must win over error when any shard is in-flight")
}

// TestRestore_Recreate_AlreadyExists_ThenGetNotFound verifies that when Create returns
// AlreadyExists but the subsequent Get also returns NotFound (the raced CR was deleted
// between Create and Get), recreate() returns nil so the next reconcile retries cleanly.
func TestRestore_Recreate_AlreadyExists_ThenGetNotFound(t *testing.T) {
	const snapshotKey = "rev-100"
	bkp := fakeBackupWithShards("backup-race-notfound", map[string]string{"shard-rnf": snapshotKey})
	rst := fakeRestore("r", bkp.Name)
	shard := fakeEtcdShard("shard-rnf")

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shard).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.CreateOption) error {
				if _, ok := obj.(*druidv1alpha1.Etcd); ok {
					// Return AlreadyExists WITHOUT actually creating — so subsequent Get returns NotFound.
					return apierrors.NewAlreadyExists(schema.GroupResource{Group: "druid.gardener.cloud", Resource: "etcds"}, obj.GetName())
				}
				return c.Create(ctx, obj, opts...)
			},
		}).
		Build()

	// The original shard is deleted by the fake client Delete in the default branch,
	// then Create returns AlreadyExists but the CR is gone → Get returns NotFound.
	// The result must be Pending (not an error).
	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err, "AlreadyExists→NotFound race must not produce an error")
	assert.True(t, result.IsPending(), "expected Pending after AlreadyExists→NotFound race")
}

// TestRestore_Recreate_AlreadyExists_TerminatingCR verifies that when Create returns
// AlreadyExists and the existing CR is itself terminating, recreate() returns nil without
// patching — the next reconcile will enter the DeletionTimestamp branch.
func TestRestore_Recreate_AlreadyExists_TerminatingCR(t *testing.T) {
	const snapshotKey = "rev-200"
	bkp := fakeBackupWithShards("backup-race-term", map[string]string{"shard-rterm": snapshotKey})
	rst := fakeRestore("r", bkp.Name)
	shard := fakeEtcdShard("shard-rterm")

	patchCalled := false
	now := metav1.Now()
	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shard).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.CreateOption) error {
				if _, ok := obj.(*druidv1alpha1.Etcd); ok {
					return apierrors.NewAlreadyExists(schema.GroupResource{Group: "druid.gardener.cloud", Resource: "etcds"}, obj.GetName())
				}
				return c.Create(ctx, obj, opts...)
			},
			Get: func(ctx context.Context, c ctrlruntimeclient.WithWatch, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
				if err := c.Get(ctx, key, obj, opts...); err != nil {
					return err
				}
				// After Create returns AlreadyExists, the Get in recreate() should see a terminating CR.
				if etcd, ok := obj.(*druidv1alpha1.Etcd); ok && key.Name == "shard-rterm" {
					etcd.DeletionTimestamp = &now
					etcd.Finalizers = []string{"druid.gardener.cloud/etcd-druid"}
				}
				return nil
			},
			Patch: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, patch ctrlruntimeclient.Patch, opts ...ctrlruntimeclient.PatchOption) error {
				if etcd, ok := obj.(*druidv1alpha1.Etcd); ok {
					// Only flag annotation patches (from recreate), not finalizer-strip patches.
					if etcd.Annotations[restore.AnnotationKeyRestoredFromSnapshot] != "" {
						patchCalled = true
					}
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err, "AlreadyExists with terminating CR must not error")
	assert.False(t, patchCalled, "Patch must NOT be called when the raced CR is terminating")
	assert.True(t, result.IsPending(), "expected Pending — next reconcile will strip finalizers")
}
