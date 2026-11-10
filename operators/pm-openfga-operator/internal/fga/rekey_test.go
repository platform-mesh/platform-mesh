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

package fga

import (
	"context"
	"fmt"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger/testlogger"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"
)

func TestStaleAccountTupleFilter(t *testing.T) {
	filter := StaleAccountTupleFilter("acct", "myorg", "new-id")

	tests := []struct {
		name  string
		tuple pmcorev1alpha1.Tuple
		want  bool
	}{
		{
			name:  "stale role object",
			tuple: pmcorev1alpha1.Tuple{User: "user:alice", Relation: "assignee", Object: "role:acct/old-id/myorg/owner"},
			want:  true,
		},
		{
			name:  "stale role user and account object",
			tuple: pmcorev1alpha1.Tuple{User: "role:acct/old-id/myorg/owner#assignee", Relation: "owner", Object: "acct:old-id/myorg"},
			want:  true,
		},
		{
			name:  "current cluster id",
			tuple: pmcorev1alpha1.Tuple{User: "user:alice", Relation: "assignee", Object: "role:acct/new-id/myorg/owner"},
			want:  false,
		},
		{
			name:  "different account name",
			tuple: pmcorev1alpha1.Tuple{User: "user:alice", Relation: "assignee", Object: "role:acct/old-id/otherorg/owner"},
			want:  false,
		},
		{
			name:  "non-account keys",
			tuple: pmcorev1alpha1.Tuple{User: "user:*", Relation: "assignee", Object: "role:authenticated"},
			want:  false,
		},
		// Keys of OTHER object types share the raw shape (the org store holds
		// e.g. apis_kcp_io_apiexport user keys for APIExportPolicies) and must
		// never match, even with the org's name and a stale cluster id.
		{
			name:  "other object type in user key",
			tuple: pmcorev1alpha1.Tuple{User: "apis_kcp_io_apiexport:old-id/myorg", Relation: "member", Object: "acct:new-id/myorg"},
			want:  false,
		},
		{
			name:  "other object type in object key",
			tuple: pmcorev1alpha1.Tuple{User: "user:alice", Relation: "member", Object: "apis_kcp_io_apiexport:old-id/myorg"},
			want:  false,
		},
		{
			name:  "other object type in role key",
			tuple: pmcorev1alpha1.Tuple{User: "user:alice", Relation: "assignee", Object: "role:apis_kcp_io_apiexport/old-id/myorg/owner"},
			want:  false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, filter(test.tuple))
		})
	}

	t.Run("empty object type, account name or cluster id never match", func(t *testing.T) {
		stale := pmcorev1alpha1.Tuple{User: "user:alice", Relation: "assignee", Object: "role:acct/old-id/myorg/owner"}
		assert.False(t, StaleAccountTupleFilter("", "myorg", "new-id")(stale))
		assert.False(t, StaleAccountTupleFilter("acct", "", "new-id")(stale))
		assert.False(t, StaleAccountTupleFilter("acct", "myorg", "")(stale))
	})
}

func TestStaleClusterIDs(t *testing.T) {
	t.Run("single stale id from both keys", func(t *testing.T) {
		tuple := pmcorev1alpha1.Tuple{User: "role:acct/old-id/myorg/owner#assignee", Relation: "owner", Object: "acct:old-id/myorg"}
		assert.Equal(t, []string{"old-id"}, StaleClusterIDs(tuple, "acct", "myorg", "new-id"))
	})
	t.Run("two distinct stale ids are both reported", func(t *testing.T) {
		tuple := pmcorev1alpha1.Tuple{User: "role:acct/old-a/myorg/owner#assignee", Relation: "owner", Object: "acct:old-b/myorg"}
		assert.ElementsMatch(t, []string{"old-a", "old-b"}, StaleClusterIDs(tuple, "acct", "myorg", "new-id"))
	})
	t.Run("no stale ids", func(t *testing.T) {
		tuple := pmcorev1alpha1.Tuple{User: "user:alice", Relation: "assignee", Object: "role:acct/new-id/myorg/owner"}
		assert.Empty(t, StaleClusterIDs(tuple, "acct", "myorg", "new-id"))
	})
	t.Run("other object types are invisible", func(t *testing.T) {
		// The apiexport key references the org's name under a foreign old id:
		// it must not contribute a stale id.
		tuple := pmcorev1alpha1.Tuple{User: "apis_kcp_io_apiexport:other-old/myorg", Relation: "member", Object: "acct:old-id/myorg"}
		assert.Equal(t, []string{"old-id"}, StaleClusterIDs(tuple, "acct", "myorg", "new-id"))
	})
}

