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

package subroutines

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

const oidcTestProfile = `
infra: {}
components:
  services:
    infra:
      enabled: true
      values:
        kcp:
          auth:
            serviceAccount:
              enabled: true
    portal:
      enabled: true
      values:
        auth:
          default:
            clientId: welcome
            discoveryUrl: https://keycloak.example/realms/welcome/.well-known/openid-configuration
`

func Test_oidcServiceValues(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		oidc         pmcorev1alpha1.OIDCAuthentication
		kcpOIDC      map[string]any
		discoveryURL string
	}{
		"issuer and client only leave claims and prefixes to the chart": {
			oidc: pmcorev1alpha1.OIDCAuthentication{
				IssuerURL: "https://issuer.example/dex",
				ClientID:  "platform-mesh",
			},
			kcpOIDC: map[string]any{
				"enabled":   true,
				"issuerUrl": "https://issuer.example/dex",
				"clientID":  "platform-mesh",
			},
			discoveryURL: "https://issuer.example/dex/.well-known/openid-configuration",
		},
		"claims and prefixes are handed on": {
			oidc: pmcorev1alpha1.OIDCAuthentication{
				IssuerURL:      "https://issuer.example",
				ClientID:       "platform-mesh",
				UsernameClaim:  "sub",
				GroupsClaim:    "roles",
				UsernamePrefix: "oidc:",
				GroupsPrefix:   "oidc-groups:",
			},
			kcpOIDC: map[string]any{
				"enabled":        true,
				"issuerUrl":      "https://issuer.example",
				"clientID":       "platform-mesh",
				"usernameClaim":  "sub",
				"groupsClaim":    "roles",
				"usernamePrefix": "oidc:",
				"groupsPrefix":   "oidc-groups:",
			},
			discoveryURL: "https://issuer.example/.well-known/openid-configuration",
		},
		"trailing slash of the issuer is not doubled in the discovery URL": {
			oidc: pmcorev1alpha1.OIDCAuthentication{
				IssuerURL: "https://issuer.example/",
				ClientID:  "platform-mesh",
			},
			kcpOIDC: map[string]any{
				"enabled":   true,
				"issuerUrl": "https://issuer.example/",
				"clientID":  "platform-mesh",
			},
			discoveryURL: "https://issuer.example/.well-known/openid-configuration",
		},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()

			values := oidcServiceValues(&cas.oidc)

			assert.Equal(t, map[string]map[string]any{
				"infra": {
					"values": map[string]any{
						"kcp": map[string]any{
							"auth": map[string]any{
								"oidc": cas.kcpOIDC,
							},
						},
					},
				},
				"portal": {
					"values": map[string]any{
						"auth": map[string]any{
							"default": map[string]any{
								"discoveryUrl": cas.discoveryURL,
							},
						},
					},
				},
			}, values)
		})
	}
}

// oidcTestInstance returns a subroutine and a PlatformMesh with Keycloak
// disabled and an OIDC issuer set, on top of profileYAML.
func oidcTestInstance(t *testing.T, profileYAML string) (*DeploymentSubroutine, *pmcorev1alpha1.PlatformMesh) {
	t.Helper()

	sub, inst := newSubroutineWithProfile(t, profileYAML, "test-pm", "test-ns", config.RemoteClusterConfig{})
	sub.cfgOperator.KCP = config.KCPConfig{
		Namespace:      "platform-mesh-system",
		FrontProxyName: "frontproxy",
		FrontProxyPort: "8443",
	}
	inst.Spec.FeatureToggles = []pmcorev1alpha1.FeatureToggle{
		{Name: FeatureDisableKeycloak},
	}
	inst.Spec.Authentication = &pmcorev1alpha1.Authentication{
		OIDC: &pmcorev1alpha1.OIDCAuthentication{
			IssuerURL: "https://issuer.example/dex",
			ClientID:  "platform-mesh",
		},
	}
	return sub, inst
}

// servicesOf returns the merged services of the component template vars of inst.
func servicesOf(t *testing.T, sub *DeploymentSubroutine, inst *pmcorev1alpha1.PlatformMesh) map[string]any {
	t.Helper()

	result, _, err := sub.buildComponentsTemplateVars(context.Background(), inst, apiextensionsv1.JSON{})
	require.NoError(t, err)
	values, ok := result["values"].(map[string]any)
	require.True(t, ok, "values is a map")
	services, ok := values["services"].(map[string]any)
	require.True(t, ok, "services is a map")
	return services
}

