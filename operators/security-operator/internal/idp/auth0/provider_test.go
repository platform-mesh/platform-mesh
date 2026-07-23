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

package auth0

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/security-operator/internal/idp"
)

// testServer wraps the given handler with an OAuth token endpoint so the Auth0
// management client can obtain a token before every management API call.
func testServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"access_token": "management-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	})
	mux.HandleFunc("/", handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// closedServer returns a server that is immediately shut down so any request to
// it fails with "connection refused", simulating a network error.
func closedServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	return srv
}

func testClient(t *testing.T, srv *httptest.Server) *ManagementClient {
	t.Helper()
	return NewManagementClient(context.Background(), srv.URL, "client-id", "client-secret", "")
}

func TestNewManagementClient_BaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{name: "plain domain gets https scheme", baseURL: "tenant.auth0.com", want: "https://tenant.auth0.com"},
		{name: "trailing slash is trimmed", baseURL: "https://tenant.auth0.com/", want: "https://tenant.auth0.com"},
		{name: "explicit scheme is preserved", baseURL: "http://localhost:8080", want: "http://localhost:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewManagementClient(context.Background(), tt.baseURL, "id", "secret", "")
			assert.Equal(t, tt.want, c.baseURL)
		})
	}
}

func TestManagementClient_CreateOrganization(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantErr     bool
	}{
		{
			name: "organization created",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Equal(t, "/api/v2/organizations", r.URL.Path)
					json.NewEncoder(w).Encode(map[string]any{"id": "org-1", "name": "my-org"}) //nolint:errcheck
				})
			},
		},
		{
			name: "create returns error",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				})
			},
			wantErr: true,
		},
		{
			name:        "connection refused",
			setupServer: closedServer,
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			err := testClient(t, srv).CreateOrganization(context.Background(), "my-org", idp.OrganizationConfig{})
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestManagementClient_EnsureOrganization(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantCreated bool
		wantErr     bool
	}{
		{
			name: "organization created",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Equal(t, "/api/v2/organizations", r.URL.Path)
					json.NewEncoder(w).Encode(map[string]any{"id": "org-1", "name": "my-org"}) //nolint:errcheck
				})
			},
			wantCreated: true,
		},
		{
			name: "organization updated on conflict",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.Method == http.MethodPost && r.URL.Path == "/api/v2/organizations":
						w.WriteHeader(http.StatusConflict)
					case r.Method == http.MethodGet && r.URL.Path == "/api/v2/organizations/name/my-org":
						json.NewEncoder(w).Encode(map[string]any{"id": "org-1", "name": "my-org"}) //nolint:errcheck
					case r.Method == http.MethodPatch && r.URL.Path == "/api/v2/organizations/org-1":
						json.NewEncoder(w).Encode(map[string]any{"id": "org-1", "name": "my-org"}) //nolint:errcheck
					default:
						t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
					}
				})
			},
			wantCreated: false,
		},
		{
			name: "create returns error",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				})
			},
			wantErr: true,
		},
		{
			name:        "connection refused",
			setupServer: closedServer,
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			created, err := testClient(t, srv).EnsureOrganization(context.Background(), "my-org", idp.OrganizationConfig{})
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantCreated, created)
		})
	}
}

