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
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/golang-commons/idpdirectory"
)

func TestDirectoryAuthorizerRequiresCallerAndTenantScope(t *testing.T) {
	options := directoryOptions{
		timeout: time.Second, providerClass: "default", classWorkspace: "root:platform-mesh-system",
		workloads: []string{"spiffe://platform-mesh.test/iam"}, clusters: []string{"tenant-cluster"},
	}
	authorize, err := options.authorizer()
	require.NoError(t, err)
	tenant := idpdirectory.Tenant{LogicalCluster: "tenant-cluster", Name: "org-a", UID: "tenant-uid"}
	require.NoError(t, authorize(t.Context(), options.workloads[0], tenant))
	require.ErrorIs(t, authorize(t.Context(), "spiffe://untrusted.test/iam", tenant), idpdirectory.ErrUnauthorized)
	tenant.LogicalCluster = "another-cluster"
	require.ErrorIs(t, authorize(t.Context(), options.workloads[0], tenant), idpdirectory.ErrUnauthorized)
	options.clusters = nil
	_, err = options.authorizer()
	require.Error(t, err)
}

func TestDirectoryAuthorizerSupportsDynamicOrganizationClustersWhenExplicit(t *testing.T) {
	options := directoryOptions{
		timeout: time.Second, providerClass: "default", classWorkspace: "root:platform-mesh-system",
		workloads: []string{"spiffe://platform-mesh.test/iam"}, allowAnyTenantCluster: true,
	}
	authorize, err := options.authorizer()
	require.NoError(t, err)
	require.NoError(t, authorize(t.Context(), options.workloads[0], idpdirectory.Tenant{
		LogicalCluster: "new-organization-cluster", Name: "org-a", UID: "tenant-uid",
	}))
	options.clusters = []string{"tenant-cluster"}
	_, err = options.authorizer()
	require.Error(t, err)
}

func TestDirectoryProviderTransportAuthenticatesAndRejectsRedirects(t *testing.T) {
	t.Setenv("KEYCLOAK_CLIENT_SECRET", "test-secret")
	tokenRequests, userRequests := 0, 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/keycloak/realms/master/protocol/openid-connect/token":
			tokenRequests++
			id, secret, ok := r.BasicAuth()
			require.True(t, ok)
			require.Equal(t, "directory-client", id)
			require.Equal(t, "test-secret", secret)
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"access_token":"test-access-token","token_type":"Bearer","expires_in":3600}`))
			require.NoError(t, err)
		case "/keycloak/admin/realms/org-a/users":
			userRequests++
			require.Equal(t, "Bearer test-access-token", r.Header.Get("Authorization"))
			_, err := w.Write([]byte("[]"))
			require.NoError(t, err)
		case "/redirect":
			http.Redirect(w, r, "/keycloak/admin/realms/org-a/users", http.StatusTemporaryRedirect)
		default:
			t.Errorf("unexpected provider request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	caFile := filepath.Join(t.TempDir(), "provider-ca.pem")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{
		Type: "CERTIFICATE", Bytes: server.Certificate().Raw,
	}), 0o600))
	options := directoryOptions{
		baseURL: server.URL + "/keycloak", clientID: "directory-client", providerCA: caFile, timeout: 5 * time.Second,
	}
	client, cleanup, err := options.providerClient(t.Context())
	require.NoError(t, err)
	t.Cleanup(cleanup)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/keycloak/admin/realms/org-a/users", nil)
	require.NoError(t, err)
	response, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 1, tokenRequests)
	require.Equal(t, 1, userRequests)
	req, err = http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/redirect", nil)
	require.NoError(t, err)
	_, err = client.Do(req)
	require.ErrorContains(t, err, "redirects are forbidden")
	require.Equal(t, 1, userRequests)
}
