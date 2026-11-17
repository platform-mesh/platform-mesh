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
)

func TestInvitationUserRecoveryAndMailRetryDoNotConfuseProvisioningWithDelivery(t *testing.T) {
	users := []invitationUser{}
	created, attempts := 0, 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/admin/realms/org":
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"attributes": map[string]string{ownerAttribute: "owner"}}))
		case req.Method == http.MethodGet && req.URL.Path == "/admin/realms/org/users":
			require.Equal(t, "true", req.URL.Query().Get("exact"))
			require.Equal(t, "2", req.URL.Query().Get("max"))
			require.NoError(t, json.NewEncoder(w).Encode(users))
		case req.Method == http.MethodPost && req.URL.Path == "/admin/realms/org/users":
			var body struct {
				Username, Email string
				RequiredActions []string `json:"requiredActions"`
			}
			require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
			require.Equal(t, "person@example.org", body.Username)
			require.ElementsMatch(t, []string{"UPDATE_PASSWORD", "VERIFY_EMAIL"}, body.RequiredActions)
			created++
			users = append(users, invitationUser{ID: "user-uuid", Email: body.Email, Enabled: true})
			w.WriteHeader(http.StatusCreated)
		case req.Method == http.MethodGet && req.URL.Path == "/admin/realms/org/users/user-uuid":
			require.NoError(t, json.NewEncoder(w).Encode(users[0]))
		case req.Method == http.MethodPut && req.URL.Path == "/admin/realms/org/users/user-uuid/execute-actions-email":
			var actions []string
			require.NoError(t, json.NewDecoder(req.Body).Decode(&actions))
			require.ElementsMatch(t, []string{"UPDATE_PASSWORD", "VERIFY_EMAIL"}, actions)
			require.Equal(t, "client-id", req.URL.Query().Get("client_id"))
			require.Equal(t, "https://portal.example/callback", req.URL.Query().Get("redirect_uri"))
			require.Equal(t, "3600", req.URL.Query().Get("lifespan"))
			attempts++
			if attempts == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
			} else {
				w.WriteHeader(http.StatusNoContent)
			}
		default:
			t.Errorf("unexpected invitation operation: %s %s", req.Method, req.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = time.Second
	backend, err := NewNativeBackend(NativeConfig{AdminURL: server.URL, PublicURL: server.URL}, client, client)
	require.NoError(t, err)
	subject, err := backend.EnsureInvitationUser(t.Context(), "org", "owner", "person@example.org", "")
	require.NoError(t, err)
	require.Equal(t, "user-uuid", subject)
	require.Error(t, backend.DispatchInvitation(t.Context(), "org", "owner", subject, "client-id", "https://portal.example/callback", 3600))
	recovered, err := backend.EnsureInvitationUser(t.Context(), "org", "owner", "person@example.org", "")
	require.NoError(t, err)
	require.Equal(t, subject, recovered)
	recovered, err = backend.EnsureInvitationUser(t.Context(), "org", "owner", "person@example.org", subject)
	require.NoError(t, err)
	require.Equal(t, subject, recovered)
	require.NoError(t, backend.DispatchInvitation(t.Context(), "org", "owner", recovered, "client-id", "https://portal.example/callback", 3600))
	require.Equal(t, 1, created)
	require.Equal(t, 2, attempts)
	users[0].Email = "changed@example.org"
	_, err = backend.EnsureInvitationUser(t.Context(), "org", "owner", "person@example.org", subject)
	require.Error(t, err)
	require.Equal(t, 1, created)
}

func TestInvitationUserLookupNeverSelectsAnAmbiguousOrDisabledIdentity(t *testing.T) {
	for _, users := range [][]invitationUser{
		{{ID: "first", Email: "person@example.org", Enabled: true}, {ID: "second", Email: "person@example.org", Enabled: true}},
		{{ID: "disabled", Email: "person@example.org", Enabled: false}},
		{{ID: "mismatched", Email: "another@example.org", Enabled: true}},
	} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			require.Equal(t, http.MethodGet, req.Method)
			if req.URL.Path == "/admin/realms/org" {
				require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"attributes": map[string]string{ownerAttribute: "owner"}}))
				return
			}
			require.NoError(t, json.NewEncoder(w).Encode(users))
		}))
		client := server.Client()
		client.Timeout = time.Second
		backend, err := NewNativeBackend(NativeConfig{AdminURL: server.URL, PublicURL: server.URL}, client, client)
		require.NoError(t, err)
		_, err = backend.EnsureInvitationUser(t.Context(), "org", "owner", "person@example.org", "")
		server.Close()
		require.Error(t, err)
	}
}
