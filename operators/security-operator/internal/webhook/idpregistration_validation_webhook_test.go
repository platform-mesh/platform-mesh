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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateIdPRegistration(t *testing.T) {
	t.Parallel()

	valid := &pmcorev1alpha1.IdPRegistration{
		ObjectMeta: metav1.ObjectMeta{Name: "corp"},
		Spec: pmcorev1alpha1.IdPRegistrationSpec{
			Alias: "corp",
			Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
			OIDC: &pmcorev1alpha1.IdPRegistrationOIDCConfig{
				ClientID: "client",
				ClientSecretRef: pmcorev1alpha1.IdPRegistrationSecretRef{
					Name: "upstream-secret",
				},
				DiscoveryURL: "https://idp.example.com/.well-known/openid-configuration",
			},
		},
	}
	require.NoError(t, validateIdPRegistration(valid))

	err := validateIdPRegistration(&pmcorev1alpha1.IdPRegistration{
		ObjectMeta: metav1.ObjectMeta{Name: "corp"},
		Spec: pmcorev1alpha1.IdPRegistrationSpec{
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
		},
	})
	assert.Error(t, err)

	err = validateIdPRegistration(&pmcorev1alpha1.IdPRegistration{
		ObjectMeta: metav1.ObjectMeta{Name: "corp"},
		Spec: pmcorev1alpha1.IdPRegistrationSpec{
			Alias: "corp",
			Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
			OIDC: &pmcorev1alpha1.IdPRegistrationOIDCConfig{
				ClientID: "client",
				ClientSecretRef: pmcorev1alpha1.IdPRegistrationSecretRef{
					Name: "upstream-secret",
				},
				DiscoveryURL: "http://idp.example.com/.well-known/openid-configuration",
			},
		},
	})
	assert.Error(t, err)

	err = validateIdPRegistration(&pmcorev1alpha1.IdPRegistration{
		ObjectMeta: metav1.ObjectMeta{Name: "other"},
		Spec: pmcorev1alpha1.IdPRegistrationSpec{
			Alias: "corp",
			Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
			OIDC: &pmcorev1alpha1.IdPRegistrationOIDCConfig{
				ClientID: "client",
				ClientSecretRef: pmcorev1alpha1.IdPRegistrationSecretRef{
					Name: "upstream-secret",
				},
				DiscoveryURL: "https://idp.example.com/.well-known/openid-configuration",
			},
		},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "metadata.name")
}

func TestIdPRegistrationValidator_ValidateUpdate(t *testing.T) {
	t.Parallel()

	enabled := true
	valid := &pmcorev1alpha1.IdPRegistration{
		ObjectMeta: metav1.ObjectMeta{Name: "corp"},
		Spec: pmcorev1alpha1.IdPRegistrationSpec{
			Alias:       "corp",
			DisplayName: "Corp IdP",
			Enabled:     &enabled,
			Type:        pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
			OIDC: &pmcorev1alpha1.IdPRegistrationOIDCConfig{
				ClientID: "client",
				ClientSecretRef: pmcorev1alpha1.IdPRegistrationSecretRef{
					Name: "upstream-secret",
				},
				DiscoveryURL: "https://idp.example.com/.well-known/openid-configuration",
			},
		},
	}

	tests := []struct {
		name            string
		mutate          func(*pmcorev1alpha1.IdPRegistration)
		wantErrContains string
	}{
		{
			name: "rejects alias change",
			mutate: func(reg *pmcorev1alpha1.IdPRegistration) {
				reg.Spec.Alias = "other"
			},
			wantErrContains: "spec.alias is immutable",
		},
		{
			name: "rejects type change",
			mutate: func(reg *pmcorev1alpha1.IdPRegistration) {
				reg.Spec.Type = "saml"
			},
			wantErrContains: "spec.type is immutable",
		},
		{
			name: "allows displayName change",
			mutate: func(reg *pmcorev1alpha1.IdPRegistration) {
				reg.Spec.DisplayName = "Updated Corp IdP"
			},
		},
		{
			name: "allows enabled change",
			mutate: func(reg *pmcorev1alpha1.IdPRegistration) {
				disabled := false
				reg.Spec.Enabled = &disabled
			},
		},
		{
			name: "allows OIDC endpoint change",
			mutate: func(reg *pmcorev1alpha1.IdPRegistration) {
				reg.Spec.OIDC.DiscoveryURL = "https://idp.example.com/realms/corp/.well-known/openid-configuration"
			},
		},
	}

	v := &idpRegistrationValidator{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			newObj := valid.DeepCopy()
			tt.mutate(newObj)

			_, err := v.ValidateUpdate(t.Context(), valid.DeepCopy(), newObj)
			if tt.wantErrContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrContains)
				return
			}
			require.NoError(t, err)
		})
	}
}
