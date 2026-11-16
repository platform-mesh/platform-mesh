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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/golang-commons/identity"
	"go.platform-mesh.io/golang-commons/idpdirectory"
)

type resolverFunc func(context.Context, idpdirectory.Tenant) (TenantConfiguration, error)

func (f resolverFunc) Resolve(ctx context.Context, tenant idpdirectory.Tenant) (TenantConfiguration, error) {
	return f(ctx, tenant)
}

func TestDirectoryUsesResolvedRealmAndRejectsOtherIssuers(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, "/admin/realms/resolved-realm/users/subject-a", r.URL.Path)
		require.NoError(t, json.NewEncoder(w).Encode(user{
			ID: "subject-a", Email: "alice@example.org", EmailVerified: true,
		}))
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = 5 * time.Second
	resolver := resolverFunc(func(_ context.Context, tenant idpdirectory.Tenant) (TenantConfiguration, error) {
		require.Equal(t, "tenant-uid", tenant.UID)
		return TenantConfiguration{Realm: "resolved-realm", IssuerURL: "https://issuer.example/realms/resolved-realm"}, nil
	})
	backend, err := New(server.URL, client, resolver)
	require.NoError(t, err)
	principal := identity.Principal{Issuer: "https://issuer.example/realms/resolved-realm", Subject: "subject-a"}
	query := idpdirectory.Query{
		Tenant:    idpdirectory.Tenant{LogicalCluster: "c", Name: "not-a-realm-name", UID: "tenant-uid"},
		Operation: idpdirectory.LookupPrincipal, Principals: []identity.Principal{principal},
	}
	result, err := backend.Query(t.Context(), query)
	require.NoError(t, err)
	require.Equal(t, principal, result.Users[0].Principal)
	require.True(t, result.Users[0].EmailVerified)
	query.Principals[0].Issuer = "https://unrelated.example"
	_, err = backend.Query(t.Context(), query)
	require.ErrorIs(t, err, idpdirectory.ErrUnauthorized)
	query.Principals[0].Issuer = principal.Issuer
	for _, subject := range []string{".", "..", "path/segment", "path\\segment"} {
		query.Principals[0].Subject = subject
		_, err = backend.Query(t.Context(), query)
		require.Error(t, err)
	}
	require.Equal(t, 1, requests)
}

func TestDirectoryEmailLookupDoesNotChooseDuplicate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "alice@example.org", r.URL.Query().Get("email"))
		require.Equal(t, "true", r.URL.Query().Get("exact"))
		require.Equal(t, "2", r.URL.Query().Get("max"))
		require.NoError(t, json.NewEncoder(w).Encode([]user{{ID: "one"}, {ID: "two"}}))
	}))
	defer server.Close()
	backend := newTestBackend(t, server)
	_, err := backend.Query(t.Context(), idpdirectory.Query{
		Tenant: testTenant(), Operation: idpdirectory.LookupEmail, Email: "alice@example.org",
	})
	require.ErrorIs(t, err, idpdirectory.ErrAmbiguous)
}

func TestDirectoryPaginationIsBoundToTenantUID(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, "2", r.URL.Query().Get("max"))
		if requests == 1 {
			require.Equal(t, "0", r.URL.Query().Get("first"))
			require.NoError(t, json.NewEncoder(w).Encode([]user{{ID: "one"}, {ID: "two"}}))
			return
		}
		require.Equal(t, "1", r.URL.Query().Get("first"))
		require.NoError(t, json.NewEncoder(w).Encode([]user{{ID: "two"}}))
	}))
	defer server.Close()
	backend := newTestBackend(t, server)
	query := idpdirectory.Query{Tenant: testTenant(), Operation: idpdirectory.List, PageSize: 1}
	first, err := backend.Query(t.Context(), query)
	require.NoError(t, err)
	require.Len(t, first.Users, 1)
	require.NotEmpty(t, first.NextPageToken)
	query.PageToken = first.NextPageToken
	second, err := backend.Query(t.Context(), query)
	require.NoError(t, err)
	require.Equal(t, "two", second.Users[0].Principal.Subject)
	require.Empty(t, second.NextPageToken)
	query.Tenant.UID = "other-tenant"
	_, err = backend.Query(t.Context(), query)
	require.Error(t, err)
	require.Equal(t, 2, requests)
}

func TestDirectoryDoesNotHideProviderOrResolverFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, err := w.Write([]byte("private provider response"))
		require.NoError(t, err)
	}))
	defer server.Close()
	backend := newTestBackend(t, server)
	_, err := backend.Query(t.Context(), idpdirectory.Query{Tenant: testTenant(), Operation: idpdirectory.List})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private")
	backend.resolver = resolverFunc(func(context.Context, idpdirectory.Tenant) (TenantConfiguration, error) {
		return TenantConfiguration{}, idpdirectory.ErrUnauthorized
	})
	_, err = backend.Query(t.Context(), idpdirectory.Query{Tenant: testTenant(), Operation: idpdirectory.List})
	require.True(t, errors.Is(err, idpdirectory.ErrUnauthorized))
}

func TestDirectoryBatchOmitsOnlyMissingIdentities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/admin/realms/org-a/users/present":
			require.NoError(t, json.NewEncoder(w).Encode(user{ID: "present"}))
		case "/admin/realms/org-a/users/missing":
			http.NotFound(w, r)
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	backend := newTestBackend(t, server)
	query := idpdirectory.Query{
		Tenant: testTenant(), Operation: idpdirectory.BatchLookup,
		Principals: []identity.Principal{
			{Issuer: "https://issuer.example/realms/org-a", Subject: "present"},
			{Issuer: "https://issuer.example/realms/org-a", Subject: "missing"},
		},
	}
	result, err := backend.Query(t.Context(), query)
	require.NoError(t, err)
	require.Len(t, result.Users, 1)
	require.Equal(t, "present", result.Users[0].Principal.Subject)
	query.Principals[1].Subject = "unavailable"
	_, err = backend.Query(t.Context(), query)
	require.Error(t, err)
}

func TestDirectoryNullIsNotAnEmptySuccessfulResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte("null"))
		require.NoError(t, err)
	}))
	defer server.Close()
	backend := newTestBackend(t, server)
	_, err := backend.Query(t.Context(), idpdirectory.Query{Tenant: testTenant(), Operation: idpdirectory.List})
	require.Error(t, err)
	_, err = backend.Query(t.Context(), idpdirectory.Query{
		Tenant: testTenant(), Operation: idpdirectory.LookupEmail, Email: "alice@example.org",
	})
	require.Error(t, err)
	require.NotErrorIs(t, err, idpdirectory.ErrNotFound)
}

func newTestBackend(t *testing.T, server *httptest.Server) *Backend {
	t.Helper()
	client := server.Client()
	client.Timeout = 5 * time.Second
	backend, err := New(server.URL, client, resolverFunc(func(context.Context, idpdirectory.Tenant) (TenantConfiguration, error) {
		return TenantConfiguration{Realm: "org-a", IssuerURL: "https://issuer.example/realms/org-a"}, nil
	}))
	require.NoError(t, err)
	return backend
}

func testTenant() idpdirectory.Tenant {
	return idpdirectory.Tenant{LogicalCluster: "c", Name: "org-a", UID: "tenant-uid"}
}
