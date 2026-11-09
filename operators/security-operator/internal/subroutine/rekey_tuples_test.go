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

package subroutine_test

import (
	"context"
	"fmt"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/security-operator/internal/metrics"
	"go.platform-mesh.io/security-operator/internal/subroutine"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"
	"go.platform-mesh.io/security-operator/pkg/config"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

const (
	rekeyTestOrg   = "myorg"
	rekeyTestOldID = "old-id"
	rekeyTestNewID = "new-id"
)

var (
	lcGroupResource      = schema.GroupResource{Group: "core.kcp.io", Resource: "logicalclusters"}
	accountGroupResource = schema.GroupResource{Group: "core.platform-mesh.io", Resource: "accounts"}
)

type rekeyTestMocks struct {
	manager       *mocks.MockManager
	cluster       *mocks.MockCluster
	clusterClient *mocks.MockClient
	kcpHelper     *mocks.MockKCPClientGetter
	deadClient    *mocks.MockClient
	storeIDGetter *mocks.MockStoreIDGetter
	fgaClient     *mocks.MockOpenFGAServiceClient
	recorder      *events.FakeRecorder
}

func newRekeyTestMocks(t *testing.T) *rekeyTestMocks {
	t.Helper()
	return &rekeyTestMocks{
		manager:       mocks.NewMockManager(t),
		cluster:       mocks.NewMockCluster(t),
		clusterClient: mocks.NewMockClient(t),
		kcpHelper:     mocks.NewMockKCPClientGetter(t),
		deadClient:    mocks.NewMockClient(t),
		storeIDGetter: mocks.NewMockStoreIDGetter(t),
		fgaClient:     mocks.NewMockOpenFGAServiceClient(t),
		recorder:      events.NewFakeRecorder(50),
	}
}

func (m *rekeyTestMocks) newSubroutine(enabled bool) *subroutine.RekeyTuplesSubroutine {
	cfg := config.Config{}
	cfg.RekeyOrphanedTuplesEnabled = enabled
	cfg.FGA.ObjectType = "acct"
	return subroutine.NewRekeyTuplesSubroutine(cfg, m.manager, m.fgaClient, m.storeIDGetter, m.kcpHelper)
}

// expectOrgContext wires up ClusterFromContext + the AccountInfo lookup that
// yields the org's current cluster id, and the store id resolution.
func (m *rekeyTestMocks) expectOrgContext() {
	m.manager.EXPECT().ClusterFromContext(mock.Anything).Return(m.cluster, nil)
	m.cluster.EXPECT().GetClient().Return(m.clusterClient)
	m.clusterClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			if ai, ok := o.(*pmcorev1alpha1.AccountInfo); ok {
				ai.Spec.Account.Name = rekeyTestOrg
				ai.Spec.Account.OriginClusterId = rekeyTestNewID
			}
			return nil
		})
	m.storeIDGetter.EXPECT().Get(mock.Anything, rekeyTestOrg).Return("store-id", nil)
}

// expectRead makes the store scan return the given tuples in a single page.
func (m *rekeyTestMocks) expectRead(tuples ...pmcorev1alpha1.Tuple) {
	fgaTuples := make([]*openfgav1.Tuple, 0, len(tuples))
	for _, t := range tuples {
		fgaTuples = append(fgaTuples, &openfgav1.Tuple{Key: &openfgav1.TupleKey{
			Object:   t.Object,
			Relation: t.Relation,
			User:     t.User,
		}})
	}
	m.fgaClient.EXPECT().Read(mock.Anything, mock.Anything).
		Return(&openfgav1.ReadResponse{Tuples: fgaTuples}, nil).Once()
}

// expectNoSameNameChild makes the same-name-descendant guard find no Account
// named like the org in the org workspace.
func (m *rekeyTestMocks) expectNoSameNameChild() {
	m.clusterClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: rekeyTestOrg}, mock.Anything).
		Return(apierrors.NewNotFound(accountGroupResource, rekeyTestOrg))
}

