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

package restore_test

import (
	"context"
	"testing"
	"time"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	backupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/restore"
	"go.platform-mesh.io/subroutines"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func injectClient(ctx context.Context, cl client.Client) context.Context {
	return subroutines.WithClient(ctx, cl)
}

func makeBackupWithShards(t *testing.T, cl client.Client, backupName string, shards map[string]string) *backupv1alpha1.PlatformBackup {
	t.Helper()
	bkp := &backupv1alpha1.PlatformBackup{
		ObjectMeta: metav1.ObjectMeta{Name: backupName},
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
	require.NoError(t, cl.Create(context.Background(), bkp))

	shardArtefacts := make(map[string]backupv1alpha1.EtcdShardArtefact, len(shards))
	for name, key := range shards {
		shardArtefacts[name] = backupv1alpha1.EtcdShardArtefact{
			SnapshotKey:  key,
			SnapshotTime: metav1.Now(),
		}
	}
	bkp.Status = backupv1alpha1.PlatformBackupStatus{
		Artefacts: backupv1alpha1.ArtefactsStatus{
			Etcd: &backupv1alpha1.EtcdArtefact{Shards: shardArtefacts},
		},
	}
	require.NoError(t, cl.Status().Update(context.Background(), bkp), "status update for backup %s", backupName)
	return bkp
}

func makeEtcdShardForRestore(t *testing.T, cl client.Client, name string) {
	t.Helper()
	localProvider := druidv1alpha1.StorageProvider("Local")
	etcd := &druidv1alpha1.Etcd{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
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
	require.NoError(t, cl.Create(context.Background(), etcd))
}

func makePlatformRestore(t *testing.T, cl client.Client, name, backupID string) *backupv1alpha1.PlatformRestore {
	t.Helper()
	rst := &backupv1alpha1.PlatformRestore{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: backupv1alpha1.PlatformRestoreSpec{
			Source: backupv1alpha1.RestoreSourceSpec{
				BackupID: backupID,
				Storage: backupv1alpha1.StorageSpec{
					S3: backupv1alpha1.S3StorageSpec{
						Endpoint:       "http://minio:9000",
						Bucket:         "backups",
						CredentialsRef: corev1.LocalObjectReference{Name: "s3-credentials"},
					},
				},
			},
			TopologyValidation: backupv1alpha1.TopologyValidationStrict,
		},
	}
	require.NoError(t, cl.Create(context.Background(), rst))
	return rst
}

func TestRestore_SingleShard_Recreate(t *testing.T) {
	cl, _, stop := setupEnvtest(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	runReadySimulator(ctx, cl)
	runFinalizerCleaner(ctx, cl)

	makeEtcdShardForRestore(t, cl, "shard-restore-single")
	bkp := makeBackupWithShards(t, cl, "backup-for-restore-single", map[string]string{
		"shard-restore-single": "rev-42",
	})

	var readBack backupv1alpha1.PlatformBackup
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: bkp.Name}, &readBack))
	require.NotNil(t, readBack.Status.Artefacts.Etcd, "backup status not persisted before subroutine")

	rst := makePlatformRestore(t, cl, "restore-single", bkp.Name)

	sub := restore.NewEtcdRestoreSubroutine(testNamespace).
		WithPollIntervals(100*time.Millisecond, 10*time.Second, 100*time.Millisecond, 20*time.Second)

	result, err := sub.Process(injectClient(ctx, cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())

	var recreated druidv1alpha1.Etcd
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: "shard-restore-single", Namespace: testNamespace}, &recreated))
	assert.Equal(t, "rev-42", recreated.Annotations[restore.AnnotationKeyRestoredFromSnapshot])
}

func TestRestore_MultiShard_ConcurrentRecreate(t *testing.T) {
	cl, _, stop := setupEnvtest(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	runReadySimulator(ctx, cl)
	runFinalizerCleaner(ctx, cl)

	makeEtcdShardForRestore(t, cl, "shard-a-multi")
	makeEtcdShardForRestore(t, cl, "shard-b-multi")
	bkp := makeBackupWithShards(t, cl, "backup-multi-restore", map[string]string{
		"shard-a-multi": "rev-10",
		"shard-b-multi": "rev-20",
	})
	rst := makePlatformRestore(t, cl, "restore-multi", bkp.Name)

	sub := restore.NewEtcdRestoreSubroutine(testNamespace).
		WithPollIntervals(100*time.Millisecond, 10*time.Second, 100*time.Millisecond, 20*time.Second)

	result, err := sub.Process(injectClient(ctx, cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())

	for _, name := range []string{"shard-a-multi", "shard-b-multi"} {
		var recreated druidv1alpha1.Etcd
		require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, &recreated))
		assert.NotEmpty(t, recreated.Annotations[restore.AnnotationKeyRestoredFromSnapshot])
	}
}

func TestRestore_MissingBackup_StopsWithRequeue(t *testing.T) {
	cl, _, stop := setupEnvtest(t)
	defer stop()

	ctx := context.Background()
	rst := makePlatformRestore(t, cl, "restore-missing", "nonexistent-backup")

	sub := restore.NewEtcdRestoreSubroutine(testNamespace).
		WithPollIntervals(100*time.Millisecond, 5*time.Second, 100*time.Millisecond, 5*time.Second)

	result, err := sub.Process(injectClient(ctx, cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsStopWithRequeue(), "expected StopWithRequeue when source backup is not found")
}

func TestRestore_MissingEtcdArtefacts_Skips(t *testing.T) {
	cl, _, stop := setupEnvtest(t)
	defer stop()

	ctx := context.Background()

	bkp := &backupv1alpha1.PlatformBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "backup-no-artefacts"},
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
	require.NoError(t, cl.Create(ctx, bkp))

	rst := makePlatformRestore(t, cl, "restore-no-artefacts", bkp.Name)

	sub := restore.NewEtcdRestoreSubroutine(testNamespace).
		WithPollIntervals(100*time.Millisecond, 5*time.Second, 100*time.Millisecond, 5*time.Second)

	result, err := sub.Process(injectClient(ctx, cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
}
