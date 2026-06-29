//go:build integration

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
	"testing"
	"time"

	druidapicommon "github.com/gardener/etcd-druid/api/common"
	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	backupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"
	"go.platform-mesh.io/subroutines"

	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/golang-commons/logger/testlogger"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// runTaskSimulator watches for EtcdOpsTask objects and transitions them to Succeeded,
// also updating the full-snap lease to simulate a completed snapshot.
func runTaskSimulator(ctx context.Context, t *testing.T, cl client.Client, snapshotKeyFn func(etcdName string) string) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}

			var taskList druidv1alpha1.EtcdOpsTaskList
			if err := cl.List(ctx, &taskList, client.InNamespace(testNamespace)); err != nil {
				continue
			}

			for i := range taskList.Items {
				task := &taskList.Items[i]
				if task.Status.State != nil {
					switch *task.Status.State {
					case druidv1alpha1.TaskStateSucceeded,
						druidv1alpha1.TaskStateFailed,
						druidv1alpha1.TaskStateRejected:
						continue
					}
				}

				etcdName := ""
				if task.Spec.EtcdName != nil {
					etcdName = *task.Spec.EtcdName
				}

				key := snapshotKeyFn(etcdName)
				leaseName := druidv1alpha1.GetFullSnapshotLeaseName(metav1.ObjectMeta{Name: etcdName})
				lease := &coordinationv1.Lease{
					ObjectMeta: metav1.ObjectMeta{Name: leaseName, Namespace: testNamespace},
					Spec:       coordinationv1.LeaseSpec{HolderIdentity: ptr.To(key)},
				}
				existing := &coordinationv1.Lease{}
				if err := cl.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, existing); err != nil {
					_ = cl.Create(ctx, lease)
				} else {
					existing.Spec.HolderIdentity = ptr.To(key)
					_ = cl.Update(ctx, existing)
				}

				succeeded := druidv1alpha1.TaskStateSucceeded
				patch := client.MergeFrom(task.DeepCopy())
				task.Status.State = &succeeded
				if patchErr := cl.Status().Patch(ctx, task, patch); patchErr != nil {
					t.Logf("simulator succeeded patch error: %v", patchErr)
				}
			}
		}
	}()
}

func injectClient(ctx context.Context, cl client.Client) context.Context {
	ctx = logger.SetLoggerInContext(ctx, testlogger.New().Logger)
	return subroutines.WithClient(ctx, cl)
}

func makeEtcdShard(t *testing.T, cl client.Client, name string) {
	t.Helper()
	localProvider := druidv1alpha1.StorageProvider("Local")
	etcd := &druidv1alpha1.Etcd{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
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
	require.NoError(t, cl.Create(t.Context(), etcd))
	patch := client.MergeFrom(etcd.DeepCopy())
	etcd.Status.Ready = ptr.To(true)
	require.NoError(t, cl.Status().Patch(t.Context(), etcd, patch))
}

func makePlatformBackup(t *testing.T, cl client.Client, name string) *backupv1alpha1.PlatformBackup {
	t.Helper()
	bkp := &backupv1alpha1.PlatformBackup{
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
	require.NoError(t, cl.Create(t.Context(), bkp))
	return bkp
}

// TestCapture_MultiShard_Success verifies that the etcd capture subroutine triggers snapshot tasks for all shards and records snapshot keys and timestamps in the backup status when all tasks succeed.
func TestCapture_MultiShard_Success(t *testing.T) {
	cl, _, stop := setupEnvtest(t)
	defer stop()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	runReadySimulator(ctx, cl)

	snapshotRevision := 1000
	runTaskSimulator(ctx, t, cl, func(etcdName string) string {
		snapshotRevision++
		return fmt.Sprintf("%d", snapshotRevision)
	})

	makeEtcdShard(t, cl, "shard-a")
	makeEtcdShard(t, cl, "shard-b")
	bkp := makePlatformBackup(t, cl, "backup-multi")

	sub := backup.NewEtcdCaptureSubroutine(testNamespace).
		WithPollInterval(100*time.Millisecond, 20*time.Second)

	result, err := sub.Process(injectClient(ctx, cl), bkp)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())

	require.NotNil(t, bkp.Status.Artefacts.Etcd)
	assert.Len(t, bkp.Status.Artefacts.Etcd.Shards, 2)
	for _, shard := range bkp.Status.Artefacts.Etcd.Shards {
		assert.NotEmpty(t, shard.SnapshotKey)
		assert.False(t, shard.SnapshotTime.IsZero())
	}
}

// TestCapture_TaskFailed_ReturnsError verifies that the etcd capture subroutine returns an error containing the failure description when an EtcdOpsTask transitions to the Failed state.
func TestCapture_TaskFailed_ReturnsError(t *testing.T) {
	cl, _, stop := setupEnvtest(t)
	defer stop()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	runReadySimulator(ctx, cl)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			var taskList druidv1alpha1.EtcdOpsTaskList
			if err := cl.List(ctx, &taskList, client.InNamespace(testNamespace)); err != nil {
				continue
			}
			for i := range taskList.Items {
				task := &taskList.Items[i]
				if task.Status.State != nil {
					switch *task.Status.State {
					case druidv1alpha1.TaskStateSucceeded,
						druidv1alpha1.TaskStateFailed,
						druidv1alpha1.TaskStateRejected:
						continue
					}
				}
				failed := druidv1alpha1.TaskStateFailed
				patch := client.MergeFrom(task.DeepCopy())
				task.Status.State = &failed
				task.Status.LastErrors = []druidapicommon.LastError{
					{
						Code:        "ERR_SNAPSHOT",
						Description: "simulated backup-restore failure",
						ObservedAt:  metav1.Now(),
					},
				}
				if err := cl.Status().Patch(ctx, task, patch); err != nil {
					t.Logf("simulator patch error: %v", err)
				}
			}
		}
	}()

	makeEtcdShard(t, cl, "shard-fail")
	bkp := makePlatformBackup(t, cl, "backup-fail")

	sub := backup.NewEtcdCaptureSubroutine(testNamespace).
		WithPollInterval(100*time.Millisecond, 20*time.Second)

	_, err := sub.Process(injectClient(ctx, cl), bkp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "simulated backup-restore failure")
}

// TestCapture_Idempotent verifies that calling the etcd capture subroutine a second time when snapshot artefacts are already present leaves the existing data unchanged and returns a continue result.
func TestCapture_Idempotent(t *testing.T) {
	cl, _, stop := setupEnvtest(t)
	defer stop()

	ctx := t.Context()

	bkp := makePlatformBackup(t, cl, "backup-idempotent")
	bkp.Status.Artefacts.Etcd = &backupv1alpha1.EtcdArtefact{
		Shards: map[string]backupv1alpha1.EtcdShardArtefact{
			"shard-x": {SnapshotKey: "already-captured"},
		},
	}

	sub := backup.NewEtcdCaptureSubroutine(testNamespace).
		WithPollInterval(100*time.Millisecond, 5*time.Second)

	result, err := sub.Process(injectClient(ctx, cl), bkp)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
	assert.Equal(t, "already-captured", bkp.Status.Artefacts.Etcd.Shards["shard-x"].SnapshotKey)
}