func TestRekeyTuple(t *testing.T) {
	tests := []struct {
		name string
		in   pmcorev1alpha1.Tuple
		want pmcorev1alpha1.Tuple
	}{
		{
			name: "creator tuple: only object rewritten",
			in:   pmcorev1alpha1.Tuple{User: "user:alice", Relation: "assignee", Object: "role:acct/old-id/myorg/owner"},
			want: pmcorev1alpha1.Tuple{User: "user:alice", Relation: "assignee", Object: "role:acct/new-id/myorg/owner"},
		},
		{
			name: "role-assignee tuple: object and user rewritten",
			in:   pmcorev1alpha1.Tuple{User: "role:acct/old-id/myorg/owner#assignee", Relation: "owner", Object: "acct:old-id/myorg"},
			want: pmcorev1alpha1.Tuple{User: "role:acct/new-id/myorg/owner#assignee", Relation: "owner", Object: "acct:new-id/myorg"},
		},
		{
			name: "keys with other cluster ids untouched",
			in:   pmcorev1alpha1.Tuple{User: "acct:another-id/parent", Relation: "parent", Object: "acct:old-id/myorg"},
			want: pmcorev1alpha1.Tuple{User: "acct:another-id/parent", Relation: "parent", Object: "acct:new-id/myorg"},
		},
		{
			name: "non-account keys untouched",
			in:   pmcorev1alpha1.Tuple{User: "user:*", Relation: "assignee", Object: "role:authenticated"},
			want: pmcorev1alpha1.Tuple{User: "user:*", Relation: "assignee", Object: "role:authenticated"},
		},
		{
			name: "keys of other object types untouched even with matching cluster id",
			in:   pmcorev1alpha1.Tuple{User: "apis_kcp_io_apiexport:old-id/myorg", Relation: "member", Object: "acct:old-id/myorg"},
			want: pmcorev1alpha1.Tuple{User: "apis_kcp_io_apiexport:old-id/myorg", Relation: "member", Object: "acct:new-id/myorg"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, RekeyTuple(test.in, "acct", "old-id", "new-id"))
		})
	}
}

func chunkTestTuples(n int) []pmcorev1alpha1.Tuple {
	tuples := make([]pmcorev1alpha1.Tuple, 0, n)
	for i := range n {
		tuples = append(tuples, pmcorev1alpha1.Tuple{
			User:     fmt.Sprintf("user:user-%d", i),
			Relation: "assignee",
			Object:   "role:acct/old-id/myorg/member",
		})
	}
	return tuples
}

func TestTupleManager_ApplyChunked(t *testing.T) {
	t.Run("chunks writes into requests of at most 100 tuples", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		var chunkSizes []int
		client.EXPECT().Write(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, req *openfgav1.WriteRequest, opts ...grpc.CallOption) (*openfgav1.WriteResponse, error) {
			require.NotNil(t, req.Writes)
			require.Nil(t, req.Deletes)
			chunkSizes = append(chunkSizes, len(req.Writes.TupleKeys))
			return &openfgav1.WriteResponse{}, nil
		}).Times(3)

		log := testlogger.New()
		mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)
		err := mgr.ApplyChunked(context.Background(), chunkTestTuples(250))
		require.NoError(t, err)
		assert.Equal(t, []int{100, 100, 50}, chunkSizes)
	})

	t.Run("empty input does not call the client", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		log := testlogger.New()
		mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)
		require.NoError(t, mgr.ApplyChunked(context.Background(), nil))
	})

	t.Run("stops at the first failed chunk", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		client.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()
		log := testlogger.New()
		mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)
		err := mgr.ApplyChunked(context.Background(), chunkTestTuples(250))
		assert.Error(t, err)
	})
}

func TestTupleManager_DeleteChunked(t *testing.T) {
	t.Run("chunks deletes into requests of at most 100 tuples", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		var chunkSizes []int
		client.EXPECT().Write(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, req *openfgav1.WriteRequest, opts ...grpc.CallOption) (*openfgav1.WriteResponse, error) {
			require.NotNil(t, req.Deletes)
			require.Nil(t, req.Writes)
			chunkSizes = append(chunkSizes, len(req.Deletes.TupleKeys))
			return &openfgav1.WriteResponse{}, nil
		}).Times(3)

		log := testlogger.New()
		mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)
		err := mgr.DeleteChunked(context.Background(), chunkTestTuples(201))
		require.NoError(t, err)
		assert.Equal(t, []int{100, 100, 1}, chunkSizes)
	})

	t.Run("stops at the first failed chunk", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		client.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()
		log := testlogger.New()
		mgr := NewTupleManager(client, "store-id", "model-id", log.Logger)
		err := mgr.DeleteChunked(context.Background(), chunkTestTuples(150))
		assert.Error(t, err)
	})
}
