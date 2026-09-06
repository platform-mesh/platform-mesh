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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"

	"k8s.io/utils/ptr"
)

func TestToKeycloakIdentityProviderFromRegistration_Discovery(t *testing.T) {
	rep, err := ToKeycloakIdentityProviderFromRegistration(pmcorev1alpha1.IdPRegistration{
		Spec: pmcorev1alpha1.IdPRegistrationSpec{
			Alias:   "corp",
			Type:    pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
			Enabled: ptr.To(true),
			OIDC: &pmcorev1alpha1.IdPRegistrationOIDCConfig{
				ClientID: "client-1",
			},
		},
	}, "secret", &OIDCDiscovery{
		Issuer:           "https://idp.example.com",
		AuthorizationURL: "https://idp.example.com/auth",
		TokenURL:         "https://idp.example.com/token",
		JWKSURL:          "https://idp.example.com/jwks",
	})
	require.NoError(t, err)
	assert.Equal(t, "corp", rep.Alias)
	assert.False(t, rep.TrustEmail)
	assert.Equal(t, "IMPORT", rep.Config["syncMode"])
	assert.Equal(t, "secret", rep.Config["clientSecret"])
	assert.Equal(t, "true", rep.Config["validateSignature"])
	assert.Equal(t, "true", rep.Config["useJwksUrl"])
	assert.Equal(t, "https://idp.example.com/token", rep.Config["tokenUrl"])
}

func TestToKeycloakIdentityProviderFromRegistration_Manual(t *testing.T) {
	rep, err := ToKeycloakIdentityProviderFromRegistration(pmcorev1alpha1.IdPRegistration{
		Spec: pmcorev1alpha1.IdPRegistrationSpec{
			Alias: "corp",
			Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
			OIDC: &pmcorev1alpha1.IdPRegistrationOIDCConfig{
				ClientID:         "client-1",
				Issuer:           "https://idp.example.com",
				AuthorizationURL: "https://idp.example.com/auth",
				TokenURL:         "https://idp.example.com/token",
			},
		},
	}, "", nil)
	require.NoError(t, err)
	assert.Equal(t, "https://idp.example.com/auth", rep.Config["authorizationUrl"])
}

func TestBrokerRedirectURI(t *testing.T) {
	assert.Equal(
		t,
		"https://portal.localhost:8443/keycloak/realms/kaufmann/broker/dex/endpoint",
		BrokerRedirectURI("portal.localhost:8443", "kaufmann", "dex"),
	)
	assert.Equal(
		t,
		"https://portal.localhost:8443/keycloak/realms/kaufmann/broker/test/endpoint",
		BrokerRedirectURI("portal.localhost:8443/", "kaufmann", "test"),
	)
	assert.Empty(t, BrokerRedirectURI("", "kaufmann", "dex"))
	assert.Empty(t, BrokerRedirectURI("portal.localhost:8443", "", "dex"))
	assert.Empty(t, BrokerRedirectURI("portal.localhost:8443", "kaufmann", "  "))
}