func Test_buildComponentsTemplateVars_OIDCAuthentication(t *testing.T) {
	t.Parallel()

	expectedExposure := map[string]any{
		"baseDomain":       "portal.localhost",
		"protocol":         "https",
		"port":             8443,
		"gatewayClassName": "traefik",
	}
	const expectedFrontProxyURL = "https://frontproxy-front-proxy.platform-mesh-system:8443"

	t.Run("merges the issuer over the profile and keeps what the profile set", func(t *testing.T) {
		t.Parallel()

		sub, inst := oidcTestInstance(t, oidcTestProfile)
		services := servicesOf(t, sub, inst)

		infra, ok := services["infra"].(map[string]any)
		require.True(t, ok, "infra is a map")
		portal, ok := services["portal"].(map[string]any)
		require.True(t, ok, "portal is a map")

		assert.Equal(t, map[string]any{
			"exposure": expectedExposure,
			"kcp": map[string]any{
				"internalFrontProxyUrl": expectedFrontProxyURL,
				"auth": map[string]any{
					"serviceAccount": map[string]any{
						"enabled": true,
					},
					"oidc": map[string]any{
						"enabled":   true,
						"issuerUrl": "https://issuer.example/dex",
						"clientID":  "platform-mesh",
					},
				},
			},
		}, infra["values"], "service account authentication of the profile stays")
		assert.Equal(t, map[string]any{
			"exposure": expectedExposure,
			"kcp": map[string]any{
				"internalFrontProxyUrl": expectedFrontProxyURL,
			},
			"auth": map[string]any{
				"default": map[string]any{
					"clientId":     "welcome",
					"discoveryUrl": "https://issuer.example/dex/.well-known/openid-configuration",
				},
			},
		}, portal["values"], "the Keycloak discovery URL of the profile is replaced")
	})

	t.Run("spec.values overrides authentication settings", func(t *testing.T) {
		t.Parallel()

		sub, inst := oidcTestInstance(t, oidcTestProfile)
		raw, err := json.Marshal(map[string]any{
			"services": map[string]any{
				"infra": map[string]any{
					"values": map[string]any{
						"kcp": map[string]any{
							"auth": map[string]any{
								"oidc": map[string]any{
									"clientID": "from-spec-values",
								},
							},
						},
					},
				},
			},
		})
		require.NoError(t, err)
		inst.Spec.Values = apiextensionsv1.JSON{Raw: raw}

		services := servicesOf(t, sub, inst)

		infra, ok := services["infra"].(map[string]any)
		require.True(t, ok, "infra is a map")
		assert.Equal(t, map[string]any{
			"exposure": expectedExposure,
			"kcp": map[string]any{
				"internalFrontProxyUrl": expectedFrontProxyURL,
				"auth": map[string]any{
					"serviceAccount": map[string]any{
						"enabled": true,
					},
					"oidc": map[string]any{
						"enabled":   true,
						"issuerUrl": "https://issuer.example/dex",
						"clientID":  "from-spec-values",
					},
				},
			},
		}, infra["values"])
	})

	t.Run("a service absent from the profile stays absent", func(t *testing.T) {
		t.Parallel()

		sub, inst := oidcTestInstance(t, `
infra: {}
components:
  services:
    infra:
      enabled: true
      values: {}
`)
		services := servicesOf(t, sub, inst)

		assert.Contains(t, services, "infra")
		assert.NotContains(t, services, "portal")
	})

	t.Run("refused while Keycloak is not disabled", func(t *testing.T) {
		t.Parallel()

		sub, inst := oidcTestInstance(t, oidcTestProfile)
		inst.Spec.FeatureToggles = nil

		_, _, err := sub.buildComponentsTemplateVars(context.Background(), inst, apiextensionsv1.JSON{})

		require.Error(t, err)
		assert.ErrorContains(t, err, "spec.authentication.oidc needs the feature toggle "+FeatureDisableKeycloak)
	})

	t.Run("without spec.authentication the profile authentication is preserved", func(t *testing.T) {
		t.Parallel()

		sub, inst := oidcTestInstance(t, oidcTestProfile)
		inst.Spec.Authentication = nil

		services := servicesOf(t, sub, inst)

		portal, ok := services["portal"].(map[string]any)
		require.True(t, ok, "portal is a map")
		assert.Equal(t, map[string]any{
			"exposure": expectedExposure,
			"kcp": map[string]any{
				"internalFrontProxyUrl": expectedFrontProxyURL,
			},
			"auth": map[string]any{
				"default": map[string]any{
					"clientId":     "welcome",
					"discoveryUrl": "https://keycloak.example/realms/welcome/.well-known/openid-configuration",
				},
			},
		}, portal["values"])
	})
}