func TestManagementClient_UpdateOrganization(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantErr     bool
	}{
		{
			name: "successful update",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.Method == http.MethodGet && r.URL.Path == "/api/v2/organizations/name/my-org":
						json.NewEncoder(w).Encode(map[string]any{"id": "org-1", "name": "my-org"}) //nolint:errcheck
					case r.Method == http.MethodPatch && r.URL.Path == "/api/v2/organizations/org-1":
						json.NewEncoder(w).Encode(map[string]any{"id": "org-1", "name": "my-org"}) //nolint:errcheck
					default:
						t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
					}
				})
			},
		},
		{
			name: "organization not found",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				})
			},
			wantErr: true,
		},
		{
			name: "get organization error",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				})
			},
			wantErr: true,
		},
		{
			name: "update returns error",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet {
						json.NewEncoder(w).Encode(map[string]any{"id": "org-1", "name": "my-org"}) //nolint:errcheck
						return
					}
					w.WriteHeader(http.StatusForbidden)
				})
			},
			wantErr: true,
		},
		{
			name:        "connection refused",
			setupServer: closedServer,
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			err := testClient(t, srv).UpdateOrganization(context.Background(), "my-org", idp.OrganizationConfig{})
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestManagementClient_DeleteOrganization(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantErr     bool
	}{
		{
			name: "successful delete",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.Method == http.MethodGet && r.URL.Path == "/api/v2/organizations/name/my-org":
						json.NewEncoder(w).Encode(map[string]any{"id": "org-1", "name": "my-org"}) //nolint:errcheck
					case r.Method == http.MethodDelete && r.URL.Path == "/api/v2/organizations/org-1":
						w.WriteHeader(http.StatusNoContent)
					default:
						t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
					}
				})
			},
		},
		{
			name: "organization not found is success",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				})
			},
		},
		{
			name: "delete 404 is success",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet {
						json.NewEncoder(w).Encode(map[string]any{"id": "org-1", "name": "my-org"}) //nolint:errcheck
						return
					}
					w.WriteHeader(http.StatusNotFound)
				})
			},
		},
		{
			name: "delete returns error",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet {
						json.NewEncoder(w).Encode(map[string]any{"id": "org-1", "name": "my-org"}) //nolint:errcheck
						return
					}
					w.WriteHeader(http.StatusForbidden)
				})
			},
			wantErr: true,
		},
		{
			name: "get organization error",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				})
			},
			wantErr: true,
		},
		{
			name:        "connection refused",
			setupServer: closedServer,
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			err := testClient(t, srv).DeleteOrganization(context.Background(), "my-org")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestManagementClient_OrganizationExists(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantExists  bool
		wantErr     bool
	}{
		{
			name: "organization exists",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, "/api/v2/organizations/name/my-org", r.URL.Path)
					json.NewEncoder(w).Encode(map[string]any{"id": "org-1", "name": "my-org"}) //nolint:errcheck
				})
			},
			wantExists: true,
		},
		{
			name: "organization not found",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				})
			},
			wantExists: false,
		},
		{
			name: "server error",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				})
			},
			wantErr: true,
		},
		{
			name:        "connection refused",
			setupServer: closedServer,
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			exists, err := testClient(t, srv).OrganizationExists(context.Background(), "my-org")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantExists, exists)
		})
	}
}

func TestManagementClient_CreateTokenProvider(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantToken   string
		wantErr     bool
	}{
		{
			name: "successful token retrieval",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {})
			},
			wantToken: "management-token",
		},
		{
			name:        "token endpoint unreachable",
			setupServer: closedServer,
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			token, err := testClient(t, srv).CreateTokenProvider("")(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantToken, token)
		})
	}
}

func TestManagementClient_CreateTokenRefresher(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantToken   string
		wantErr     bool
	}{
		{
			name: "successful token refresh",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {})
			},
			wantToken: "management-token",
		},
		{
			name:        "token endpoint unreachable",
			setupServer: closedServer,
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			token, err := testClient(t, srv).CreateTokenRefresher("")(context.Background(), "")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantToken, token)
		})
	}
}

func TestManagementClient_GetUserByEmail(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantUser    *idp.User
		wantErr     bool
	}{
		{
			name: "user found",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, "/api/v2/users-by-email", r.URL.Path)
					json.NewEncoder(w).Encode([]map[string]any{ //nolint:errcheck
						{"user_id": "auth0|123", "email": "user@example.com", "blocked": false},
					})
				})
			},
			wantUser: &idp.User{ID: "auth0|123", Email: "user@example.com", Enabled: true},
		},
		{
			name: "blocked user is disabled",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					json.NewEncoder(w).Encode([]map[string]any{ //nolint:errcheck
						{"user_id": "auth0|123", "email": "user@example.com", "blocked": true},
					})
				})
			},
			wantUser: &idp.User{ID: "auth0|123", Email: "user@example.com", Enabled: false},
		},
		{
			name: "user not found",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					json.NewEncoder(w).Encode([]map[string]any{}) //nolint:errcheck
				})
			},
			wantErr: true,
		},
		{
			name: "query error",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				})
			},
			wantErr: true,
		},
		{
			name:        "connection refused",
			setupServer: closedServer,
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			user, err := testClient(t, srv).GetUserByEmail(context.Background(), "my-org", "user@example.com")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantUser, user)
		})
	}
}

func TestManagementClient_ListUsers(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantUsers   []idp.User
		wantErr     bool
	}{
		{
			name: "list all users",
			setupServer: func(t *testing.T) *httptest.Server {
				call := 0
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, "/api/v2/users", r.URL.Path)
					call++
					if call == 1 {
						json.NewEncoder(w).Encode(map[string]any{"users": []map[string]any{ //nolint:errcheck
							{"user_id": "auth0|1", "email": "a@example.com"},
							{"user_id": "auth0|2", "email": "b@example.com", "blocked": true},
						}})
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"users": []map[string]any{}}) //nolint:errcheck
				})
			},
			wantUsers: []idp.User{
				{ID: "auth0|1", Email: "a@example.com", Enabled: true},
				{ID: "auth0|2", Email: "b@example.com", Enabled: false},
			},
		},
		{
			name: "list all users error",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				})
			},
			wantErr: true,
		},
		{
			name:        "connection refused",
			setupServer: closedServer,
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			users, err := testClient(t, srv).ListUsers(context.Background(), "my-org")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantUsers, users)
		})
	}
}

