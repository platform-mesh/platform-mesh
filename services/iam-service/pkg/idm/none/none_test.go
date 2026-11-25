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

package none

import (
	"context"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	appcontext "go.platform-mesh.io/iam-service/pkg/context"
	fgamocks "go.platform-mesh.io/iam-service/pkg/fga/mocks"
	"go.platform-mesh.io/iam-service/pkg/graph"
)

type fakeStores struct {
	orgs []string
	err  error
}

func (f *fakeStores) GetStoreID(_ context.Context, _ openfgav1.OpenFGAServiceClient, orgID string) (string, error) {
	f.orgs = append(f.orgs, orgID)
	return "store-1", f.err
}

func (f *fakeStores) GetModelID(context.Context, openfgav1.OpenFGAServiceClient, string) (string, error) {
	return "", nil
}

func orgContext() context.Context {
	return appcontext.SetKCPContext(context.Background(), appcontext.KCPContext{OrganizationName: "acme"})
}

func tuple(user, relation, object string) *openfgav1.Tuple {
	return &openfgav1.Tuple{Key: &openfgav1.TupleKey{User: user, Relation: relation, Object: object}}
}

func TestUserByMail(t *testing.T) {
	d := New(nil, nil)

	user, err := d.UserByMail(context.Background(), " alice@example.com ")
	require.NoError(t, err)
	assert.Equal(t, &graph.User{UserID: "alice@example.com", Email: "alice@example.com"}, user)

	user, err = d.UserByMail(context.Background(), "")
	require.NoError(t, err)
	assert.Nil(t, user)
}

func TestGetUsers(t *testing.T) {
	fga := fgamocks.NewOpenFGAServiceClient(t)
	stores := &fakeStores{}

	fga.EXPECT().Read(mock.Anything, mock.MatchedBy(func(req *openfgav1.ReadRequest) bool {
		return req.GetStoreId() == "store-1" && req.GetTupleKey() == nil && req.GetContinuationToken() == ""
	})).Return(&openfgav1.ReadResponse{
		Tuples: []*openfgav1.Tuple{
			tuple("user:bob@example.com", "assignee", "role:core_platform-mesh_io_account/c1/acme/owner"),
			tuple("role:core_platform-mesh_io_account/c1/acme/owner#assignee", "owner", "core_platform-mesh_io_account:c1/acme"),
			tuple("user:*", "assignee", "role:core_platform-mesh_io_account/c1/acme/member"),
		},
		ContinuationToken: "next",
	}, nil).Once()
	fga.EXPECT().Read(mock.Anything, mock.MatchedBy(func(req *openfgav1.ReadRequest) bool {
		return req.GetContinuationToken() == "next"
	})).Return(&openfgav1.ReadResponse{
		Tuples: []*openfgav1.Tuple{
			tuple("user:alice@example.com", "assignee", "role:core_platform-mesh_io_account/c1/acme/member"),
			tuple("user:bob@example.com", "assignee", "role:core_platform-mesh_io_account/c1/acme/member"),
			tuple("user:carol@example.com", "parent", "core_namespace:c1/default"),
		},
	}, nil).Once()

	users, err := New(fga, stores).GetUsers(orgContext())
	require.NoError(t, err)
	assert.Equal(t, []*graph.User{
		{UserID: "alice@example.com", Email: "alice@example.com"},
		{UserID: "bob@example.com", Email: "bob@example.com"},
	}, users)
	assert.Equal(t, []string{"acme"}, stores.orgs, "the store of the request's organization must be read")
}

func TestGetUsers_Errors(t *testing.T) {
	t.Run("no kcp context", func(t *testing.T) {
		_, err := New(nil, nil).GetUsers(context.Background())
		require.Error(t, err)
	})

	t.Run("no store", func(t *testing.T) {
		fga := fgamocks.NewOpenFGAServiceClient(t)
		stores := &fakeStores{err: assert.AnError}

		_, err := New(fga, stores).GetUsers(orgContext())
		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("read fails", func(t *testing.T) {
		fga := fgamocks.NewOpenFGAServiceClient(t)
		stores := &fakeStores{}
		fga.EXPECT().Read(mock.Anything, mock.Anything).Return(nil, assert.AnError)

		_, err := New(fga, stores).GetUsers(orgContext())
		require.ErrorIs(t, err, assert.AnError)
	})
}

func TestEnrichUserRoles(t *testing.T) {
	userRoles := []*graph.UserRoles{
		{User: &graph.User{Email: "alice@example.com"}},
		{User: &graph.User{UserID: "kept", Email: "bob@example.com"}},
		{},
		nil,
	}

	require.NoError(t, New(nil, nil).EnrichUserRoles(context.Background(), userRoles))
	assert.Equal(t, "alice@example.com", userRoles[0].User.UserID)
	assert.Nil(t, userRoles[0].User.FirstName)
	assert.Equal(t, "kept", userRoles[1].User.UserID)
}

func TestTenantRetriever(t *testing.T) {
	tenant, err := TenantRetriever{}.GetIDMTenant("http://127.0.0.1:8080/oidc")
	require.NoError(t, err)
	assert.Empty(t, tenant)
}
