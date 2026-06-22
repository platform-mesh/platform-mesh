package fga

import (
	"context"
	"errors"
	"slices"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger/testlogger"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"
	"google.golang.org/grpc"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTupleManager_Apply(t *testing.T) {
	t.Run("returns nil for empty tuples", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		log := testlogger.New()
		mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)

		err := mgr.Apply(context.Background(), nil)
		assert.NoError(t, err)

		err = mgr.Apply(context.Background(), []corev1alpha1.Tuple{})
		assert.NoError(t, err)
	})

	t.Run("writes tuples successfully", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		client.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
			return req.StoreId == "store-id" &&
				req.AuthorizationModelId == "model-id" &&
				req.Writes != nil &&
				len(req.Writes.TupleKeys) == 2 &&
				req.Writes.OnDuplicate == "ignore"
		})).Return(&openfgav1.WriteResponse{}, nil)

		log := testlogger.New()
		mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)

		tuples := []corev1alpha1.Tuple{
			{Object: "doc:1", Relation: "viewer", User: "user:alice"},
			{Object: "doc:2", Relation: "owner", User: "user:bob"},
		}

		err := mgr.Apply(context.Background(), tuples)
		assert.NoError(t, err)
	})

	t.Run("returns error when write fails", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		client.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, errors.New("write failed"))

		log := testlogger.New()
		mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)

		tuples := []corev1alpha1.Tuple{
			{Object: "doc:1", Relation: "viewer", User: "user:alice"},
		}

		err := mgr.Apply(context.Background(), tuples)
		assert.Error(t, err)
	})
}

func TestTupleManager_Delete(t *testing.T) {
	t.Run("returns nil for empty tuples", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		log := testlogger.New()
		mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)

		err := mgr.Delete(context.Background(), nil)
		assert.NoError(t, err)

		err = mgr.Delete(context.Background(), []corev1alpha1.Tuple{})
		assert.NoError(t, err)
	})

	t.Run("deletes tuples successfully", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		client.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
			return req.StoreId == "store-id" &&
				req.AuthorizationModelId == "model-id" &&
				req.Deletes != nil &&
				len(req.Deletes.TupleKeys) == 2 &&
				req.Deletes.OnMissing == "ignore"
		})).Return(&openfgav1.WriteResponse{}, nil)

		log := testlogger.New()
		mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)

		tuples := []corev1alpha1.Tuple{
			{Object: "doc:1", Relation: "viewer", User: "user:alice"},
			{Object: "doc:2", Relation: "owner", User: "user:bob"},
		}

		err := mgr.Delete(context.Background(), tuples)
		assert.NoError(t, err)
	})

	t.Run("returns error when delete fails", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		client.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, errors.New("delete failed"))

		log := testlogger.New()
		mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)

		tuples := []corev1alpha1.Tuple{
			{Object: "doc:1", Relation: "viewer", User: "user:alice"},
		}

		err := mgr.Delete(context.Background(), tuples)
		assert.Error(t, err)
	})
}

func TestTupleManager_Apply_verifies_tuple_contents(t *testing.T) {
	var capturedReq *openfgav1.WriteRequest
	client := mocks.NewMockOpenFGAServiceClient(t)
	client.EXPECT().Write(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, req *openfgav1.WriteRequest, opts ...grpc.CallOption) (*openfgav1.WriteResponse, error) {
		capturedReq = req
		return &openfgav1.WriteResponse{}, nil
	})

	log := testlogger.New()
	mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)

	tuples := []corev1alpha1.Tuple{
		{Object: "doc:1", Relation: "viewer", User: "user:alice"},
		{Object: "doc:2", Relation: "owner", User: "user:bob"},
	}

	err := mgr.Apply(context.Background(), tuples)
	require.NoError(t, err)
	require.NotNil(t, capturedReq)
	require.NotNil(t, capturedReq.Writes)
	require.Len(t, capturedReq.Writes.TupleKeys, 2)

	// Verify both tuples are in the request
	keys := capturedReq.Writes.TupleKeys
	assert.True(t, (keys[0].Object == "doc:1" && keys[0].Relation == "viewer" && keys[0].User == "user:alice") ||
		(keys[1].Object == "doc:1" && keys[1].Relation == "viewer" && keys[1].User == "user:alice"))
	assert.True(t, (keys[0].Object == "doc:2" && keys[0].Relation == "owner" && keys[0].User == "user:bob") ||
		(keys[1].Object == "doc:2" && keys[1].Relation == "owner" && keys[1].User == "user:bob"))
}

