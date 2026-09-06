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
	"fmt"
	"strconv"
	"strings"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
)

const (
	platformTrustEmail         = false
	platformSyncMode           = "IMPORT"
	platformValidateSignatures = true
	platformUseJWKSURL         = true
	platformClientAuthMethod   = "client_secret_post"
)

// ToKeycloakIdentityProviderFromRegistration maps a tenant IdPRegistration to a
// Keycloak identity provider representation with platform-controlled defaults applied.
func ToKeycloakIdentityProviderFromRegistration(
	reg pmcorev1alpha1.IdPRegistration,
	clientSecret string,
	discovery *OIDCDiscovery,
) (IdentityProviderRepresentation, error) {
	if reg.Spec.OIDC == nil {
		return IdentityProviderRepresentation{}, fmt.Errorf("oidc config is required for provider %q", reg.Spec.Alias)
	}
	oidc := reg.Spec.OIDC

	rep := IdentityProviderRepresentation{
		Alias:       reg.Spec.Alias,
		DisplayName: reg.Spec.DisplayName,
		ProviderID:  string(reg.Spec.Type),
		Enabled:     boolPtrOrDefault(reg.Spec.Enabled, true),
		Config:      map[string]string{},
	}

	setBoolPtr(&rep.HideOnLogin, reg.Spec.HideOnLoginPage)
	applyPlatformIdentityProviderDefaults(&rep)

	if clientSecret != "" {
		rep.Config["clientSecret"] = clientSecret
	}
	if oidc.ClientID != "" {
		rep.Config["clientId"] = oidc.ClientID
	}
	rep.Config["clientAuthMethod"] = platformClientAuthMethod
	rep.Config["validateSignature"] = strconv.FormatBool(platformValidateSignatures)
	rep.Config["useJwksUrl"] = strconv.FormatBool(platformUseJWKSURL)

	if discovery != nil {
		rep.Config["issuer"] = discovery.Issuer
		rep.Config["authorizationUrl"] = discovery.AuthorizationURL
		rep.Config["tokenUrl"] = discovery.TokenURL
		if discovery.JWKSURL != "" {
			rep.Config["jwksUrl"] = discovery.JWKSURL
		}
	} else {
		if oidc.Issuer != "" {
			rep.Config["issuer"] = oidc.Issuer
		}
		if oidc.AuthorizationURL != "" {
			rep.Config["authorizationUrl"] = oidc.AuthorizationURL
		}
		if oidc.TokenURL != "" {
			rep.Config["tokenUrl"] = oidc.TokenURL
		}
		if oidc.JWKSURL != "" {
			rep.Config["jwksUrl"] = oidc.JWKSURL
		}
	}

	return rep, nil
}

func applyPlatformIdentityProviderDefaults(rep *IdentityProviderRepresentation) {
	rep.TrustEmail = platformTrustEmail
	if rep.Config == nil {
		rep.Config = map[string]string{}
	}
	// Keycloak 26+ rejects top-level syncMode on IdentityProviderRepresentation.
	rep.Config["syncMode"] = platformSyncMode
	rep.FirstBrokerLoginFlowAlias = ""
	rep.PostBrokerLoginFlowAlias = ""
}

// SyncIdentityProviderSpec replaces dst with desired platform-synced fields.
func SyncIdentityProviderSpec(dst *IdentityProviderRepresentation, desired IdentityProviderRepresentation) {
	alias := dst.Alias
	orgID := dst.OrganizationID
	*dst = desired
	dst.Alias = alias
	dst.OrganizationID = orgID
}

func RegistrationToUpstream(reg pmcorev1alpha1.IdPRegistration) pmcorev1alpha1.UpstreamIdentityProvider {
	up := pmcorev1alpha1.UpstreamIdentityProvider{
		Alias:              reg.Spec.Alias,
		DisplayName:        reg.Spec.DisplayName,
		Enabled:            reg.Spec.Enabled,
		HideOnLoginPage:    reg.Spec.HideOnLoginPage,
		EmailDomainRouting: reg.Spec.EmailDomainRouting,
		Type:               reg.Spec.Type,
	}
	return up
}

// BrokerRedirectURI returns the OAuth redirect URI tenants must allow at the upstream IdP.
func BrokerRedirectURI(baseDomain, realm, alias string) string {
	baseDomain = strings.TrimSpace(strings.TrimSuffix(baseDomain, "/"))
	realm = strings.TrimSpace(realm)
	alias = strings.TrimSpace(alias)
	if baseDomain == "" || realm == "" || alias == "" {
		return ""
	}
	return fmt.Sprintf("https://%s/keycloak/realms/%s/broker/%s/endpoint", baseDomain, realm, alias)
}
