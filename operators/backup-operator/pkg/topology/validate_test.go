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

package topology_test

import (
	"context"
	"testing"

	druidv1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/backup"
	"go.platform-mesh.io/backup-operator/pkg/topology"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/golang-commons/logger/testlogger"
	"go.platform-mesh.io/subroutines"

	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testNamespace = "default"

func newFakeScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, pmbackupv1alpha1.AddToScheme(s))
	require.NoError(t, druidv1alpha1.AddToScheme(s))
	require.NoError(t, coordinationv1.AddToScheme(s))
	return s
}

func ctxWithClient(cl ctrlruntimeclient.Client) context.Context {
	ctx := subroutines.WithClient(context.Background(), cl)
	return logger.SetLoggerInContext(ctx, testlogger.New().Logger)
}

func newTopologySub() *topology.ValidateSubroutine {
	return topology.NewValidateSubroutine(testNamespace)
}

func fakeEtcdShard(name string) *druidv1alpha1.Etcd {
	localProvider := druidv1alpha1.StorageProvider("Local")
	return &druidv1alpha1.Etcd{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels:    map[string]string{backup.LabelKeyComponent: backup.LabelComponentKCPShard},
		},
		Spec: druidv1alpha1.EtcdSpec{
			Replicas: 1,
			Backup: druidv1alpha1.BackupSpec{
				Store: &druidv1alpha1.StoreSpec{
					Prefix:   name,
					Provider: &localProvider,
				},
			},
		},
	}
}

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
		},
	}
}

func fakeBackupWithShards(name string, shards map[string]string) *pmbackupv1alpha1.PlatformBackup {
	artefacts := make(map[string]pmbackupv1alpha1.EtcdShardArtefact, len(shards))
	for k, v := range shards {
		artefacts[k] = pmbackupv1alpha1.EtcdShardArtefact{SnapshotKey: v, SnapshotTime: metav1.Now()}
	}
	b := fakeBackup(name)
	b.Status.Artefacts.Etcd = &pmbackupv1alpha1.EtcdArtefact{Shards: artefacts}
	return b
}

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
		},
	}
}

func fakeRestoreStrict(name, backupID string) *pmbackupv1alpha1.PlatformRestore {
	rst := fakeRestore(name, backupID)
	rst.Spec.TopologyValidation = pmbackupv1alpha1.TopologyValidationStrict
	return rst
}

func fakeRestoreNoValidation(name, backupID string) *pmbackupv1alpha1.PlatformRestore {
	rst := fakeRestore(name, backupID)
	rst.Spec.TopologyValidation = ""
	return rst
}

func TestTopologyValidate_NonStrict(t *testing.T) {
	bkp := fakeBackupWithShards("bkp", map[string]string{"shard-a": "rev-1"})
	rst := fakeRestoreNoValidation("rst", "bkp")
	cl := fake.NewClientBuilder().WithScheme(newFakeScheme(t)).WithObjects(bkp).Build()

	result, err := newTopologySub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsSkip(), "non-strict mode must skip (not block) the chain")
}

func TestTopologyValidate_Idempotent(t *testing.T) {
	bkp := fakeBackupWithShards("bkp", map[string]string{"shard-a": "rev-1"})
	rst := fakeRestoreStrict("rst", "bkp")
	rst.Status.Conditions = []metav1.Condition{{
		Type:               topology.ConditionTopologyValidated,
		Status:             metav1.ConditionTrue,
		Reason:             "Complete",
		LastTransitionTime: metav1.Now(),
	}}
	cl := fake.NewClientBuilder().WithScheme(newFakeScheme(t)).WithObjects(bkp).Build()

	result, err := newTopologySub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
}

func TestTopologyValidate_SourceBackupNotFound(t *testing.T) {
	rst := fakeRestoreStrict("rst", "nonexistent-backup")
	cl := fake.NewClientBuilder().WithScheme(newFakeScheme(t)).Build()

	result, err := newTopologySub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err, "missing backup must requeue, not error")
	assert.True(t, result.IsStopWithRequeue())
}

func TestTopologyValidate_NoEtcdArtefacts(t *testing.T) {
	bkp := fakeBackup("bkp")
	rst := fakeRestoreStrict("rst", "bkp")
	cl := fake.NewClientBuilder().WithScheme(newFakeScheme(t)).WithObjects(bkp).Build()

	result, err := newTopologySub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsSkip(), "no-artefacts path must skip, not pass as Complete")
}

func TestTopologyValidate_ShardSetsMatch(t *testing.T) {
	bkp := fakeBackupWithShards("bkp", map[string]string{"shard-a": "rev-1", "shard-b": "rev-2"})
	rst := fakeRestoreStrict("rst", "bkp")
	cl := fake.NewClientBuilder().WithScheme(newFakeScheme(t)).
		WithObjects(bkp, fakeEtcdShard("shard-a"), fakeEtcdShard("shard-b")).Build()

	result, err := newTopologySub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err, "matching shard sets must pass validation")
	assert.True(t, result.IsContinue())
}

func TestTopologyValidate_ShardMissingFromCluster(t *testing.T) {
	bkp := fakeBackupWithShards("bkp", map[string]string{"shard-a": "rev-1", "shard-b": "rev-2"})
	rst := fakeRestoreStrict("rst", "bkp")
	cl := fake.NewClientBuilder().WithScheme(newFakeScheme(t)).
		WithObjects(bkp, fakeEtcdShard("shard-a")).Build()

	result, err := newTopologySub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsStopWithRequeue())
	assert.Contains(t, result.Message(), "shard-b")
	assert.Contains(t, result.Message(), "missing from live cluster")
}

func TestTopologyValidate_ExtraShardInCluster(t *testing.T) {
	bkp := fakeBackupWithShards("bkp", map[string]string{"shard-a": "rev-1"})
	rst := fakeRestoreStrict("rst", "bkp")
	cl := fake.NewClientBuilder().WithScheme(newFakeScheme(t)).
		WithObjects(bkp, fakeEtcdShard("shard-a"), fakeEtcdShard("shard-extra")).Build()

	result, err := newTopologySub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsStopWithRequeue())
	assert.Contains(t, result.Message(), "shard-extra")
	assert.Contains(t, result.Message(), "absent from backup")
}

func TestTopologyValidate_MultipleErrors(t *testing.T) {
	bkp := fakeBackupWithShards("bkp", map[string]string{"shard-a": "rev-1", "shard-missing": "rev-2"})
	rst := fakeRestoreStrict("rst", "bkp")
	cl := fake.NewClientBuilder().WithScheme(newFakeScheme(t)).
		WithObjects(bkp, fakeEtcdShard("shard-a"), fakeEtcdShard("shard-extra")).Build()

	result, err := newTopologySub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsStopWithRequeue())
	assert.Contains(t, result.Message(), "shard-missing")
	assert.Contains(t, result.Message(), "shard-extra")
}

func TestTopologyValidate_WrongObjectType(t *testing.T) {
	bkp := fakeBackup("bkp")
	cl := fake.NewClientBuilder().WithScheme(newFakeScheme(t)).WithObjects(bkp).Build()

	_, err := newTopologySub().Process(ctxWithClient(cl), bkp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected object type")
}