// expectLiveness makes the liveness check for the old cluster id answer with
// the given error (nil = the cluster still exists).
func (m *rekeyTestMocks) expectLiveness(getErr error) {
	m.kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, rekeyTestOldID).Return(m.deadClient, nil)
	m.deadClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "cluster"}, mock.Anything).Return(getErr)
}

func staleCreatorTuple() pmcorev1alpha1.Tuple {
	return pmcorev1alpha1.Tuple{
		User:     "user:alice",
		Relation: "assignee",
		Object:   "role:acct/" + rekeyTestOldID + "/" + rekeyTestOrg + "/owner",
	}
}

func staleOwnerGroupTuple() pmcorev1alpha1.Tuple {
	return pmcorev1alpha1.Tuple{
		User:     "role:acct/" + rekeyTestOldID + "/" + rekeyTestOrg + "/owner#assignee",
		Relation: "owner",
		Object:   "acct:" + rekeyTestOldID + "/" + rekeyTestOrg,
	}
}

func currentCreatorTuple() pmcorev1alpha1.Tuple {
	return pmcorev1alpha1.Tuple{
		User:     "user:bob",
		Relation: "assignee",
		Object:   "role:acct/" + rekeyTestNewID + "/" + rekeyTestOrg + "/owner",
	}
}

func TestRekeyTuplesSubroutine_GetName(t *testing.T) {
	sub := subroutine.NewRekeyTuplesSubroutine(config.Config{}, nil, nil, nil, nil)
	assert.Equal(t, "RekeyTuplesSubroutine", sub.GetName())
}

func TestRekeyTuplesSubroutine_FlagOff_NoAction(t *testing.T) {
	// All mocks are created without any expectations: any call to any of them
	// fails the test. With the flag off the subroutine must be a no-op.
	m := newRekeyTestMocks(t)
	sub := m.newSubroutine(false)

	for _, fn := range []func(context.Context, ctrlruntimeclient.Object) (subroutines.Result, error){sub.Process, sub.Initialize} {
		result, err := fn(context.Background(), &kcpcorev1alpha1.LogicalCluster{})
		require.NoError(t, err)
		assert.Equal(t, subroutines.OK(), result)
	}
}

func TestRekeyTuplesSubroutine_RekeysStaleGroupWithDeadCluster(t *testing.T) {
	m := newRekeyTestMocks(t)
	m.expectOrgContext()
	m.expectRead(staleCreatorTuple(), staleOwnerGroupTuple(), currentCreatorTuple())
	m.expectNoSameNameChild()
	m.expectLiveness(apierrors.NewNotFound(lcGroupResource, "cluster"))
	m.cluster.EXPECT().GetEventRecorder(mock.Anything).Return(m.recorder)

	var ops []string
	var written []*openfgav1.TupleKey
	var deletedKeys []*openfgav1.TupleKeyWithoutCondition
	m.fgaClient.EXPECT().Write(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, req *openfgav1.WriteRequest, opts ...grpc.CallOption) (*openfgav1.WriteResponse, error) {
			if req.Writes != nil {
				ops = append(ops, "write")
				written = append(written, req.Writes.TupleKeys...)
			}
			if req.Deletes != nil {
				ops = append(ops, "delete")
				deletedKeys = append(deletedKeys, req.Deletes.TupleKeys...)
			}
			return &openfgav1.WriteResponse{}, nil
		}).Times(2)

	sub := m.newSubroutine(true)
	result, err := sub.Initialize(context.Background(), &kcpcorev1alpha1.LogicalCluster{})
	require.NoError(t, err)
	assert.Equal(t, subroutines.OK(), result)

	// Write happened strictly before delete.
	require.Equal(t, []string{"write", "delete"}, ops)

	// The written tuples are the stale ones re-keyed to the new cluster id.
	require.Len(t, written, 2)
	assert.Equal(t, "role:acct/new-id/myorg/owner", written[0].Object)
	assert.Equal(t, "user:alice", written[0].User)
	assert.Equal(t, "acct:new-id/myorg", written[1].Object)
	assert.Equal(t, "role:acct/new-id/myorg/owner#assignee", written[1].User)

	// The deleted tuples are exactly the stale originals; the tuple already on
	// the current cluster id is untouched.
	require.Len(t, deletedKeys, 2)
	assert.Equal(t, "role:acct/old-id/myorg/owner", deletedKeys[0].Object)
	assert.Equal(t, "acct:old-id/myorg", deletedKeys[1].Object)

	// An event with the tuple count was emitted.
	select {
	case ev := <-m.recorder.Events:
		assert.Contains(t, ev, "OrphanedTuplesRekeyed")
		assert.Contains(t, ev, "2 FGA tuples")
	default:
		t.Fatal("expected an OrphanedTuplesRekeyed event")
	}
}

