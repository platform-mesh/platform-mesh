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

package directory

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/golang-commons/identity"
	"go.platform-mesh.io/golang-commons/idpdirectory"
	"go.platform-mesh.io/iam-service/pkg/config"
	appcontext "go.platform-mesh.io/iam-service/pkg/context"
	"go.platform-mesh.io/iam-service/pkg/graph"
)

type queryFunc func(context.Context, idpdirectory.Query) (idpdirectory.Result, error)

func (f queryFunc) Query(ctx context.Context, query idpdirectory.Query) (idpdirectory.Result, error) {
	return f(ctx, query)
}

func TestLookupEmailUsesBoundTenantAndCanonicalPrincipal(t *testing.T) {
	client := queryFunc(func(_ context.Context, query idpdirectory.Query) (idpdirectory.Result, error) {
		require.Equal(t, testTenant, query.Tenant)
		require.Equal(t, idpdirectory.LookupEmail, query.Operation)
		require.Equal(t, "alice@example.org", query.Email)
		return idpdirectory.Result{Users: []idpdirectory.User{testUser()}}, nil
	})
	service, err := New(client)
	require.NoError(t, err)
	user, err := service.LookupEmail(tenantContext(), "alice@example.org")
	require.NoError(t, err)
	require.Equal(t, "alice@example.org", user.UserID)
	require.Equal(t, "alice@example.org", user.Email)
	require.Equal(t, "Alice", *user.FirstName)
	require.Equal(t, "Anderson", *user.LastName)
}

func TestLookupEmailPropagatesAmbiguity(t *testing.T) {
	service, err := New(queryFunc(func(context.Context, idpdirectory.Query) (idpdirectory.Result, error) {
		return idpdirectory.Result{}, idpdirectory.ErrAmbiguous
	}))
	require.NoError(t, err)
	_, err = service.LookupEmail(tenantContext(), "alice@example.org")
	require.ErrorIs(t, err, idpdirectory.ErrAmbiguous)
}

func TestLookupEmailRejectsDifferentAddress(t *testing.T) {
	service, err := New(queryFunc(func(context.Context, idpdirectory.Query) (idpdirectory.Result, error) {
		user := testUser()
		user.Email = "other@example.org"
		return idpdirectory.Result{Users: []idpdirectory.User{user}}, nil
	}))
	require.NoError(t, err)
	_, err = service.LookupEmail(tenantContext(), "alice@example.org")
	require.ErrorContains(t, err, "different email address")
}

func TestListUsersFollowsPagesAndRejectsRepeatedCursor(t *testing.T) {
	t.Run("pages", func(t *testing.T) {
		calls := 0
		service, err := New(queryFunc(func(_ context.Context, query idpdirectory.Query) (idpdirectory.Result, error) {
			require.Equal(t, testTenant, query.Tenant)
			calls++
			if calls == 1 {
				require.Empty(t, query.PageToken)
				return idpdirectory.Result{Users: []idpdirectory.User{testUser()}, NextPageToken: "next"}, nil
			}
			require.Equal(t, "next", query.PageToken)
			return idpdirectory.Result{Users: []idpdirectory.User{{
				Principal: identity.Principal{Issuer: "https://issuer.example", Subject: "bob"},
				Email:     "bob@example.org",
			}}}, nil
		}))
		require.NoError(t, err)
		users, err := service.ListUsers(tenantContext())
		require.NoError(t, err)
		require.Len(t, users, 2)
		require.Equal(t, 2, calls)
	})

	t.Run("repeated cursor", func(t *testing.T) {
		service, err := New(queryFunc(func(context.Context, idpdirectory.Query) (idpdirectory.Result, error) {
			return idpdirectory.Result{NextPageToken: "same"}, nil
		}))
		require.NoError(t, err)
		_, err = service.ListUsers(tenantContext())
		require.ErrorContains(t, err, "repeated a pagination token")
	})
}

func TestEnrichUserRolesUsesDirectoryAndSkipsMissingUsers(t *testing.T) {
	var mu sync.Mutex
	lookups := make(map[string]int)
	service, err := New(queryFunc(func(_ context.Context, query idpdirectory.Query) (idpdirectory.Result, error) {
		require.Equal(t, testTenant, query.Tenant)
		mu.Lock()
		lookups[query.Email]++
		mu.Unlock()
		if query.Email == "missing@example.org" {
			return idpdirectory.Result{}, idpdirectory.ErrNotFound
		}
		return idpdirectory.Result{Users: []idpdirectory.User{testUser()}}, nil
	}))
	require.NoError(t, err)
	roles := []*graph.UserRoles{
		{User: &graph.User{Email: "alice@example.org"}},
		{User: &graph.User{Email: "alice@example.org"}},
		{User: &graph.User{Email: "missing@example.org"}},
	}
	require.NoError(t, service.EnrichUserRoles(tenantContext(), roles))
	require.Equal(t, "alice@example.org", roles[0].User.UserID)
	require.Equal(t, "alice@example.org", roles[1].User.UserID)
	require.Empty(t, roles[2].User.UserID)
	require.Equal(t, map[string]int{"alice@example.org": 1, "missing@example.org": 1}, lookups)
}

func TestDirectoryOperationsRequireIdentityTenantContext(t *testing.T) {
	service, err := New(queryFunc(func(context.Context, idpdirectory.Query) (idpdirectory.Result, error) {
		return idpdirectory.Result{}, errors.New("unexpected query")
	}))
	require.NoError(t, err)
	ctx := appcontext.SetKCPContext(context.Background(), appcontext.KCPContext{})
	_, err = service.LookupEmail(ctx, "alice@example.org")
	require.ErrorContains(t, err, "identity tenant is unavailable")
}

func TestNewFromConfigRequiresMutualTLSCredentials(t *testing.T) {
	_, err := NewFromConfig(config.IdentityDirectoryConfig{
		Endpoint: "https://directory.example.org", Timeout: time.Second,
	})
	require.ErrorContains(t, err, "client certificate/key")
}

var testTenant = idpdirectory.Tenant{LogicalCluster: "tenant-cluster", Name: "org-a", UID: "tenant-uid"}

func tenantContext() context.Context {
	return appcontext.SetKCPContext(context.Background(), appcontext.KCPContext{IdentityTenant: &testTenant})
}

func testUser() idpdirectory.User {
	return idpdirectory.User{
		Principal: identity.Principal{Issuer: "https://issuer.example", Subject: "alice"},
		Email:     "alice@example.org", GivenName: "Alice", FamilyName: "Anderson",
	}
}
