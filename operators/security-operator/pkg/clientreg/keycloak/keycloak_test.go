package keycloak

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// closedServer returns a server that is immediately shut down so any request
// to it fails with "connection refused", simulating a network error.
func closedServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	return srv
}

func adminClient(t *testing.T, srv *httptest.Server) *AdminClient {
	t.Helper()
	return NewAdminClient(http.DefaultClient, srv.URL, "test-realm")
}

func listClientsJSON() string {
	b, _ := json.Marshal([]ClientInfo{{ID: "uuid-1", ClientID: "my-client"}})
	return string(b)
}

func TestAdminClient_TokenForRegistration(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantToken   string
		wantErr     bool
	}{
		{
			name: "successful token retrieval",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Equal(t, "/admin/realms/test-realm/clients-initial-access", r.URL.Path)
					assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
					json.NewEncoder(w).Encode(map[string]string{"token": "initial-access-token-123"}) //nolint:errcheck
				}))
			},
			wantToken: "initial-access-token-123",
		},
		{
			name: "server returns error",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
					fmt.Fprint(w, "access denied") //nolint:errcheck
				}))
			},
			wantErr: true,
		},
		{
			name:        "connection refused",
			setupServer: closedServer,
			wantErr:     true,
		},
		{
			name: "decode error",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					fmt.Fprint(w, "not-json") //nolint:errcheck
				}))
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			defer srv.Close()
			token, err := adminClient(t, srv).TokenForRegistration(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantToken, token)
		})
	}
}

func TestAdminClient_RefreshToken(t *testing.T) {
	tests := []struct {
		name        string
		clientID    string
		setupServer func(t *testing.T) *httptest.Server
		wantToken   string
		wantErr     bool
	}{
		{
			name:     "successful token refresh",
			clientID: "my-client-id",
			setupServer: func(t *testing.T) *httptest.Server {
				call := 0
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					call++
					if call == 1 {
						assert.Equal(t, "/admin/realms/test-realm/clients", r.URL.Path)
						json.NewEncoder(w).Encode([]ClientInfo{{ID: "uuid-123", ClientID: "my-client-id", Name: "my-client"}}) //nolint:errcheck
						return
					}
					assert.Equal(t, "/admin/realms/test-realm/clients/uuid-123/registration-access-token", r.URL.Path)
					json.NewEncoder(w).Encode(map[string]string{"registrationAccessToken": "refreshed-token"}) //nolint:errcheck
				}))
			},
			wantToken: "refreshed-token",
		},
		{
			name:     "client not found",
			clientID: "unknown-client",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					json.NewEncoder(w).Encode([]ClientInfo{}) //nolint:errcheck
				}))
			},
			wantErr: true,
		},
		{
			name:     "token regeneration fails",
			clientID: "my-client-id",
			setupServer: func(t *testing.T) *httptest.Server {
				call := 0
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					call++
					if call == 1 {
						json.NewEncoder(w).Encode([]ClientInfo{{ID: "uuid-123", ClientID: "my-client-id"}}) //nolint:errcheck
						return
					}
					w.WriteHeader(http.StatusInternalServerError)
				}))
			},
			wantErr: true,
		},
		{
			name:        "ListClients connection refused",
			clientID:    "my-client",
			setupServer: closedServer,
			wantErr:     true,
		},
		{
			name:     "ListClients returns non-OK",
			clientID: "my-client",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
			},
			wantErr: true,
		},
		{
			name:     "token refresh call connection refused",
			clientID: "my-client",
			setupServer: func(t *testing.T) *httptest.Server {
				call := 0
				var srv *httptest.Server
				srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					call++
					if call == 1 {
						fmt.Fprint(w, listClientsJSON()) //nolint:errcheck
						go srv.Close()
						return
					}
				}))
				return srv
			},
			wantErr: true,
		},
		{
			name:     "decode error on token refresh response",
			clientID: "my-client",
			setupServer: func(t *testing.T) *httptest.Server {
				call := 0
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					call++
					if call == 1 {
						fmt.Fprint(w, listClientsJSON()) //nolint:errcheck
						return
					}
					fmt.Fprint(w, "not-json") //nolint:errcheck
				}))
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			defer srv.Close()
			token, err := adminClient(t, srv).RefreshToken(context.Background(), tt.clientID)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantToken, token)
		})
	}
}

func TestAdminClient_RegistrationEndpoint(t *testing.T) {
	assert.Equal(t,
		"https://keycloak.example.com/realms/my-realm/clients-registrations/openid-connect",
		NewAdminClient(nil, "https://keycloak.example.com", "my-realm").RegistrationEndpoint(),
	)
}