func TestRekeyTuplesSubroutine_FailsClosedWhenLivenessCheckErrors(t *testing.T) {
	m := newRekeyTestMocks(t)
	m.expectOrgContext()
	m.expectRead(staleCreatorTuple(), staleOwnerGroupTuple())
	m.expectNoSameNameChild()
	// Liveness check fails with an error that is neither NotFound nor
	// Forbidden: no tuple may be touched. Note: no Write expectation is set,
	// so any Write call fails the test.
	m.expectLiveness(apierrors.NewInternalError(assert.AnError))

	sub := m.newSubroutine(true)
	result, err := sub.Process(context.Background(), &kcpcorev1alpha1.LogicalCluster{})
	require.NoError(t, err)
	assert.True(t, result.IsStopWithRequeue(), "must requeue to retry the liveness check")
}

func TestRekeyTuplesSubroutine_SkipsGroupWhenClusterStillAlive(t *testing.T) {
	m := newRekeyTestMocks(t)
	m.expectOrgContext()
	m.expectRead(staleCreatorTuple())
	m.expectNoSameNameChild()
	// The referenced cluster still exists: never guess, never touch tuples.
	m.expectLiveness(nil)
	m.cluster.EXPECT().GetEventRecorder(mock.Anything).Return(m.recorder)

	sub := m.newSubroutine(true)
	result, err := sub.Process(context.Background(), &kcpcorev1alpha1.LogicalCluster{})
	require.NoError(t, err)
	assert.Equal(t, subroutines.OK(), result)

	select {
	case ev := <-m.recorder.Events:
		assert.Contains(t, ev, "OrphanedTupleRekeySkipped")
	default:
		t.Fatal("expected an OrphanedTupleRekeySkipped event")
	}
}

func TestRekeyTuplesSubroutine_TreatsForbiddenAsDead(t *testing.T) {
	m := newRekeyTestMocks(t)
	m.expectOrgContext()
	m.expectRead(staleCreatorTuple())
	m.expectNoSameNameChild()
	m.expectLiveness(apierrors.NewForbidden(lcGroupResource, "cluster", assert.AnError))
	m.cluster.EXPECT().GetEventRecorder(mock.Anything).Return(m.recorder)

	m.fgaClient.EXPECT().Write(mock.Anything, mock.Anything).Return(&openfgav1.WriteResponse{}, nil).Times(2)

	sub := m.newSubroutine(true)
	result, err := sub.Process(context.Background(), &kcpcorev1alpha1.LogicalCluster{})
	require.NoError(t, err)
	assert.Equal(t, subroutines.OK(), result)
}

func TestRekeyTuplesSubroutine_NoDeleteWhenWriteFails(t *testing.T) {
	m := newRekeyTestMocks(t)
	m.expectOrgContext()
	m.expectRead(staleCreatorTuple(), staleOwnerGroupTuple())
	m.expectNoSameNameChild()
	m.expectLiveness(apierrors.NewNotFound(lcGroupResource, "cluster"))

	// The write of the re-keyed copies fails: the originals must NOT be
	// deleted (only one Write call in total).
	m.fgaClient.EXPECT().Write(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, req *openfgav1.WriteRequest, opts ...grpc.CallOption) (*openfgav1.WriteResponse, error) {
			require.NotNil(t, req.Writes, "the only Write call must be the write of re-keyed copies, not a delete")
			return nil, assert.AnError
		}).Once()

	sub := m.newSubroutine(true)
	_, err := sub.Process(context.Background(), &kcpcorev1alpha1.LogicalCluster{})
	require.Error(t, err)
}

