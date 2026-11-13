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

package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/security-operator/pkg/clientreg/keycloak"
	"go.platform-mesh.io/security-operator/pkg/config"
)

func TestLoadInitContainerConfigAdmin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
keycloakBaseURL: http://keycloak
clients:
  - name: security-operator
    secretRef:
      name: security-operator-client-secret
      namespace: platform-mesh-system
  - name: reader
    admin: false
    secretRef:
      name: reader-client-secret
      namespace: platform-mesh-system
  - name: explicit-admin
    admin: true
    secretRef:
      name: explicit-admin-client-secret
      namespace: platform-mesh-system
`), 0o600))

	cfg, err := loadInitContainerConfig(&config.InitContainerConfig{ConfigFile: path})
	require.NoError(t, err)
	require.Len(t, cfg.Clients, 3)

	assert.Nil(t, cfg.Clients[0].Admin)
	assert.True(t, cfg.Clients[0].IsAdmin())
	assert.Equal(t, "reader-client-secret", cfg.Clients[1].SecretRef.Name)
	assert.False(t, cfg.Clients[1].IsAdmin())
	assert.True(t, cfg.Clients[2].IsAdmin())
}

func TestReconcileAdminRole(t *testing.T) {
	adminRole := keycloak.RoleInfo{ID: "admin-id", Name: "admin"}
	tests := []struct {
		name         string
		admin        bool
		mapped       []keycloak.RoleInfo
		deleteStatus int
		wantMethods  []string
		wantRemoved  bool
		wantErr      bool
	}{
		{
			name:        "admin client gets the role",
			admin:       true,
			wantMethods: []string{http.MethodPost},
		},
		{
			name:        "non-admin client without the role is left alone",
			mapped:      []keycloak.RoleInfo{{ID: "default-id", Name: "default-roles-master"}},
			wantMethods: []string{http.MethodGet},
		},
		{
			name:        "non-admin client loses the role",
			mapped:      []keycloak.RoleInfo{{ID: "default-id", Name: "default-roles-master"}, adminRole},
			wantMethods: []string{http.MethodGet, http.MethodDelete},
			wantRemoved: true,
		},
		{
			name:         "failing removal is an error",
			mapped:       []keycloak.RoleInfo{adminRole},
			deleteStatus: http.StatusForbidden,
			wantMethods:  []string{http.MethodGet, http.MethodDelete},
			wantErr:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var methods []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/admin/realms/master/users/sa-1/role-mappings/realm", r.URL.Path)
				methods = append(methods, r.Method)
				switch r.Method {
				case http.MethodGet:
					_ = json.NewEncoder(w).Encode(tt.mapped)
				case http.MethodDelete:
					var got []keycloak.RoleInfo
					assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
					assert.Equal(t, []keycloak.RoleInfo{adminRole}, got)
					if tt.deleteStatus != 0 {
						w.WriteHeader(tt.deleteStatus)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				default:
					w.WriteHeader(http.StatusNoContent)
				}
			}))
			defer srv.Close()

			client := keycloak.NewAdminClient(srv.Client(), srv.URL, "master")
			removed, err := reconcileAdminRole(context.Background(), client, "sa-1", adminRole, tt.admin)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.wantRemoved, removed)
			assert.Equal(t, tt.wantMethods, methods)
		})
	}
}
