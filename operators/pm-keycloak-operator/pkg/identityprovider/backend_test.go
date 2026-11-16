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

package identityprovider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/pm-keycloak-operator/pkg/clientreg"
)

func TestNativeRealmOwnershipAndDiscovery(t *testing.T) {
	tests := []struct {
		name, owner, issuer, subjects string
		missing, mustExist, wantError bool
		wantCreates, wantUpdates      int
	}{
		{name: "foreign realm", owner: "foreign", wantError: true},
		{name: "missing established realm", missing: true, mustExist: true, wantError: true},
		{name: "new owned realm", missing: true, subjects: `["public"]`, wantCreates: 1},
		{name: "existing owned realm", owner: "owner", subjects: `["public"]`, wantUpdates: 1},
		{name: "untrusted issuer", owner: "owner", issuer: "https://foreign.example", subjects: `["public"]`, wantUpdates: 1, wantError: true},
		{name: "pairwise only", owner: "owner", subjects: `["pairwise"]`, wantUpdates: 1, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creates, updates := 0, 0
			baseURL := ""
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case req.Method == http.MethodGet && req.URL.Path == "/admin/realms/org":
					if tt.missing {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"attributes": map[string]string{ownerAttribute: tt.owner}}))
				case req.Method == http.MethodPost && req.URL.Path == "/admin/realms":
					creates++
					var body struct {
						Attributes map[string]string `json:"attributes"`
					}
					require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
					require.Equal(t, "owner", body.Attributes[ownerAttribute])
					w.WriteHeader(http.StatusCreated)
				case req.Method == http.MethodPut && req.URL.Path == "/admin/realms/org":
					updates++
					w.WriteHeader(http.StatusNoContent)
				case req.Method == http.MethodGet && req.URL.Path == "/realms/org/.well-known/openid-configuration":
					issuer := tt.issuer
					if issuer == "" {
						issuer = baseURL + "/realms/org"
					}
					require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
						"issuer": issuer, "subject_types_supported": json.RawMessage(tt.subjects),
					}))
				default:
					t.Errorf("unexpected provider request: %s %s", req.Method, req.URL.Path)
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			defer server.Close()
			baseURL = server.URL
			client := server.Client()
			client.Timeout = time.Second
			backend, err := NewNativeBackend(NativeConfig{AdminURL: baseURL, PublicURL: baseURL}, client, client)
			require.NoError(t, err)
			observed, err := backend.EnsureTenant(t.Context(), "org", "owner", nil, tt.mustExist)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, baseURL+"/realms/org", observed.Issuer)
				require.Equal(t, observed.Issuer+"/.well-known/openid-configuration", observed.DiscoveryURL)
			}
			require.Equal(t, tt.wantCreates, creates)
			require.Equal(t, tt.wantUpdates, updates)
		})
	}
}

func TestManagementURITrustAndNativeIdentityRecovery(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		require.Equal(t, http.MethodGet, req.Method)
		require.Equal(t, "/admin/realms/org/clients", req.URL.Path)
		require.NoError(t, json.NewEncoder(w).Encode([]nativeClient{{ClientID: "client", ID: "replacement-uuid"}}))
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = time.Second
	backend, err := NewNativeBackend(NativeConfig{AdminURL: server.URL, PublicURL: "https://public.example/keycloak"}, client, client)
	require.NoError(t, err)
	for _, suffix := range []string{
		"/../client", "/%2e%2e", "/%2Fclient", "/%252Fclient", "/client?secret=x", "/client?", "/client#fragment",
	} {
		_, err := backend.managementURI("org", "https://public.example/keycloak/realms/org/clients-registrations/openid-connect"+suffix)
		require.Error(t, err, suffix)
	}
	for _, uri := range []string{
		"https://foreign.example/realms/org/clients-registrations/openid-connect/client",
		"https://public.example/keycloak/realms/other/clients-registrations/openid-connect/client",
	} {
		_, err := backend.managementURI("org", uri)
		require.Error(t, err)
	}
	internal, err := backend.managementURI("org", "https://public.example/keycloak/realms/org/clients-registrations/openid-connect/client")
	require.NoError(t, err)
	require.Equal(t, server.URL+"/realms/org/clients-registrations/openid-connect/client", internal)
	_, err = backend.RecoverManagementToken(t.Context(), "org", "client", "original-uuid")
	require.ErrorContains(t, err, "identity changed")
}

func TestNativeMetadataAccountsForDefaultScopesWithoutIgnoringOptionalScopeDrift(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/admin/realms/org/clients" {
			require.NoError(t, json.NewEncoder(w).Encode([]nativeClient{{ClientID: "client", ID: "uuid"}}))
			return
		}
		require.Equal(t, "/admin/realms/org/clients/uuid", req.URL.Path)
		_, err := w.Write([]byte(`{"defaultClientScopes":["profile","email"]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = time.Second
	backend, err := NewNativeBackend(NativeConfig{AdminURL: server.URL, PublicURL: server.URL}, client, client)
	require.NoError(t, err)
	scope := "profile email"
	empty := ""
	wanted := clientreg.ClientMetadata{
		Scope: &scope, ResponseTypes: []string{"code"}, RedirectURIs: []string{"https://portal.example/callback"},
	}
	info := clientreg.ClientInformation{
		ClientID: "client", ClientMetadata: clientreg.ClientMetadata{
			Scope: &empty, ResponseTypes: []string{"code", "none"}, RedirectURIs: wanted.RedirectURIs,
			PostLogoutRedirectURIs: wanted.RedirectURIs,
		},
	}
	matches, err := backend.MatchesMetadata(t.Context(), "org", info, wanted)
	require.NoError(t, err)
	require.True(t, matches)
	unwanted := "offline_access"
	info.Scope = &unwanted
	matches, err = backend.MatchesMetadata(t.Context(), "org", info, wanted)
	require.NoError(t, err)
	require.False(t, matches)
}
