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
)

func TestLinkIdentityProviderOrganization(t *testing.T) {
	redirect := true
	hideUnresolved := true

	rep := IdentityProviderRepresentation{
		Alias: "dex",
		Config: map[string]string{
			"clientId": "broker",
		},
	}

	LinkIdentityProviderOrganization(&rep, "org-1", &pmcorev1alpha1.EmailDomainRouting{
		Domains:              []string{"portal.localhost"},
		AutoRedirect:         &redirect,
		HideUntilDomainMatch: &hideUnresolved,
	}, nil)

	assert.True(t, rep.HideOnLogin)
	assert.Equal(t, "org-1", rep.OrganizationID)
	assert.Equal(t, "portal.localhost", rep.Config["kc.org.domain"])
	assert.Equal(t, "true", rep.Config["kc.org.broker.redirect.mode.email-matches"])
	assert.Equal(t, "true", rep.Config["kc.org.broker.login.hide-when-org-unknown"])
}

func TestClearOrganizationBrokerConfig(t *testing.T) {
	rep := IdentityProviderRepresentation{
		OrganizationID: "org-1",
		Config: map[string]string{
			"clientId":      "broker",
			"kc.org.domain": "portal.localhost",
			"kc.org.broker.redirect.mode.email-matches": "true",
			"kc.org.broker.login.hide-when-org-unknown": "true",
		},
	}

	ClearOrganizationBrokerConfig(&rep)

	assert.Empty(t, rep.OrganizationID)
	assert.Equal(t, "broker", rep.Config["clientId"])
	assert.NotContains(t, rep.Config, "kc.org.domain")
	assert.NotContains(t, rep.Config, "kc.org.broker.redirect.mode.email-matches")
	assert.NotContains(t, rep.Config, "kc.org.broker.login.hide-when-org-unknown")
}

func TestLinkIdentityProviderOrganization_DisabledRedirect(t *testing.T) {
	redirect := false
	rep := IdentityProviderRepresentation{
		Config: map[string]string{
			"kc.org.broker.redirect.mode.email-matches": "true",
		},
	}

	LinkIdentityProviderOrganization(&rep, "org-1", &pmcorev1alpha1.EmailDomainRouting{
		Domains:      []string{"portal.localhost"},
		AutoRedirect: &redirect,
	}, nil)

	assert.Equal(t, "org-1", rep.OrganizationID)
	assert.Equal(t, "portal.localhost", rep.Config["kc.org.domain"])
	assert.NotContains(t, rep.Config, "kc.org.broker.redirect.mode.email-matches")
}

func TestAdminClient_IdentityProviderCRUD(t *testing.T) {
	var stored IdentityProviderRepresentation

	mux := http.NewServeMux()
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

	rep := IdentityProviderRepresentation{
		Alias:      "dex",
		ProviderID: "oidc",
		Enabled:    true,
		Config: map[string]string{
			"clientId":     "broker",
			"clientSecret": "secret",
		},
	}

	require.NoError(t, client.CreateIdentityProvider(ctx, rep))

	got, err := client.GetIdentityProvider(ctx, "dex")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "dex", got.Alias)

	rep.DisplayName = "Dex"
	require.NoError(t, client.UpdateIdentityProvider(ctx, "dex", rep))

	got, err = client.GetIdentityProvider(ctx, "dex")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Dex", got.DisplayName)

	require.NoError(t, client.DeleteIdentityProvider(ctx, "dex"))

	got, err = client.GetIdentityProvider(ctx, "dex")
	require.NoError(t, err)
	assert.Nil(t, got)
}