func TestTupleManager_Delete_verifies_tuple_contents(t *testing.T) {
	var capturedReq *openfgav1.WriteRequest
	client := mocks.NewMockOpenFGAServiceClient(t)
	client.EXPECT().Write(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, req *openfgav1.WriteRequest, opts ...grpc.CallOption) (*openfgav1.WriteResponse, error) {
		capturedReq = req
		return &openfgav1.WriteResponse{}, nil
	})

	log := testlogger.New()
	mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)

	tuples := []corev1alpha1.Tuple{
		{Object: "doc:1", Relation: "viewer", User: "user:alice"},
		{Object: "doc:2", Relation: "owner", User: "user:bob"},
	}

	err := mgr.Delete(context.Background(), tuples)
	require.NoError(t, err)
	require.NotNil(t, capturedReq)
	require.NotNil(t, capturedReq.Deletes)
	require.Len(t, capturedReq.Deletes.TupleKeys, 2)

	keys := capturedReq.Deletes.TupleKeys
	assert.True(t, (keys[0].Object == "doc:1" && keys[0].Relation == "viewer" && keys[0].User == "user:alice") ||
		(keys[1].Object == "doc:1" && keys[1].Relation == "viewer" && keys[1].User == "user:alice"))
	assert.True(t, (keys[0].Object == "doc:2" && keys[0].Relation == "owner" && keys[0].User == "user:bob") ||
		(keys[1].Object == "doc:2" && keys[1].Relation == "owner" && keys[1].User == "user:bob"))
}

func TestIsTupleOfAccountFilter_returnsFalseForAllTuplesWhenGeneratedClusterIdEmpty(t *testing.T) {
	_, ai := testAccountAndInfo("test-account", "")
	filter := IsTupleOfAccountFilter(ai.Spec.Account.GeneratedClusterId)

	// Any tuple should be rejected when GeneratedClusterId is empty
	tuples := []corev1alpha1.Tuple{
		{Object: "account:1mj722nrt4jo3ggn/test-account", Relation: "viewer", User: "user:alice"},
		{Object: "account:1yrj2fwqtxcxbm1v/other-account", Relation: "owner", User: "user:bob"},
		{Object: "doc:1", Relation: "viewer", User: "user:charlie"},
	}
	for _, tpl := range tuples {
		assert.False(t, filter(tpl), "filter should return false for tuple %s when GeneratedClusterId is empty", tpl.Object)
	}
}