func TestRekeyTuplesSubroutine_ChunksGroupsOver100Tuples(t *testing.T) {
	m := newRekeyTestMocks(t)
	m.expectOrgContext()

	stale := make([]pmcorev1alpha1.Tuple, 0, 250)
	for i := range 250 {
		stale = append(stale, pmcorev1alpha1.Tuple{
			User:     fmt.Sprintf("user:member-%d", i),
			Relation: "assignee",
			Object:   "role:acct/" + rekeyTestOldID + "/" + rekeyTestOrg + "/member",
		})
	}
	m.expectRead(stale...)
	m.expectNoSameNameChild()
	m.expectLiveness(apierrors.NewNotFound(lcGroupResource, "cluster"))
	m.cluster.EXPECT().GetEventRecorder(mock.Anything).Return(m.recorder)

	var ops []string
	m.fgaClient.EXPECT().Write(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, req *openfgav1.WriteRequest, opts ...grpc.CallOption) (*openfgav1.WriteResponse, error) {
			switch {
			case req.Writes != nil:
				require.LessOrEqual(t, len(req.Writes.TupleKeys), 100)
				ops = append(ops, fmt.Sprintf("write-%d", len(req.Writes.TupleKeys)))
			case req.Deletes != nil:
				require.LessOrEqual(t, len(req.Deletes.TupleKeys), 100)
				ops = append(ops, fmt.Sprintf("delete-%d", len(req.Deletes.TupleKeys)))
			}
			return &openfgav1.WriteResponse{}, nil
		}).Times(6)

	sub := m.newSubroutine(true)
	result, err := sub.Process(context.Background(), &kcpcorev1alpha1.LogicalCluster{})
	require.NoError(t, err)
	assert.Equal(t, subroutines.OK(), result)

	// All writes happen before any delete, and every request is capped at 100.
	assert.Equal(t, []string{"write-100", "write-100", "write-50", "delete-100", "delete-100", "delete-50"}, ops)
}

func TestRekeyTuplesSubroutine_SkipsAllWhenSameNameDescendantAccountExists(t *testing.T) {
	// A sub-account may legally carry the org's own name (Account names are
	// only unique per workspace). Its tuples are keyed by its parent
	// workspace's cluster id, which after a tree re-creation is just as dead
	// as the org's old origin cluster id — so stale tuples cannot be
	// attributed to the org. Re-keying them would merge the sub-account's role
	// assignees into the org's roles: the subroutine must not touch anything.
	m := newRekeyTestMocks(t)
	m.expectOrgContext()
	m.expectRead(staleCreatorTuple(), staleOwnerGroupTuple())
	// The guard finds an Account named like the org in the org workspace.
	m.clusterClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: rekeyTestOrg}, mock.Anything).Return(nil)
	m.cluster.EXPECT().GetEventRecorder(mock.Anything).Return(m.recorder)
	// Note: no liveness and no Write expectations are set, so any FGA write or
	// kcp client call fails the test.

	before := testutil.ToFloat64(metrics.RekeyedTuples.WithLabelValues("skipped_ambiguous"))

	sub := m.newSubroutine(true)
	result, err := sub.Process(context.Background(), &kcpcorev1alpha1.LogicalCluster{})
	require.NoError(t, err)
	assert.Equal(t, subroutines.OK(), result)

	assert.Equal(t, before+2, testutil.ToFloat64(metrics.RekeyedTuples.WithLabelValues("skipped_ambiguous")))

	select {
	case ev := <-m.recorder.Events:
		assert.Contains(t, ev, "OrphanedTupleRekeySkipped")
		assert.Contains(t, ev, rekeyTestOrg)
	default:
		t.Fatal("expected an OrphanedTupleRekeySkipped event")
	}
}

