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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"

	corev1 "k8s.io/api/core/v1"
)

func TestToKeycloakIdentityProvider_Discovery(t *testing.T) {
	rep, err := ToKeycloakIdentityProvider(pmcorev1alpha1.UpstreamIdentityProvider{
		Alias: "dex",
		Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
		OIDC: &pmcorev1alpha1.OIDCUpstreamConfig{
			DiscoveryURL:         "https://dex.example/.well-known/openid-configuration",
			ClientID:             "broker",
			ClientAuthentication: "client_secret_post",
		},
	}, "secret")
	require.NoError(t, err)

	assert.Equal(t, "dex", rep.Alias)
	assert.Equal(t, "oidc", rep.ProviderID)
	assert.Equal(t, "broker", rep.Config["clientId"])
	assert.Equal(t, "secret", rep.Config["clientSecret"])
	assert.Equal(t, "client_secret_post", rep.Config["clientAuthMethod"])
	assert.NotContains(t, rep.Config, "authorizationUrl")
}

func TestToKeycloakIdentityProvider_Manual(t *testing.T) {
	rep, err := ToKeycloakIdentityProvider(pmcorev1alpha1.UpstreamIdentityProvider{
		Alias: "manual",
		Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
		OIDC: &pmcorev1alpha1.OIDCUpstreamConfig{
			Issuer:           "https://issuer.example",
			AuthorizationURL: "https://issuer.example/auth",
			TokenURL:         "https://issuer.example/token",
			ClientID:         "broker",
		},
	}, "")
	require.NoError(t, err)

	assert.Equal(t, "https://issuer.example", rep.Config["issuer"])
	assert.Equal(t, "https://issuer.example/auth", rep.Config["authorizationUrl"])
	assert.Equal(t, "https://issuer.example/token", rep.Config["tokenUrl"])
}

func TestAdminClient_ImportIdentityProviderConfig(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/realms/test-realm/identity-provider/import-config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"authorizationUrl":      "https://dex.example/auth",
			"tokenUrl":              "https://dex.example/token",
			"issuer":                "https://dex.example",
			"jwksUrl":               "https://dex.example/keys",
			"useJwksUrl":            "true",
			"validateSignature":     "true",
			"metadataDescriptorUrl": "https://dex.example/.well-known/openid-configuration",
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := adminClient(t, srv)
	config, err := client.ImportIdentityProviderConfig(
		t.Context(),
		"oidc",
		"https://dex.example/.well-known/openid-configuration",
	)
	require.NoError(t, err)
	assert.Equal(t, "https://dex.example/auth", config["authorizationUrl"])
}

func TestMergeImportedOIDCConfig_PreservesClientCredentials(t *testing.T) {
	rep := IdentityProviderRepresentation{
		Config: map[string]string{
			"clientId":     "broker",
			"clientSecret": "secret",
		},
	}
	MergeImportedOIDCConfig(&rep, map[string]string{
		"authorizationUrl": "https://dex.example/auth",
		"clientId":         "imported",
	}, "clientId", "clientSecret")

	assert.Equal(t, "https://dex.example/auth", rep.Config["authorizationUrl"])
	assert.Equal(t, "broker", rep.Config["clientId"])
	assert.Equal(t, "secret", rep.Config["clientSecret"])
}

func TestAdminClient_IdentityProviderCRUD(t *testing.T) {
	var stored IdentityProviderRepresentation

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/realms/test-realm/identity-provider/instances", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]IdentityProviderRepresentation{stored})
	})
	mux.HandleFunc("GET /admin/realms/test-realm/identity-provider/instances/dex", func(w http.ResponseWriter, r *http.Request) {
		if stored.Alias == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stored)
	})
	mux.HandleFunc("POST /admin/realms/test-realm/identity-provider/instances", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &stored))
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("PUT /admin/realms/test-realm/identity-provider/instances/dex", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &stored))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /admin/realms/test-realm/identity-provider/instances/dex", func(w http.ResponseWriter, r *http.Request) {
		stored = IdentityProviderRepresentation{}
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := adminClient(t, srv)
	ctx := t.Context()

	rep, err := ToKeycloakIdentityProvider(pmcorev1alpha1.UpstreamIdentityProvider{
		Alias: "dex",
		Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
		OIDC: &pmcorev1alpha1.OIDCUpstreamConfig{
			DiscoveryURL: "https://dex.example/.well-known/openid-configuration",
			ClientID:     "broker",
			ClientSecretRef: corev1.SecretReference{
				Name: "dex-secret",
			},
		},
	}, "secret")
	require.NoError(t, err)

	require.NoError(t, client.CreateIdentityProvider(ctx, rep))

	got, err := client.GetIdentityProvider(ctx, "dex")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "dex", got.Alias)

	rep.DisplayName = "Dex"
	require.NoError(t, client.UpdateIdentityProvider(ctx, "dex", rep))

	list, err := client.ListIdentityProviders(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "Dex", list[0].DisplayName)

	require.NoError(t, client.DeleteIdentityProvider(ctx, "dex"))

	got, err = client.GetIdentityProvider(ctx, "dex")
	require.NoError(t, err)
	assert.Nil(t, got)
}
