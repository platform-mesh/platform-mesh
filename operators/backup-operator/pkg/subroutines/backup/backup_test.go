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
	"bytes"
	"context"
	"encoding/json"
	"testing"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/internal/testfixtures"
	"go.platform-mesh.io/backup-operator/pkg/archive"
	"go.platform-mesh.io/backup-operator/pkg/manifest"
	"go.platform-mesh.io/backup-operator/pkg/pki"
	"go.platform-mesh.io/backup-operator/pkg/storage"
	subbackup "go.platform-mesh.io/backup-operator/pkg/subroutines/backup"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func newBackup(name string) *pmbackupv1alpha1.Backup {
	return &pmbackupv1alpha1.Backup{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testfixtures.Namespace},
		Spec:       pmbackupv1alpha1.BackupSpec{StorageRef: corev1.LocalObjectReference{Name: testfixtures.Storage}},
	}
}

func newSubs(cl ctrlruntimeclient.Client, stores *storage.MemoryFactory) []subroutines.Subroutine {
	return subbackup.New(cl, stores, subbackup.FromBackupStorage(cl, stores))
}

// extract reads the archive of a completed backup.
func extract(t *testing.T, stores *storage.MemoryFactory, key string) *archive.Extracted {
	t.Helper()
	data, err := storage.GetBytes(context.Background(), stores.Bucket(testfixtures.BackupBkt), key)
	require.NoError(t, err)
	x, err := archive.Extract(bytes.NewReader(data), testfixtures.Key, t.TempDir())
	require.NoError(t, err)
	return x
}

func TestBackupCompletes(t *testing.T) {
	ctx := context.Background()
	cl := testfixtures.Cluster(t)
	stores := &storage.MemoryFactory{}
	subs := newSubs(cl, stores)
	b := newBackup("daily")
	b.UID = "uid-daily"

	res, stoppedAt := testfixtures.Run(ctx, t, subs, b)
	require.True(t, res.IsStopWithRequeue())
	assert.Equal(t, "EtcdSnapshot", stoppedAt)
	assert.Equal(t, pmbackupv1alpha1.PhaseRunning, b.Status.Phase)
	assert.Equal(t, "backups/platform-mesh-system/daily.tar", b.Status.Archive)

	tasks := &druidv1alpha1.EtcdOpsTaskList{}
	require.NoError(t, cl.List(ctx, tasks))
	require.Len(t, tasks.Items, 3, "one task for each etcd, all created in the first run")
	assert.Equal(t, druidv1alpha1.OnDemandSnapshotTypeFull, tasks.Items[0].Spec.Config.OnDemandSnapshot.Type)
	assert.Equal(t, "daily", tasks.Items[0].OwnerReferences[0].Name)

	// a second run while druid is busy does not create more tasks
	res, _ = testfixtures.Run(ctx, t, subs, b)
	require.True(t, res.IsStopWithRequeue())
	require.NoError(t, cl.List(ctx, tasks))
	require.Len(t, tasks.Items, 3)

	testfixtures.SimulateDruid(ctx, t, cl, stores, 100)
	res, stoppedAt = testfixtures.Run(ctx, t, subs, b)
	require.True(t, res.IsContinue(), "stopped at %s", stoppedAt)
	assert.Equal(t, pmbackupv1alpha1.PhaseCompleted, b.Status.Phase)
	assert.NotNil(t, b.Status.CompletionTime)
	assert.Equal(t, 3, b.Status.Secrets)
	assert.Equal(t, []string{"backups/platform-mesh-system/daily.tar"}, stores.Bucket(testfixtures.BackupBkt).Keys(), "one archive, nothing else")

	x := extract(t, stores, b.Status.Archive)
	m := x.Manifest
	require.Len(t, m.Etcds, 3)
	for _, e := range m.Etcds {
		data, err := x.ReadAll(e.Entry())
		require.NoError(t, err)
		assert.Equal(t, "snapshot "+e.Name+"100", string(data))
		assert.Equal(t, manifest.SHA256(data), e.SHA256)
		assert.Equal(t, testfixtures.StorePrefix(e.Name), e.StorePrefix)
		assert.Equal(t, "Full-00000000-00000100-1", e.Snapshot)
	}
	assert.Equal(t, []string{"root"}, m.Topology.RootShards)
	assert.Equal(t, []string{"triton"}, m.Topology.Shards)

	plain, err := x.ReadAll(manifest.PKIEntry)
	require.NoError(t, err)
	bundle := &pki.Bundle{}
	require.NoError(t, json.Unmarshal(plain, bundle))
	names := make([]string, 0, len(bundle.Secrets))
	for _, s := range bundle.Secrets {
		names = append(names, s.Name)
	}
	assert.Equal(t, testfixtures.KcpPKISecrets, names)

	// a finished backup is not run again
	res, _ = testfixtures.Run(ctx, t, subs, b)
	assert.True(t, res.IsSkipAll())
	assert.True(t, res.Ready())
}