func TestManagementClient_Endpoints(t *testing.T) {
	c := NewManagementClient(context.Background(), "https://tenant.auth0.com", "id", "secret", "")
	assert.Equal(t, "https://tenant.auth0.com/", c.IssuerURL(""))
	assert.Equal(t, "https://tenant.auth0.com/.well-known/jwks.json", c.JWKSURL(""))
	assert.Equal(t, "https://tenant.auth0.com/authorize", c.AuthorizationEndpoint(""))
	assert.Equal(t, "https://tenant.auth0.com/oauth/token", c.TokenEndpoint(""))
}

func TestDeref(t *testing.T) {
	s := "value"
	assert.Equal(t, "value", deref(&s))
	assert.Equal(t, "", deref(nil))
}

func TestManagementClient_ListClients(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantClients []idp.Client
		wantErr     bool
	}{
		{
			name: "list clients",
			setupServer: func(t *testing.T) *httptest.Server {
				call := 0
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, "/api/v2/clients", r.URL.Path)
					call++
					if call == 1 {
						json.NewEncoder(w).Encode(map[string]any{"clients": []map[string]any{ //nolint:errcheck
							{"client_id": "c1", "name": "client-one"},
							{"client_id": "c2", "name": "client-two"},
						}})
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"clients": []map[string]any{}}) //nolint:errcheck
				})
			},
			wantClients: []idp.Client{
				{ID: "c1", ClientID: "c1", Name: "client-one"},
				{ID: "c2", ClientID: "c2", Name: "client-two"},
			},
		},
		{
			name: "list error",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				})
			},
			wantErr: true,
		},
		{
			name:        "connection refused",
			setupServer: closedServer,
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			clients, err := testClient(t, srv).ListClients(context.Background(), "my-org")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantClients, clients)
		})
	}
}

func TestManagementClient_GetClientByName(t *testing.T) {
	listClients := func(w http.ResponseWriter, call *int) {
		*call++
		if *call == 1 {
			json.NewEncoder(w).Encode(map[string]any{"clients": []map[string]any{ //nolint:errcheck
				{"client_id": "c1", "name": "client-one"},
			}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"clients": []map[string]any{}}) //nolint:errcheck
	}
	tests := []struct {
		name       string
		clientName string
		wantClient *idp.Client
		wantErr    bool
	}{
		{
			name:       "client found",
			clientName: "client-one",
			wantClient: &idp.Client{ID: "c1", ClientID: "c1", Name: "client-one"},
		},
		{
			name:       "client not found returns nil",
			clientName: "missing",
			wantClient: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := 0
			srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
				listClients(w, &call)
			})
			client, err := testClient(t, srv).GetClientByName(context.Background(), "my-org", tt.clientName)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantClient, client)
		})
	}
}

func TestManagementClient_GetClientByID(t *testing.T) {
	listClients := func(w http.ResponseWriter, call *int) {
		*call++
		if *call == 1 {
			json.NewEncoder(w).Encode(map[string]any{"clients": []map[string]any{ //nolint:errcheck
				{"client_id": "c1", "name": "client-one"},
			}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"clients": []map[string]any{}}) //nolint:errcheck
	}
	t.Run("client found", func(t *testing.T) {
		call := 0
		srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
			listClients(w, &call)
		})
		client, err := testClient(t, srv).GetClientByID(context.Background(), "my-org", "c1")
		require.NoError(t, err)
		assert.Equal(t, &idp.Client{ID: "c1", ClientID: "c1", Name: "client-one"}, client)
	})
	t.Run("client not found returns error", func(t *testing.T) {
		call := 0
		srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
			listClients(w, &call)
		})
		_, err := testClient(t, srv).GetClientByID(context.Background(), "my-org", "missing")
		require.Error(t, err)
	})
	t.Run("list error", func(t *testing.T) {
		srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})
		_, err := testClient(t, srv).GetClientByID(context.Background(), "my-org", "c1")
		require.Error(t, err)
	})
}