func TestIsTupleOfAccountFilter_deleteRemovesGeneratedTuples(t *testing.T) {
	// Use distinct GeneratedClusterIds so the filter matches only one account's tuples
	acc, ai := testAccountAndInfo("test-account", "1mj722nrt4jo3ggn")
	var creator string
	if acc.Spec.Creator != nil {
		creator = *acc.Spec.Creator
	}
	accountTuples, err := InitialTuplesForAccount(InitialTuplesForAccountInput{
		BaseTuplesInput: BaseTuplesInput{
			Creator:                creator,
			AccountOriginClusterID: ai.Spec.Account.OriginClusterId,
			AccountName:            ai.Spec.Account.Name,
			CreatorRelation:        "creator",
			ObjectType:             "account",
		},
		ParentOriginClusterID: ai.Spec.ParentAccount.OriginClusterId,
		ParentName:            ai.Spec.ParentAccount.Name,
		ParentRelation:        "parent",
	})
	require.NoError(t, err)

	// Tuples for a second account (should NOT be deleted when we delete test-account's tuples)
	acc2, ai2 := testAccountAndInfo("other-account", "1yrj2fwqtxcxbm1v")
	var creator2 string
	if acc2.Spec.Creator != nil {
		creator2 = *acc2.Spec.Creator
	}
	otherTuples, err := InitialTuplesForAccount(InitialTuplesForAccountInput{
		BaseTuplesInput: BaseTuplesInput{
			Creator:                creator2,
			AccountOriginClusterID: ai2.Spec.Account.OriginClusterId,
			AccountName:            ai2.Spec.Account.Name,
			CreatorRelation:        "creator",
			ObjectType:             "account",
		},
		ParentOriginClusterID: ai2.Spec.ParentAccount.OriginClusterId,
		ParentName:            ai2.Spec.ParentAccount.Name,
		ParentRelation:        "parent",
	})
	require.NoError(t, err)

	// allTuples: database managed by mocks (Write appends/deletes, Read returns current state)
	allTuples := make([]corev1alpha1.Tuple, 0)

	client := mocks.NewMockOpenFGAServiceClient(t)
	log := testlogger.New()
	mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)

	client.EXPECT().Write(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, req *openfgav1.WriteRequest, opts ...grpc.CallOption) (*openfgav1.WriteResponse, error) {
		if req.Writes != nil {
			for _, k := range req.Writes.TupleKeys {
				allTuples = append(allTuples, corev1alpha1.Tuple{
					Object: k.Object, Relation: k.Relation, User: k.User,
				})
			}
		}
		if req.Deletes != nil {
			for _, k := range req.Deletes.TupleKeys {
				allTuples = slices.DeleteFunc(allTuples, func(t corev1alpha1.Tuple) bool {
					return t.Object == k.Object && t.Relation == k.Relation && t.User == k.User
				})
			}
		}
		return &openfgav1.WriteResponse{}, nil
	})

	client.EXPECT().Read(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, req *openfgav1.ReadRequest, opts ...grpc.CallOption) (*openfgav1.ReadResponse, error) {
		return &openfgav1.ReadResponse{Tuples: tuplesToOpenFGA(allTuples)}, nil
	})

	// 1. Apply: populate store with account + other tuples
	tuplesToApply := append(accountTuples, otherTuples...)
	err = mgr.Apply(context.Background(), tuplesToApply)
	require.NoError(t, err)
	require.Len(t, allTuples, len(tuplesToApply), "database should contain all applied tuples")

	// 2. ListWithFilter: should return only account tuples
	filtered, err := mgr.ListWithFilter(context.Background(), IsTupleOfAccountFilter(ai.Spec.Account.GeneratedClusterId))
	require.NoError(t, err)
	require.Len(t, filtered, len(accountTuples), "filter should return only account tuples")

	// 3. Delete: remove filtered (account) tuples from store
	err = mgr.Delete(context.Background(), filtered)
	require.NoError(t, err)

	// 4. Verify database: only otherTuples remain
	require.Len(t, allTuples, len(otherTuples), "database should only contain non-account tuples after delete")
	for _, tpl := range allTuples {
		assert.False(t, slices.Contains(accountTuples, tpl), "account tuple %s should have been deleted", tpl.Object)
	}
}

func TestTupleManager_ListWithFilter(t *testing.T) {
	t.Run("nil filter returns error", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		log := testlogger.New()
		mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)

		result, err := mgr.ListWithFilter(context.Background(), nil)
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("Read error returns error", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		client.EXPECT().Read(mock.Anything, mock.Anything).Return(nil, errors.New("read failed"))

		log := testlogger.New()
		mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)

		result, err := mgr.ListWithFilter(context.Background(), func(t corev1alpha1.Tuple) bool { return true })
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("skips tuples with nil key", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		client.EXPECT().Read(mock.Anything, mock.Anything).Return(&openfgav1.ReadResponse{
			Tuples: []*openfgav1.Tuple{
				{Key: nil},
				{Key: &openfgav1.TupleKey{Object: "doc:1", Relation: "viewer", User: "user:alice"}},
			},
		}, nil)

		log := testlogger.New()
		mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)

		result, err := mgr.ListWithFilter(context.Background(), func(t corev1alpha1.Tuple) bool { return true })
		assert.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, "doc:1", result[0].Object)
	})

	t.Run("pagination: reads all pages", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		callCount := 0
		client.EXPECT().Read(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, req *openfgav1.ReadRequest, opts ...grpc.CallOption) (*openfgav1.ReadResponse, error) {
			callCount++
			if callCount == 1 {
				return &openfgav1.ReadResponse{
					Tuples:            []*openfgav1.Tuple{{Key: &openfgav1.TupleKey{Object: "doc:1", Relation: "viewer", User: "user:alice"}}},
					ContinuationToken: "page2-token",
				}, nil
			}
			return &openfgav1.ReadResponse{
				Tuples: []*openfgav1.Tuple{{Key: &openfgav1.TupleKey{Object: "doc:2", Relation: "owner", User: "user:bob"}}},
			}, nil
		}).Times(2)

		log := testlogger.New()
		mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)

		result, err := mgr.ListWithFilter(context.Background(), func(t corev1alpha1.Tuple) bool { return true })
		assert.NoError(t, err)
		assert.Len(t, result, 2)
	})
}

