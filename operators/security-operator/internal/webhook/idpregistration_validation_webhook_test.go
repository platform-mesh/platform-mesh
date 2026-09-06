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

package webhook

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
)

func TestValidateIdPRegistrationSpec(t *testing.T) {
	t.Parallel()

	valid := pmcorev1alpha1.IdPRegistrationSpec{
		Alias: "corp",
		Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
		OIDC: &pmcorev1alpha1.IdPRegistrationOIDCConfig{
			ClientID: "client",
			ClientSecretRef: pmcorev1alpha1.IdPRegistrationSecretRef{
				Name: "upstream-secret",
			},
			DiscoveryURL: "https://idp.example.com/.well-known/openid-configuration",
		},
	}
	require.NoError(t, validateIdPRegistrationSpec(valid))

	err := validateIdPRegistrationSpec(pmcorev1alpha1.IdPRegistrationSpec{
		Alias: "corp",
		Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
		OIDC: &pmcorev1alpha1.IdPRegistrationOIDCConfig{
			ClientID: "client",
			ClientSecretRef: pmcorev1alpha1.IdPRegistrationSecretRef{
				Name: "upstream-secret",
			},
			DiscoveryURL:     "https://idp.example.com/.well-known/openid-configuration",
			AuthorizationURL: "https://idp.example.com/auth",
		},
	})
	assert.Error(t, err)

	err = validateIdPRegistrationSpec(pmcorev1alpha1.IdPRegistrationSpec{
		Alias: "corp",
		Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
		OIDC: &pmcorev1alpha1.IdPRegistrationOIDCConfig{
			ClientID: "client",
			ClientSecretRef: pmcorev1alpha1.IdPRegistrationSecretRef{
				Name: "upstream-secret",
			},
			DiscoveryURL: "http://idp.example.com/.well-known/openid-configuration",
		},
	})
	assert.Error(t, err)
}
