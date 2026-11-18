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
	"strings"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/pkg/merge"
)

const oidcDiscoveryPath = "/.well-known/openid-configuration"

// kubectlClientName is the name consumers of an AccountInfo look the client of
// kubeconfigs up by, as the pm-keycloak-operator names it in a realm.
const kubectlClientName = "kubectl"

// applyOIDCAuthentication merges the values for inst's spec.authentication.oidc
// over the services the profile has, in place.
// Refused without FeatureDisableKeycloak: Keycloak and an external issuer are alternatives.
func applyOIDCAuthentication(inst *pmcorev1alpha1.PlatformMesh, services map[string]any, log *logger.Logger) error {
	if inst.Spec.Authentication == nil || inst.Spec.Authentication.OIDC == nil {
		return nil
	}
	if !HasFeatureToggle(inst, FeatureDisableKeycloak) {
		return errors.New("spec.authentication.oidc needs the feature toggle %s: Keycloak and an external OIDC issuer are alternatives", FeatureDisableKeycloak)
	}

	for name, values := range oidcServiceValues(inst.Spec.Authentication.OIDC) {
		service, ok := services[name].(map[string]any)
		if !ok {
			continue
		}
		merged, err := merge.MergeMaps(service, values, log)
		if err != nil {
			return errors.Wrap(err, "Failed to merge spec.authentication.oidc into service %s", name)
		}
		services[name] = merged
	}
	return nil
}

// oidcServiceValues returns what makes kcp, the portal and the account-operator
// use oidc, keyed by profile service.
// Claims and prefixes left empty are omitted, so the charts' defaults apply.
func oidcServiceValues(oidc *pmcorev1alpha1.OIDCAuthentication) map[string]map[string]any {
	kcpOIDC := map[string]any{
		"enabled":   true,
		"issuerUrl": oidc.IssuerURL,
		"clientID":  oidc.ClientID,
	}
	optional := map[string]string{
		"usernameClaim":  oidc.UsernameClaim,
		"groupsClaim":    oidc.GroupsClaim,
		"usernamePrefix": oidc.UsernamePrefix,
		"groupsPrefix":   oidc.GroupsPrefix,
	}
	for key, value := range optional {
		if value != "" {
			kcpOIDC[key] = value
		}
	}

	// What the account-operator publishes in every AccountInfo, for the
	// kubeconfigs the portal hands out. Keycloak's operator does that per
	// organization; with an external issuer it is the same everywhere.
	kubectlClientID := oidc.KubectlClientID
	if kubectlClientID == "" {
		kubectlClientID = oidc.ClientID
	}
	accountOIDC := map[string]any{
		"issuerUrl": oidc.IssuerURL,
		"clients":   map[string]any{kubectlClientName: kubectlClientID},
	}
	if oidc.KubectlClientSecret != "" {
		accountOIDC["clientSecrets"] = map[string]any{kubectlClientName: oidc.KubectlClientSecret}
	}
	if len(oidc.KubectlScopes) > 0 {
		scopes := make([]any, 0, len(oidc.KubectlScopes))
		for _, scope := range oidc.KubectlScopes {
			scopes = append(scopes, scope)
		}
		accountOIDC["scopes"] = scopes
	}

	return map[string]map[string]any{
		"account-operator": {
			"values": map[string]any{
				"oidc": accountOIDC,
			},
		},
		"infra": {
			"values": map[string]any{
				"kcp": map[string]any{
					"auth": map[string]any{
						"oidc": kcpOIDC,
					},
				},
			},
		},
		"portal": {
			"values": map[string]any{
				"auth": map[string]any{
					"default": map[string]any{
						"discoveryUrl": strings.TrimSuffix(oidc.IssuerURL, "/") + oidcDiscoveryPath,
					},
				},
			},
		},
	}
}