func TestAdminClient_RegistrationEndpoint_TrailingSlash(t *testing.T) {
	assert.Equal(t,
		"https://keycloak.example.com/realms/my-realm/clients-registrations/openid-connect",
		NewAdminClient(nil, "https://keycloak.example.com/", "my-realm").RegistrationEndpoint(),
	)
}

func TestAdminClient_RealmExists(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantExists  bool
		wantErr     bool
	}{
		{
			name: "realm exists",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
			},
			wantExists: true,
		},
		{
			name: "realm not found",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			wantExists: false,
		},
		{
			name: "server error",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
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
			defer srv.Close()
			exists, err := adminClient(t, srv).RealmExists(context.Background(), "test-realm")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantExists, exists)
		})
	}
}

func TestAdminClient_CreateOrUpdateRealm(t *testing.T) {
	tests := []struct {
		name        string
		config      RealmConfig
		setupServer func(t *testing.T) *httptest.Server
		wantCreated bool
		wantErr     bool
	}{
		{
			name:   "realm created",
			config: RealmConfig{Realm: "new-realm", Enabled: true},
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Equal(t, "/admin/realms", r.URL.Path)
					w.WriteHeader(http.StatusCreated)
				}))
			},
			wantCreated: true,
		},
		{
			name:   "realm updated on conflict",
			config: RealmConfig{Realm: "existing-realm", Enabled: true},
			setupServer: func(t *testing.T) *httptest.Server {
				call := 0
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					call++
					if call == 1 {
						assert.Equal(t, http.MethodPost, r.Method)
						w.WriteHeader(http.StatusConflict)
						return
					}
					assert.Equal(t, http.MethodPut, r.Method)
					assert.Equal(t, "/admin/realms/existing-realm", r.URL.Path)
					w.WriteHeader(http.StatusNoContent)
				}))
			},
			wantCreated: false,
		},
		{
			name:   "server error on create",
			config: RealmConfig{Realm: "error-realm"},
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
			},
			wantErr: true,
		},
		{
			name:        "connection refused on create",
			config:      RealmConfig{Realm: "my-realm"},
			setupServer: closedServer,
			wantErr:     true,
		},
		{
			name:   "connection refused on update",
			config: RealmConfig{Realm: "my-realm"},
			setupServer: func(t *testing.T) *httptest.Server {
				call := 0
				var srv *httptest.Server
				srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					call++
					if call == 1 {
						w.WriteHeader(http.StatusConflict)
						go srv.Close()
						return
					}
				}))
				return srv
			},
			wantErr: true,
		},
		{
			name:   "update realm returns error status",
			config: RealmConfig{Realm: "my-realm"},
			setupServer: func(t *testing.T) *httptest.Server {
				call := 0
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					call++
					if call == 1 {
						w.WriteHeader(http.StatusConflict)
						return
					}
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprint(w, "update failed") //nolint:errcheck
				}))
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			defer srv.Close()
			created, err := adminClient(t, srv).CreateOrUpdateRealm(context.Background(), tt.config)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantCreated, created)
		})
	}
}

func TestAdminClient_DeleteRealm(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantErr     bool
	}{
		{
			name: "successful delete",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodDelete, r.Method)
					assert.Equal(t, "/admin/realms/my-realm", r.URL.Path)
					w.WriteHeader(http.StatusNoContent)
				}))
			},
		},
		{
			name: "realm not found is success",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
			},
		},
		{
			name: "server error",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
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
			defer srv.Close()
			err := adminClient(t, srv).DeleteRealm(context.Background(), "my-realm")
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "failed to delete realm")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestAdminClient_ListClients(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantErr     bool
	}{
		{
			name:        "connection refused",
			setupServer: closedServer,
			wantErr:     true,
		},
		{
			name: "non-OK response",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				}))
			},
			wantErr: true,
		},
		{
			name: "decode error",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					fmt.Fprint(w, "not-json") //nolint:errcheck
				}))
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			defer srv.Close()
			_, err := adminClient(t, srv).ListClients(context.Background())
			require.Error(t, err)
		})
	}
}

func TestAdminClient_GetClientByName(t *testing.T) {
	tests := []struct {
		name        string
		clientName  string
		setupServer func(t *testing.T) *httptest.Server
		wantClient  *ClientInfo
		wantErr     bool
	}{
		{
			name:       "client found",
			clientName: "my-client",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, "/admin/realms/test-realm/clients", r.URL.Path)
					json.NewEncoder(w).Encode([]ClientInfo{ //nolint:errcheck
						{ID: "uuid-1", ClientID: "client-id-1", Name: "other-client"},
						{ID: "uuid-2", ClientID: "client-id-2", Name: "my-client"},
					})
				}))
			},
			wantClient: &ClientInfo{ID: "uuid-2", ClientID: "client-id-2", Name: "my-client"},
		},
		{
			name:       "client not found",
			clientName: "missing-client",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					json.NewEncoder(w).Encode([]ClientInfo{}) //nolint:errcheck
				}))
			},
			wantClient: nil,
		},
		{
			name:       "list error",
			clientName: "any",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			defer srv.Close()
			info, err := adminClient(t, srv).GetClientByName(context.Background(), tt.clientName)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, info)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantClient, info)
		})
	}
}

