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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"
	"go.platform-mesh.io/backup-operator/pkg/restore"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTopologySub() *restore.TopologyValidateSubroutine {
	return restore.NewTopologyValidateSubroutine(unitTestNamespace)
}

// fakeRestoreStrict returns a PlatformRestore with TopologyValidation=Strict.
func fakeRestoreStrict(name, backupID string) *pmbackupv1alpha1.PlatformRestore {
	rst := fakeRestore(name, backupID)
	rst.Spec.TopologyValidation = pmbackupv1alpha1.TopologyValidationStrict
	return rst
}

// fakeRestoreNoValidation returns a PlatformRestore with no TopologyValidation set.
func fakeRestoreNoValidation(name, backupID string) *pmbackupv1alpha1.PlatformRestore {
	rst := fakeRestore(name, backupID)
	rst.Spec.TopologyValidation = ""
	return rst
}

// TestTopologyValidate_NonStrict verifies the subroutine is a no-op when
// TopologyValidation is not Strict — it must not block or error.
func TestTopologyValidate_NonStrict(t *testing.T) {
	bkp := fakeBackupWithShards("bkp", map[string]string{"shard-a": "rev-1"})
	rst := fakeRestoreNoValidation("rst", "bkp")

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp).
		Build()

	result, err := newTopologySub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsSkip(), "non-strict mode must skip (not block) the chain")
}

// TestTopologyValidate_Idempotent verifies the subroutine skips work when the
// condition is already True (set in a prior reconcile).
func TestTopologyValidate_Idempotent(t *testing.T) {
	bkp := fakeBackupWithShards("bkp", map[string]string{"shard-a": "rev-1"})
	rst := fakeRestoreStrict("rst", "bkp")
	rst.Status.Conditions = []metav1.Condition{{
		Type:               restore.ConditionTopologyValidated,
		Status:             metav1.ConditionTrue,
		Reason:             "Complete",
		LastTransitionTime: metav1.Now(),
	}}

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp).
		Build()

	result, err := newTopologySub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsContinue())
}

// TestTopologyValidate_SourceBackupNotFound verifies StopWithRequeue when the
// source backup doesn't exist yet.
func TestTopologyValidate_SourceBackupNotFound(t *testing.T) {
	rst := fakeRestoreStrict("rst", "nonexistent-backup")

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		Build()

	result, err := newTopologySub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err, "missing backup must requeue, not error")
	assert.False(t, result.IsContinue())
	assert.True(t, result.IsStopWithRequeue())
}

// TestTopologyValidate_NoEtcdArtefacts verifies the subroutine passes when the
// backup has no etcd artefacts — there is nothing to validate against.
func TestTopologyValidate_NoEtcdArtefacts(t *testing.T) {
	bkp := fakeBackup("bkp") // no artefacts set
	rst := fakeRestoreStrict("rst", "bkp")

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp).
		Build()

	result, err := newTopologySub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err)
	assert.True(t, result.IsSkip(), "no-artefacts path must skip, not pass as Complete")
}

// TestTopologyValidate_ShardSetsMatch verifies that when the live shard set
// exactly matches the backup artefact, the subroutine returns OK.
func TestTopologyValidate_ShardSetsMatch(t *testing.T) {
	bkp := fakeBackupWithShards("bkp", map[string]string{
		"shard-a": "rev-1",
		"shard-b": "rev-2",
	})
	rst := fakeRestoreStrict("rst", "bkp")
	shardA := fakeEtcdShard("shard-a")
	shardB := fakeEtcdShard("shard-b")

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shardA, shardB).
		Build()

	result, err := newTopologySub().Process(ctxWithClient(cl), rst)
	require.NoError(t, err, "matching shard sets must pass validation")
	// StopWithRequeue(0) is returned on success so the lifecycle commits
	// TopologyValidated=True before the restore subroutine runs.
	assert.True(t, result.IsStopWithRequeue(), "matching topology must stop-and-requeue to commit status before restore")
}

// TestTopologyValidate_ShardMissingFromCluster verifies that a shard present in
// the backup but absent from the live cluster produces a validation error.
func TestTopologyValidate_ShardMissingFromCluster(t *testing.T) {
	bkp := fakeBackupWithShards("bkp", map[string]string{
		"shard-a": "rev-1",
		"shard-b": "rev-2", // shard-b does NOT exist in the cluster
	})
	rst := fakeRestoreStrict("rst", "bkp")
	shardA := fakeEtcdShard("shard-a")

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shardA).
		Build()

	_, err := newTopologySub().Process(ctxWithClient(cl), rst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shard-b")
	assert.Contains(t, err.Error(), "missing from live cluster")
}

// TestTopologyValidate_ExtraShardInCluster verifies that a live shard not present
// in the backup artefact produces a validation error.
func TestTopologyValidate_ExtraShardInCluster(t *testing.T) {
	bkp := fakeBackupWithShards("bkp", map[string]string{
		"shard-a": "rev-1",
		// shard-extra exists in the cluster but was not in the backup
	})
	rst := fakeRestoreStrict("rst", "bkp")
	shardA := fakeEtcdShard("shard-a")
	shardExtra := fakeEtcdShard("shard-extra")

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shardA, shardExtra).
		Build()

	_, err := newTopologySub().Process(ctxWithClient(cl), rst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shard-extra")
	assert.Contains(t, err.Error(), "absent from backup")
}

// TestTopologyValidate_MultipleErrors verifies that all mismatches are reported,
// not just the first.
func TestTopologyValidate_MultipleErrors(t *testing.T) {
	bkp := fakeBackupWithShards("bkp", map[string]string{
		"shard-a":       "rev-1",
		"shard-missing": "rev-2", // in backup, not in cluster
	})
	rst := fakeRestoreStrict("rst", "bkp")
	shardA := fakeEtcdShard("shard-a")
	shardExtra := fakeEtcdShard("shard-extra") // in cluster, not in backup

	cl := fake.NewClientBuilder().
		WithScheme(newFakeScheme(t)).
		WithObjects(bkp, shardA, shardExtra).
		Build()

	_, err := newTopologySub().Process(ctxWithClient(cl), rst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shard-missing")
	assert.Contains(t, err.Error(), "shard-extra")
}

// TestTopologyValidate_WrongObjectType verifies the subroutine errors on
// non-PlatformRestore objects.
func TestTopologyValidate_WrongObjectType(t *testing.T) {
	bkp := fakeBackup("bkp")
	cl := fake.NewClientBuilder().WithScheme(newFakeScheme(t)).WithObjects(bkp).Build()

	_, err := newTopologySub().Process(ctxWithClient(cl), bkp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected object type")
}