func TestBackupPreflightFails(t *testing.T) {
	tests := map[string]struct {
		change func(t *testing.T, cl ctrlruntimeclient.Client)
		msg    string
	}{
		"etcd not ready": {
			change: func(t *testing.T, cl ctrlruntimeclient.Client) {
				e := &druidv1alpha1.Etcd{}
				require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Namespace: testfixtures.Namespace, Name: "etcd-kcp-triton"}, e))
				e.Status.Ready = ptr.To(false)
				require.NoError(t, cl.Status().Update(context.Background(), e))
			},
			msg: "etcd etcd-kcp-triton is not ready",
		},
		"etcd without store": {
			change: func(t *testing.T, cl ctrlruntimeclient.Client) {
				e := &druidv1alpha1.Etcd{}
				require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Namespace: testfixtures.Namespace, Name: "etcd-cache"}, e))
				e.Spec.Backup.Store = nil
				require.NoError(t, cl.Update(context.Background(), e))
			},
			msg: "etcd etcd-cache has no backup store",
		},
		"store prefixes overlap": {
			change: func(t *testing.T, cl ctrlruntimeclient.Client) {
				e := &druidv1alpha1.Etcd{}
				require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Namespace: testfixtures.Namespace, Name: "etcd-kcp"}, e))
				e.Spec.Backup.Store.Prefix = "etcd-kcp"
				require.NoError(t, cl.Update(context.Background(), e))
			},
			msg: `etcd etcd-kcp-triton: store prefix "etcd-kcp-triton-store" starts with "etcd-kcp" of etcd etcd-kcp in the same bucket, druid would mix their snapshots`,
		},
		"short encryption key": {
			change: func(t *testing.T, cl ctrlruntimeclient.Client) {
				s := &corev1.Secret{}
				require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Namespace: testfixtures.Namespace, Name: "backup-key"}, s))
				s.Data["key"] = []byte("short")
				require.NoError(t, cl.Update(context.Background(), s))
			},
			msg: "encryption key secret backup-key: key must be 32 bytes, got 5",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			cl := testfixtures.Cluster(t)
			tc.change(t, cl)
			b := newBackup("daily")
			res, stoppedAt := testfixtures.Run(ctx, t, newSubs(cl, &storage.MemoryFactory{}), b)
			assert.True(t, res.IsStop())
			assert.Equal(t, "Preflight", stoppedAt)
			assert.Equal(t, pmbackupv1alpha1.PhaseFailed, b.Status.Phase)
			assert.Equal(t, tc.msg, b.Status.Message)

			tasks := &druidv1alpha1.EtcdOpsTaskList{}
			require.NoError(t, cl.List(ctx, tasks))
			assert.Empty(t, tasks.Items, "nothing is started when preflight fails")

			res, _ = testfixtures.Run(ctx, t, newSubs(cl, &storage.MemoryFactory{}), b)
			assert.True(t, res.IsSkipAll())
			assert.False(t, res.Ready())
		})
	}
}

func TestBackupFailsWhenSnapshotTaskFails(t *testing.T) {
	ctx := context.Background()
	cl := testfixtures.Cluster(t)
	stores := &storage.MemoryFactory{}
	subs := newSubs(cl, stores)
	b := newBackup("daily")
	testfixtures.Run(ctx, t, subs, b)

	task := &druidv1alpha1.EtcdOpsTask{}
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Namespace: testfixtures.Namespace, Name: subbackup.TaskName("daily", "etcd-kcp")}, task))
	task.Status.State = ptr.To(druidv1alpha1.TaskStateFailed)
	require.NoError(t, cl.Status().Update(ctx, task))

	res, _ := testfixtures.Run(ctx, t, subs, b)
	assert.True(t, res.IsStop())
	assert.Equal(t, pmbackupv1alpha1.PhaseFailed, b.Status.Phase)
	assert.Contains(t, b.Status.Message, "snapshot of etcd etcd-kcp")
	assert.Empty(t, stores.Bucket(testfixtures.BackupBkt).Keys(), "a failed backup has no archive")
}

// TestBackupWithoutObject is the backup command: the Backup is not stored in the cluster.
func TestBackupWithoutObject(t *testing.T) {
	ctx := context.Background()
	cl := testfixtures.Cluster(t)
	stores := &storage.MemoryFactory{}
	opened := &storage.Opened{Store: stores.Bucket("cli"), Prefix: "manual", Key: testfixtures.Key}
	subs := subbackup.New(cl, stores, func(context.Context, *pmbackupv1alpha1.Backup) (*storage.Opened, error) { return opened, nil })
	b := &pmbackupv1alpha1.Backup{ObjectMeta: metav1.ObjectMeta{Name: "cli", Namespace: testfixtures.Namespace}}

	testfixtures.Run(ctx, t, subs, b)
	tasks := &druidv1alpha1.EtcdOpsTaskList{}
	require.NoError(t, cl.List(ctx, tasks))
	require.Len(t, tasks.Items, 3)
	assert.Empty(t, tasks.Items[0].OwnerReferences)

	testfixtures.SimulateDruid(ctx, t, cl, stores, 100)
	testfixtures.Run(ctx, t, subs, b)
	require.Equal(t, pmbackupv1alpha1.PhaseCompleted, b.Status.Phase, b.Status.Message)
	assert.Equal(t, []string{"manual/platform-mesh-system/cli.tar"}, stores.Bucket("cli").Keys())
}

func TestBackupDoesNotOverwrite(t *testing.T) {
	ctx := context.Background()
	cl := testfixtures.Cluster(t)
	stores := &storage.MemoryFactory{}
	require.NoError(t, storage.PutBytes(ctx, stores.Bucket(testfixtures.BackupBkt), "backups/platform-mesh-system/daily.tar", []byte("old")))

	b := newBackup("daily")
	res, _ := testfixtures.Run(ctx, t, newSubs(cl, stores), b)
	assert.True(t, res.IsStop())
	assert.Equal(t, "a backup already exists at backups/platform-mesh-system/daily.tar", b.Status.Message)
}

func TestTaskName(t *testing.T) {
	assert.Equal(t, "daily-etcd-kcp", subbackup.TaskName("daily", "etcd-kcp"))
	long := subbackup.TaskName("a-very-long-backup-name-that-goes-on-and-on-and-on-and-on", "etcd-kcp-triton")
	assert.LessOrEqual(t, len(long), 63)
}
