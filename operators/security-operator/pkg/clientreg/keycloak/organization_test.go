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

package keycloak

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminClient_ListOrganizations(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/realms/test-realm/organizations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]OrganizationRepresentation{{
			ID:    "org-1",
			Name:  "Corp SSO",
			Alias: "corp-sso",
			Domains: []OrganizationDomainRepresentation{{
				Name:     "corp.example.com",
				Verified: true,
			}},
		}})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewAdminClient(srv.Client(), srv.URL, "test-realm")
	orgs, err := client.ListOrganizations(context.Background())
	require.NoError(t, err)
	require.Len(t, orgs, 1)
	assert.Equal(t, "org-1", orgs[0].ID)
}

func TestAdminClient_CreateOrUpdateOrganizationForDomains_Create(t *testing.T) {
	var created OrganizationRepresentation

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/realms/test-realm/organizations", func(w http.ResponseWriter, r *http.Request) {
		if created.ID != "" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]OrganizationRepresentation{created})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]OrganizationRepresentation{})
	})
	mux.HandleFunc("POST /admin/realms/test-realm/organizations", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &created))
		created.ID = "org-created"
		w.WriteHeader(http.StatusCreated)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewAdminClient(srv.Client(), srv.URL, "test-realm")
	org, reused, err := client.CreateOrUpdateOrganizationForDomains(
		context.Background(),
		"Corp SSO",
		"corp-sso",
		[]string{"corp.example.com"},
	)
	require.NoError(t, err)
	require.NotNil(t, org)
	assert.False(t, reused)
	assert.Equal(t, "org-created", org.ID)
	assert.Equal(t, "Corp SSO", created.Name)
}

func TestAdminClient_CreateOrUpdateOrganizationForDomains_ReuseExisting(t *testing.T) {
	var updated OrganizationRepresentation

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/realms/test-realm/organizations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]OrganizationRepresentation{{
			ID:    "org-existing",
			Name:  "Old Name",
			Alias: "old-alias",
			Domains: []OrganizationDomainRepresentation{{
				Name:     "corp.example.com",
				Verified: true,
			}},
		}})
	})
	mux.HandleFunc("PUT /admin/realms/test-realm/organizations/org-existing", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &updated))
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewAdminClient(srv.Client(), srv.URL, "test-realm")
	org, reused, err := client.CreateOrUpdateOrganizationForDomains(
		context.Background(),
		"Corp SSO",
		"corp-sso",
		[]string{"corp.example.com"},
	)
	require.NoError(t, err)
	require.NotNil(t, org)
	assert.True(t, reused)
	assert.Equal(t, "org-existing", org.ID)
	assert.Equal(t, "Corp SSO", updated.Name)
	assert.Equal(t, "corp-sso", updated.Alias)
}

func TestAdminClient_GetOrganizationByDomain_CaseInsensitive(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/realms/test-realm/organizations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]OrganizationRepresentation{{
			ID:   "org-1",
			Name: "Dex users",
			Domains: []OrganizationDomainRepresentation{{
				Name: "Portal.Localhost",
			}},
		}})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewAdminClient(srv.Client(), srv.URL, "test-realm")
	org, err := client.GetOrganizationByDomain(context.Background(), "portal.localhost")
	require.NoError(t, err)
	require.NotNil(t, org)
	assert.Equal(t, "org-1", org.ID)
}

func TestAdminClient_DeleteOrganization(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /admin/realms/test-realm/organizations/org-1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewAdminClient(srv.Client(), srv.URL, "test-realm")
	require.NoError(t, client.DeleteOrganization(context.Background(), "org-1"))
}