func TestManagementClient_CreateServiceAccountClient(t *testing.T) {
	tests := []struct {
		name        string
		config      idp.ServiceAccountClientConfig
		setupServer func(t *testing.T) *httptest.Server
		wantClient  *idp.Client
		wantErr     bool
	}{
		{
			name:   "client created",
			config: idp.ServiceAccountClientConfig{ClientID: "svc", Name: "svc-account"},
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Equal(t, "/api/v2/clients", r.URL.Path)
					json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
						"client_id": "svc", "name": "svc-account", "client_secret": "topsecret",
					})
				})
			},
			wantClient: &idp.Client{ID: "svc", ClientID: "svc", Name: "svc-account", Secret: "topsecret"},
		},
		{
			name:   "name falls back to client id",
			config: idp.ServiceAccountClientConfig{ClientID: "svc"},
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
						"client_id": "svc", "name": "svc", "client_secret": "topsecret",
					})
				})
			},
			wantClient: &idp.Client{ID: "svc", ClientID: "svc", Name: "svc", Secret: "topsecret"},
		},
		{
			name:   "create error",
			config: idp.ServiceAccountClientConfig{ClientID: "svc"},
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				})
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			client, err := testClient(t, srv).CreateServiceAccountClient(context.Background(), "my-org", tt.config)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantClient, client)
		})
	}
}

func TestManagementClient_GetClientSecret(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantSecret  string
		wantErr     bool
	}{
		{
			name: "secret returned",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, "/api/v2/clients/c1", r.URL.Path)
					json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
						"client_id": "c1", "client_secret": "topsecret",
					})
				})
			},
			wantSecret: "topsecret",
		},
		{
			name: "get error",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				})
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			secret, err := testClient(t, srv).GetClientSecret(context.Background(), "my-org", "c1")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantSecret, secret)
		})
	}
}

func TestManagementClient_GrantServiceAccountRole(t *testing.T) {
	tests := []struct {
		name        string
		role        idp.Role
		audience    string
		setupServer func(t *testing.T) *httptest.Server
		wantErr     bool
	}{
		{
			name: "grant with explicit scopes",
			role: idp.Role{ID: "r1", Name: "admin", Scopes: []string{"read:x", "write:y"}},
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Equal(t, "/api/v2/client-grants", r.URL.Path)
					var body map[string]any
					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					assert.Equal(t, "c1", body["client_id"])
					assert.Equal(t, []any{"read:x", "write:y"}, body["scope"])
					assert.Nil(t, body["allow_all_scopes"])
					json.NewEncoder(w).Encode(map[string]any{"id": "cg1"}) //nolint:errcheck
				})
			},
		},
		{
			name: "grant with no scopes allows all",
			role: idp.Role{ID: "r1", Name: "admin"},
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					var body map[string]any
					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					assert.Equal(t, true, body["allow_all_scopes"])
					json.NewEncoder(w).Encode(map[string]any{"id": "cg1"}) //nolint:errcheck
				})
			},
		},
		{
			name: "create error",
			role: idp.Role{ID: "r1", Name: "admin"},
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				})
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			err := testClient(t, srv).GrantServiceAccountRole(context.Background(), "my-org", "c1", tt.role)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestManagementClient_GrantServiceAccountRole_NoAudience(t *testing.T) {
	c := NewManagementClient(context.Background(), "https://tenant.auth0.com", "id", "secret", "")
	c.audience = ""
	err := c.GrantServiceAccountRole(context.Background(), "my-org", "c1", idp.Role{Name: "admin"})
	require.Error(t, err)
}

func TestManagementClient_GetOrganizationRole(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantRole    *idp.Role
		wantErr     bool
	}{
		{
			name: "role found",
			setupServer: func(t *testing.T) *httptest.Server {
				call := 0
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, "/api/v2/roles", r.URL.Path)
					call++
					if call == 1 {
						json.NewEncoder(w).Encode(map[string]any{"roles": []map[string]any{ //nolint:errcheck
							{"id": "r1", "name": "admin"},
						}})
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"roles": []map[string]any{}}) //nolint:errcheck
				})
			},
			wantRole: &idp.Role{ID: "r1", Name: "admin"},
		},
		{
			name: "role not found returns nil",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					json.NewEncoder(w).Encode(map[string]any{"roles": []map[string]any{}}) //nolint:errcheck
				})
			},
			wantRole: nil,
		},
		{
			name: "list error",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				})
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			role, err := testClient(t, srv).GetOrganizationRole(context.Background(), "my-org", "admin")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantRole, role)
		})
	}
}

func TestManagementClient_CreateUser(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantErr     bool
	}{
		{
			name: "user created",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Equal(t, "/api/v2/users", r.URL.Path)
					json.NewEncoder(w).Encode(map[string]any{"user_id": "auth0|new", "email": "new@example.com"}) //nolint:errcheck
				})
			},
		},
		{
			name: "create error",
			setupServer: func(t *testing.T) *httptest.Server {
				return testServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				})
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			err := testClient(t, srv).CreateUser(context.Background(), "my-org", "client-id", "new@example.com", "https://invite")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