func TestTupleManager_ListWithKey(t *testing.T) {
	t.Run("returns matching tuples", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		client.EXPECT().Read(mock.Anything, mock.MatchedBy(func(req *openfgav1.ReadRequest) bool {
			return req.StoreId == "store-id" && req.TupleKey != nil && req.TupleKey.Object == "doc:1"
		})).Return(&openfgav1.ReadResponse{
			Tuples: []*openfgav1.Tuple{
				{Key: &openfgav1.TupleKey{Object: "doc:1", Relation: "viewer", User: "user:alice"}},
				{Key: &openfgav1.TupleKey{Object: "doc:1", Relation: "viewer", User: "user:bob"}},
			},
		}, nil)

		log := testlogger.New()
		mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)

		key := &openfgav1.ReadRequestTupleKey{Object: "doc:1"}
		result, err := mgr.ListWithKey(context.Background(), key)
		assert.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, "doc:1", result[0].Object)
		assert.Equal(t, "user:alice", result[0].User)
	})

	t.Run("Read error returns error", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		client.EXPECT().Read(mock.Anything, mock.Anything).Return(nil, errors.New("read failed"))

		log := testlogger.New()
		mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)

		result, err := mgr.ListWithKey(context.Background(), nil)
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("skips tuples with nil key", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		client.EXPECT().Read(mock.Anything, mock.Anything).Return(&openfgav1.ReadResponse{
			Tuples: []*openfgav1.Tuple{
				{Key: nil},
				{Key: &openfgav1.TupleKey{Object: "doc:1", Relation: "viewer", User: "user:alice"}},
			},
		}, nil)

		log := testlogger.New()
		mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)

		result, err := mgr.ListWithKey(context.Background(), nil)
		assert.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, "doc:1", result[0].Object)
	})

	t.Run("pagination: reads all pages", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		callCount := 0
		client.EXPECT().Read(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, req *openfgav1.ReadRequest, opts ...grpc.CallOption) (*openfgav1.ReadResponse, error) {
			callCount++
			if callCount == 1 {
				assert.Equal(t, "", req.ContinuationToken)
				return &openfgav1.ReadResponse{
					Tuples:            []*openfgav1.Tuple{{Key: &openfgav1.TupleKey{Object: "doc:1", Relation: "viewer", User: "user:alice"}}},
					ContinuationToken: "page2-token",
				}, nil
			}
			assert.Equal(t, "page2-token", req.ContinuationToken)
			return &openfgav1.ReadResponse{
				Tuples: []*openfgav1.Tuple{{Key: &openfgav1.TupleKey{Object: "doc:2", Relation: "owner", User: "user:bob"}}},
			}, nil
		}).Times(2)

		log := testlogger.New()
		mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)

		result, err := mgr.ListWithKey(context.Background(), nil)
		assert.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, "doc:1", result[0].Object)
		assert.Equal(t, "doc:2", result[1].Object)
	})
}

func testAccountAndInfo(accountName, clusterID string) (corev1alpha1.Account, corev1alpha1.AccountInfo) {
	creator := "user:alice"
	acc := corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: accountName},
		Spec: corev1alpha1.AccountSpec{
			Creator: &creator,
		},
	}
	ai := corev1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account"},
		Spec: corev1alpha1.AccountInfoSpec{
			Account: corev1alpha1.AccountLocation{
				Name:               accountName,
				GeneratedClusterId: clusterID,
				OriginClusterId:    clusterID,
			},
			ParentAccount: &corev1alpha1.AccountLocation{
				Name:            "parent-account",
				OriginClusterId: clusterID,
			},
		},
	}
	return acc, ai
}

func tuplesToOpenFGA(tuples []corev1alpha1.Tuple) []*openfgav1.Tuple {
	out := make([]*openfgav1.Tuple, 0, len(tuples))
	for _, t := range tuples {
		out = append(out, &openfgav1.Tuple{
			Key: &openfgav1.TupleKey{
				Object:   t.Object,
				Relation: t.Relation,
				User:     t.User,
			},
		})
	}
	return out
}
