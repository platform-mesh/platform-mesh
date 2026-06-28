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
	"testing"
	"time"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
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
	return restore.NewEtcdRestoreSubroutine(unitTestNamespace).
		WithPollIntervals(1*time.Millisecond, 5*time.Second, 1*time.Millisecond, 5*time.Second)
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
			Labels:    map[string]string{restore.LabelKeyComponent: restore.LabelComponentKCPShard},
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

// newRestoreClient builds a fake client pre-loaded with the given objects, plus an interceptor
// that makes every Etcd Get return status.ready=true so waitForReady passes immediately.
func newRestoreClient(t *testing.T, objs ...ctrlruntimeclient.Object) ctrlruntimeclient.Client {
	t.Helper()
	return fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(objs...).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cl ctrlruntimeclient.WithWatch, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
				if err := cl.Get(ctx, key, obj, opts...); err != nil {
					return err
				}
				if etcd, ok := obj.(*druidv1alpha1.Etcd); ok {
					etcd.Status.Ready = ptr.To(true)
				}
				return nil
			},
		}).
		Build()
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

// TestRestore_ShardNotFound verifies that a missing Etcd CR returns an error.
func TestRestore_ShardNotFound(t *testing.T) {
	bkp := fakeBackupWithShards("backup-1", map[string]string{"missing-shard": "rev-1"})
	rst := fakeRestore("r", bkp.Name)

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp).
		Build()

	_, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestRestore_SingleShard_AnnotationSet verifies the restored Etcd CR has the snapshot annotation.
func TestRestore_SingleShard_AnnotationSet(t *testing.T) {
	bkp := fakeBackupWithShards("backup-ann", map[string]string{"shard-ann": "rev-42"})
	rst := fakeRestore("r", bkp.Name)
	shard := fakeEtcdShard("shard-ann")

	cl := newRestoreClient(t, bkp, shard)

	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())

	var recreated druidv1alpha1.Etcd
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: "shard-ann", Namespace: unitTestNamespace}, &recreated))
	assert.Equal(t, "rev-42", recreated.Annotations[restore.AnnotationKeyRestoredFromSnapshot])
}

// TestRestore_SingleShard_SpecPreserved verifies the recreated Etcd CR preserves the original spec.
func TestRestore_SingleShard_SpecPreserved(t *testing.T) {
	bkp := fakeBackupWithShards("backup-spec", map[string]string{"shard-spec": "rev-7"})
	rst := fakeRestore("r", bkp.Name)
	shard := fakeEtcdShard("shard-spec")
	shard.Spec.Replicas = 3

	cl := newRestoreClient(t, bkp, shard)

	_, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)

	var recreated druidv1alpha1.Etcd
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: "shard-spec", Namespace: unitTestNamespace}, &recreated))
	assert.Equal(t, int32(3), recreated.Spec.Replicas)
}

// TestRestore_MultiShard_AllRestored verifies all shards are restored sequentially.
func TestRestore_MultiShard_AllRestored(t *testing.T) {
	bkp := fakeBackupWithShards("backup-multi", map[string]string{
		"shard-a": "rev-10",
		"shard-b": "rev-20",
	})
	rst := fakeRestore("r", bkp.Name)
	shardA := fakeEtcdShard("shard-a")
	shardB := fakeEtcdShard("shard-b")

	cl := newRestoreClient(t, bkp, shardA, shardB)

	result, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())

	for _, name := range []string{"shard-a", "shard-b"} {
		var recreated druidv1alpha1.Etcd
		require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: name, Namespace: unitTestNamespace}, &recreated))
		assert.NotEmpty(t, recreated.Annotations[restore.AnnotationKeyRestoredFromSnapshot])
	}
}

