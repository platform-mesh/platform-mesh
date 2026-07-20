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

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
)

func TestLoadSeedUpstreamConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "idp-seed.yaml")
	err := os.WriteFile(path, []byte(`
seedUpstreamIdentityProviders:
  realms: [default]
  providers:
    - alias: dex
      displayName: Dex
      type: oidc
      clientSecret: local-dev-broker-secret
      oidc:
        discoveryUrl: https://portal.localhost:8443/dex/.well-known/openid-configuration
        clientId: keycloak-broker
        clientAuthentication: client_secret_post
`), 0o600)
	require.NoError(t, err)

	cfg, err := LoadSeedUpstreamConfig(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.True(t, cfg.AllowsSeedingForRealm("default"))
	assert.False(t, cfg.AllowsSeedingForRealm("other"))

	require.Len(t, cfg.SeedUpstreamIdentityProviders.Providers, 1)
	provider := cfg.SeedUpstreamIdentityProviders.Providers[0]
	assert.Equal(t, "dex", provider.Alias)
	assert.Equal(t, "local-dev-broker-secret", provider.ClientSecret)
	assert.Equal(t, "keycloak-broker", provider.OIDC.ClientID)
	assert.Equal(t, "client_secret_post", provider.OIDC.ClientAuthentication)

	upstream := provider.ToUpstreamIdentityProvider("default")
	assert.Equal(t, "upstream-idp-client-secret-default-dex", upstream.OIDC.ClientSecretRef.Name)
	assert.Equal(t, "default", upstream.OIDC.ClientSecretRef.Namespace)
	assert.Equal(t, pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC, upstream.Type)
}

func TestAllowsSeedingForRealmEmptyRealmsAllowsNone(t *testing.T) {
	cfg := &SeedUpstreamConfig{
		SeedUpstreamIdentityProviders: SeedUpstreamIdentityProviders{
			Realms: nil,
			Providers: []SeedUpstreamIdentityProvider{{
				UpstreamIdentityProvider: pmcorev1alpha1.UpstreamIdentityProvider{Alias: "dex"},
			}},
		},
	}
	assert.False(t, cfg.AllowsSeedingForRealm("default"))
	assert.False(t, cfg.AllowsSeedingForRealm("kaufmann"))
}