func TestRekeyTuplesSubroutine_FailsClosedWhenDescendantCheckErrors(t *testing.T) {
	// The same-name-descendant guard cannot be evaluated: no tuple may be
	// touched (no Write expectation is set, so any Write call fails the test).
	m := newRekeyTestMocks(t)
	m.expectOrgContext()
	m.expectRead(staleCreatorTuple())
	m.clusterClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: rekeyTestOrg}, mock.Anything).
		Return(apierrors.NewInternalError(assert.AnError))

	sub := m.newSubroutine(true)
	_, err := sub.Process(context.Background(), &kcpcorev1alpha1.LogicalCluster{})
	require.Error(t, err)
}

func TestRekeyTuplesSubroutine_IgnoresForeignObjectTypeKeys(t *testing.T) {
	// The org store also holds keys of other object types in the same raw
	// shape — e.g. apis_kcp_io_apiexport user keys written for
	// APIExportPolicies. Even when such a key carries the org's name under an
	// old cluster id it must be invisible to the re-key pass.
	m := newRekeyTestMocks(t)
	m.expectOrgContext()
	apiExportTuple := pmcorev1alpha1.Tuple{
		User:     "apis_kcp_io_apiexport:" + rekeyTestOldID + "/" + rekeyTestOrg,
		Relation: "member",
		Object:   "acct:" + rekeyTestNewID + "/" + rekeyTestOrg,
	}
	m.expectRead(apiExportTuple, staleCreatorTuple())
	m.expectNoSameNameChild()
	m.expectLiveness(apierrors.NewNotFound(lcGroupResource, "cluster"))
	m.cluster.EXPECT().GetEventRecorder(mock.Anything).Return(m.recorder)

	var written []*openfgav1.TupleKey
	var deletedKeys []*openfgav1.TupleKeyWithoutCondition
	m.fgaClient.EXPECT().Write(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, req *openfgav1.WriteRequest, opts ...grpc.CallOption) (*openfgav1.WriteResponse, error) {
			if req.Writes != nil {
				written = append(written, req.Writes.TupleKeys...)
			}
			if req.Deletes != nil {
				deletedKeys = append(deletedKeys, req.Deletes.TupleKeys...)
			}
			return &openfgav1.WriteResponse{}, nil
		}).Times(2)

	sub := m.newSubroutine(true)
	result, err := sub.Process(context.Background(), &kcpcorev1alpha1.LogicalCluster{})
	require.NoError(t, err)
	assert.Equal(t, subroutines.OK(), result)

	// Only the genuine account tuple was re-keyed and deleted; the apiexport
	// key never appears in any request.
	require.Len(t, written, 1)
	assert.Equal(t, "role:acct/new-id/myorg/owner", written[0].Object)
	assert.Equal(t, "user:alice", written[0].User)
	require.Len(t, deletedKeys, 1)
	assert.Equal(t, "role:acct/old-id/myorg/owner", deletedKeys[0].Object)
}

func TestRekeyTuplesSubroutine_SecondPassIsIdempotent(t *testing.T) {
	// After a successful re-key the store only contains tuples on the current
	// cluster id: the next reconcile must not issue any Write.
	m := newRekeyTestMocks(t)
	m.expectOrgContext()
	m.expectRead(
		currentCreatorTuple(),
		pmcorev1alpha1.Tuple{
			User:     "role:acct/" + rekeyTestNewID + "/" + rekeyTestOrg + "/owner#assignee",
			Relation: "owner",
			Object:   "acct:" + rekeyTestNewID + "/" + rekeyTestOrg,
		},
	)

	sub := m.newSubroutine(true)
	result, err := sub.Process(context.Background(), &kcpcorev1alpha1.LogicalCluster{})
	require.NoError(t, err)
	assert.Equal(t, subroutines.OK(), result)
}