// TestRestore_MultiShard_FirstFailureHalts verifies all-or-nothing: first shard error stops processing.
// shard-a sorts before shard-b; shard-b is missing so restore returns an error referencing shard-b.
func TestRestore_MultiShard_FirstFailureHalts(t *testing.T) {
	bkp := fakeBackupWithShards("backup-halt", map[string]string{
		"shard-a": "rev-1",
		"shard-b": "rev-2",
	})
	rst := fakeRestore("r", bkp.Name)
	shardA := fakeEtcdShard("shard-a")

	cl := newRestoreClient(t, bkp, shardA)

	_, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shard-b")
}

// TestRestore_ReadyTimeout verifies that a timeout waiting for readiness surfaces as an error.
func TestRestore_ReadyTimeout(t *testing.T) {
	bkp := fakeBackupWithShards("backup-rdy", map[string]string{"shard-rdy": "rev-1"})
	rst := fakeRestore("r", bkp.Name)
	shard := fakeEtcdShard("shard-rdy")

	// Plain fake client: the recreated Etcd CR has no ready status set.
	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shard).
		Build()

	sub := restore.NewEtcdRestoreSubroutine(unitTestNamespace).
		WithPollIntervals(1*time.Millisecond, 5*time.Second, 1*time.Millisecond, 50*time.Millisecond)

	_, err := sub.Process(ctxWithClient(cl), rst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
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

// TestRestore_EmptySnapshotKey verifies that fanOutRestore returns an error when
// a shard artefact has an empty SnapshotKey. The API normally prevents this via
// CEL validation, but this guard protects against future schema changes or
// direct status manipulation.
func TestRestore_EmptySnapshotKey(t *testing.T) {
	bkp := fakeBackup("backup-emptykey")
	bkp.Status.Artefacts.Etcd = &pmbackupv1alpha1.EtcdArtefact{
		Shards: map[string]pmbackupv1alpha1.EtcdShardArtefact{
			"shard-a": {SnapshotKey: "", SnapshotTime: metav1.Now()}, // empty key
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
	assert.Contains(t, err.Error(), "empty snapshot key",
		"error must describe the problem")
}

// TestRestore_CreateAndWait_AlreadyExists verifies that when etcd-druid races to
// recreate the Etcd CR between waitForDeletion and Create, the operator:
//   - patches the restore annotation onto the existing CR
//   - patches the kcp-shard label onto the existing CR
//   - proceeds to waitForReady without returning an error
func TestRestore_CreateAndWait_AlreadyExists(t *testing.T) {
	const snapshotKey = "rev-42"
	bkp := fakeBackup("backup-race")
	bkp.Status.Artefacts.Etcd = &pmbackupv1alpha1.EtcdArtefact{
		Shards: map[string]pmbackupv1alpha1.EtcdShardArtefact{
			"shard-race": {SnapshotKey: snapshotKey, SnapshotTime: metav1.Now()},
		},
	}
	// The shard CR has no annotation — simulates first-restore state.
	shard := fakeEtcdShard("shard-race")
	shard.Labels = map[string]string{} // strip kcp-shard label to simulate druid recreation
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
				// Simulate deletion that appears instant to the operator: the fake
				// client removes the object, but our Create interceptor below will
				// return AlreadyExists (druid recreated it before the operator's Create).
				return c.Delete(ctx, obj, opts...)
			},
			Create: func(ctx context.Context, c ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.CreateOption) error {
				if etcd, ok := obj.(*druidv1alpha1.Etcd); ok {
					// Simulate etcd-druid racing to recreate — put the object back first.
					recreated := etcd.DeepCopy()
					recreated.ResourceVersion = ""
					recreated.Labels = map[string]string{} // druid-created CR has no platform-mesh labels
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
					assert.Equal(t, restore.LabelComponentKCPShard, etcd.Labels[restore.LabelKeyComponent],
						"kcp-shard label must be patched onto the existing CR")
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	_, err := newRestoreSub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err, "AlreadyExists on Create must be handled gracefully")
	assert.True(t, patchCalled, "Patch must be called to apply annotation and label on the raced CR")
}