func TestAdminClient_CreateServiceAccountClient(t *testing.T) {
	tests := []struct {
		name           string
		setupServer    func(t *testing.T) *httptest.Server
		wantClientUUID string
		wantErr        bool
	}{
		{
			name:        "connection refused",
			setupServer: closedServer,
			wantErr:     true,
		},
		{
			name: "non-201 response",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusConflict)
					fmt.Fprint(w, "already exists") //nolint:errcheck
				}))
			},
			wantErr: true,
		},
		{
			name: "empty Location header",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusCreated)
				}))
			},
			wantErr: true,
		},
		{
			name: "success",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Location", r.URL.String()+"/abc-uuid-123")
					w.WriteHeader(http.StatusCreated)
				}))
			},
			wantClientUUID: "abc-uuid-123",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			defer srv.Close()
			info, err := adminClient(t, srv).CreateServiceAccountClient(context.Background(), ServiceAccountClientConfig{
				ClientID: "svc-client",
				Enabled:  true,
			})
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantClientUUID, info.ID)
		})
	}
}

func TestAdminClient_GetClientSecret(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantSecret  string
		wantErr     bool
	}{
		{
			name:        "connection refused",
			setupServer: closedServer,
			wantErr:     true,
		},
		{
			name: "non-200 response",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			wantErr: true,
		},
		{
			name: "decode error",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					fmt.Fprint(w, "not-json") //nolint:errcheck
				}))
			},
			wantErr: true,
		},
		{
			name: "success",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					json.NewEncoder(w).Encode(map[string]string{"value": "my-secret"}) //nolint:errcheck
				}))
			},
			wantSecret: "my-secret",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			defer srv.Close()
			secret, err := adminClient(t, srv).GetClientSecret(context.Background(), "uuid-123")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantSecret, secret)
		})
	}
}

func TestAdminClient_GetServiceAccountUser(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantUser    *UserInfo
		wantErr     bool
	}{
		{
			name:        "connection refused",
			setupServer: closedServer,
			wantErr:     true,
		},
		{
			name: "non-200 response",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			wantErr: true,
		},
		{
			name: "decode error",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					fmt.Fprint(w, "not-json") //nolint:errcheck
				}))
			},
			wantErr: true,
		},
		{
			name: "success",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					json.NewEncoder(w).Encode(UserInfo{ID: "user-123", Username: "svc-user"}) //nolint:errcheck
				}))
			},
			wantUser: &UserInfo{ID: "user-123", Username: "svc-user"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			defer srv.Close()
			user, err := adminClient(t, srv).GetServiceAccountUser(context.Background(), "uuid-123")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantUser, user)
		})
	}
}

func TestAdminClient_GetRealmRole(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantRole    *RoleInfo
		wantErr     bool
	}{
		{
			name:        "connection refused",
			setupServer: closedServer,
			wantErr:     true,
		},
		{
			name: "not found returns nil",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
			},
		},
		{
			name: "non-OK non-404 response",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				}))
			},
			wantErr: true,
		},
		{
			name: "decode error",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					fmt.Fprint(w, "not-json") //nolint:errcheck
				}))
			},
			wantErr: true,
		},
		{
			name: "success",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					json.NewEncoder(w).Encode(RoleInfo{ID: "role-123", Name: "admin"}) //nolint:errcheck
				}))
			},
			wantRole: &RoleInfo{ID: "role-123", Name: "admin"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			defer srv.Close()
			role, err := adminClient(t, srv).GetRealmRole(context.Background(), "admin")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantRole, role)
		})
	}
}

func TestAdminClient_AssignRealmRoleToUser(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantErr     bool
	}{
		{
			name:        "connection refused",
			setupServer: closedServer,
			wantErr:     true,
		},
		{
			name: "non-204 response",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
					fmt.Fprint(w, "forbidden") //nolint:errcheck
				}))
			},
			wantErr: true,
		},
		{
			name: "success with 204",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNoContent)
				}))
			},
		},
		{
			name: "success with 200",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			defer srv.Close()
			err := adminClient(t, srv).AssignRealmRoleToUser(context.Background(), "user-123", RoleInfo{ID: "role-123", Name: "admin"})
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
