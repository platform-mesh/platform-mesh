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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
)

// IdentityProviderRepresentation mirrors Keycloak's identity provider
// representation for the Admin REST API.
type IdentityProviderRepresentation struct {
	Alias                         string            `json:"alias"`
	DisplayName                   string            `json:"displayName,omitempty"`
	ProviderID                    string            `json:"providerId"`
	Enabled                       bool              `json:"enabled"`
	HideOnLogin                   bool              `json:"hideOnLogin,omitempty"`
	LinkOnly                      bool              `json:"linkOnly,omitempty"`
	StoreToken                    bool              `json:"storeToken,omitempty"`
	StoredTokensReadable          bool              `json:"storedTokensReadable,omitempty"`
	TrustEmail                    bool              `json:"trustEmail,omitempty"`
	GUIOrder                      int               `json:"guiOrder,omitempty"`
	VerifyEssentialClaim          bool              `json:"verifyEssentialClaim,omitempty"`
	EssentialClaim                string            `json:"essentialClaim,omitempty"`
	EssentialClaimValue           string            `json:"essentialClaimValue,omitempty"`
	FirstBrokerLoginFlowAlias     string            `json:"firstBrokerLoginFlowAlias,omitempty"`
	PostBrokerLoginFlowAlias      string            `json:"postBrokerLoginFlowAlias,omitempty"`
	SyncMode                      string            `json:"syncMode,omitempty"`
	CaseSensitiveOriginalUsername bool              `json:"caseSensitiveOriginalUsername,omitempty"`
	AddReadTokenRoleOnCreate      bool              `json:"addReadTokenRoleOnCreate,omitempty"`
	Config                        map[string]string `json:"config,omitempty"`
}

func (c *AdminClient) ListIdentityProviders(ctx context.Context) ([]IdentityProviderRepresentation, error) {
	url := fmt.Sprintf("%s/admin/realms/%s/identity-provider/instances", c.baseURL, c.realm)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create list identity providers request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list identity providers: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, readErrorResponse(resp, "list identity providers")
	}

	var providers []IdentityProviderRepresentation
	if err := json.NewDecoder(resp.Body).Decode(&providers); err != nil {
		return nil, fmt.Errorf("failed to parse identity providers response: %w", err)
	}

	return providers, nil
}

func (c *AdminClient) GetIdentityProvider(ctx context.Context, alias string) (*IdentityProviderRepresentation, error) {
	url := fmt.Sprintf("%s/admin/realms/%s/identity-provider/instances/%s", c.baseURL, c.realm, alias)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create get identity provider request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get identity provider %q: %w", alias, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, readErrorResponse(resp, "get identity provider")
	}

	var provider IdentityProviderRepresentation
	if err := json.NewDecoder(resp.Body).Decode(&provider); err != nil {
		return nil, fmt.Errorf("failed to parse identity provider response: %w", err)
	}

	return &provider, nil
}

func (c *AdminClient) CreateIdentityProvider(ctx context.Context, rep IdentityProviderRepresentation) error {
	body, err := json.Marshal(rep)
	if err != nil {
		return fmt.Errorf("failed to marshal identity provider: %w", err)
	}

	url := fmt.Sprintf("%s/admin/realms/%s/identity-provider/instances", c.baseURL, c.realm)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create identity provider request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to create identity provider: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return readErrorResponse(resp, "create identity provider")
	}

	return nil
}

func (c *AdminClient) UpdateIdentityProvider(ctx context.Context, alias string, rep IdentityProviderRepresentation) error {
	body, err := json.Marshal(rep)
	if err != nil {
		return fmt.Errorf("failed to marshal identity provider: %w", err)
	}

	url := fmt.Sprintf("%s/admin/realms/%s/identity-provider/instances/%s", c.baseURL, c.realm, alias)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create identity provider update request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to update identity provider: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return readErrorResponse(resp, "update identity provider")
	}

	return nil
}

func (c *AdminClient) ImportIdentityProviderConfig(
	ctx context.Context,
	providerID, fromURL string,
) (map[string]string, error) {
	body, err := json.Marshal(map[string]string{
		"providerId": providerID,
		"fromUrl":    fromURL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal import config request: %w", err)
	}

	url := fmt.Sprintf(
		"%s/admin/realms/%s/identity-provider/import-config",
		c.baseURL,
		c.realm,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create import config request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to import identity provider config: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, readErrorResponse(resp, "import identity provider config")
	}

	var config map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to parse import config response: %w", err)
	}

	return config, nil
}

// MergeImportedOIDCConfig overlays Keycloak import-config output onto a
// provider representation. Spec-controlled client credentials are preserved.
func MergeImportedOIDCConfig(
	rep *IdentityProviderRepresentation,
	imported map[string]string,
	preserveKeys ...string,
) {
	if rep.Config == nil {
		rep.Config = map[string]string{}
	}

	preserved := make(map[string]string, len(preserveKeys))
	for _, key := range preserveKeys {
		if value, ok := rep.Config[key]; ok {
			preserved[key] = value
		}
	}

	for key, value := range imported {
		rep.Config[key] = value
	}

	for key, value := range preserved {
		rep.Config[key] = value
	}
}

func (c *AdminClient) DeleteIdentityProvider(ctx context.Context, alias string) error {
	url := fmt.Sprintf("%s/admin/realms/%s/identity-provider/instances/%s", c.baseURL, c.realm, alias)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create identity provider delete request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete identity provider: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return readErrorResponse(resp, "delete identity provider")
	}

	return nil
}

// ToKeycloakIdentityProvider maps an upstream identity provider spec entry
// to a Keycloak identity provider representation.
func ToKeycloakIdentityProvider(
	upstream pmcorev1alpha1.UpstreamIdentityProvider,
	clientSecret string,
) (IdentityProviderRepresentation, error) {
	if upstream.OIDC == nil {
		return IdentityProviderRepresentation{}, fmt.Errorf("oidc config is required for provider %q", upstream.Alias)
	}

	rep := IdentityProviderRepresentation{
		Alias:       upstream.Alias,
		DisplayName: upstream.DisplayName,
		ProviderID:  string(upstream.Type),
		Enabled:     boolPtrOrDefault(upstream.Enabled, true),
		Config:      map[string]string{},
	}

	setBoolPtr(&rep.HideOnLogin, upstream.HideOnLoginPage)
	setBoolPtr(&rep.LinkOnly, upstream.AccountLinkingOnly)
	setBoolPtr(&rep.StoreToken, upstream.StoreTokens)
	setBoolPtr(&rep.StoredTokensReadable, upstream.StoredTokensReadable)
	setBoolPtr(&rep.TrustEmail, upstream.TrustEmail)
	setBoolPtr(&rep.VerifyEssentialClaim, upstream.VerifyEssentialClaim)
	setBoolPtr(&rep.CaseSensitiveOriginalUsername, upstream.CaseSensitiveUsername)

	if upstream.GUIOrder != nil {
		rep.GUIOrder = *upstream.GUIOrder
	}

	rep.EssentialClaim = upstream.EssentialClaim
	rep.EssentialClaimValue = upstream.EssentialClaimValue
	rep.FirstBrokerLoginFlowAlias = upstream.FirstLoginFlow
	rep.PostBrokerLoginFlowAlias = upstream.PostLoginFlow
	rep.SyncMode = upstream.SyncMode

	if upstream.ShowInAccountConsole != "" {
		rep.Config["showInAccountConsole"] = upstream.ShowInAccountConsole
	}

	oidc := upstream.OIDC
	cfg := rep.Config

	if oidc.DiscoveryURL == "" {
		setConfigString(cfg, "issuer", oidc.Issuer)
		setConfigString(cfg, "authorizationUrl", oidc.AuthorizationURL)
		setConfigString(cfg, "tokenUrl", oidc.TokenURL)
	}

	setConfigString(cfg, "logoutUrl", oidc.LogoutURL)
	setConfigString(cfg, "userInfoUrl", oidc.UserInfoURL)
	setConfigString(cfg, "clientId", oidc.ClientID)
	setConfigString(cfg, "clientSecret", clientSecret)
	setConfigString(cfg, "clientAuthMethod", oidc.ClientAuthentication)
	setConfigString(cfg, "clientAssertionSigningAlg", oidc.ClientAssertionSignatureAlgorithm)
	setConfigString(cfg, "clientAssertionAudience", oidc.ClientAssertionAudience)
	setConfigString(cfg, "defaultScope", oidc.DefaultScopes)
	setConfigString(cfg, "prompt", oidc.Prompt)
	setConfigString(cfg, "jwksUrl", oidc.JWKSURL)
	setConfigString(cfg, "publicKeySignatureVerifier", oidc.ValidatingPublicKey)
	setConfigString(cfg, "publicKeySignatureVerifierKeyId", oidc.ValidatingPublicKeyID)
	setConfigString(cfg, "forwardParameters", oidc.ForwardedQueryParameters)

	setConfigBoolPtr(cfg, "backchannelSupported", oidc.BackchannelLogout)
	setConfigBoolPtr(cfg, "acceptsPromptNoneForwardFromClient", oidc.AcceptsPromptNoneForwardFromClient)
	setConfigBoolPtr(cfg, "shortStateParameter", oidc.RequiresShortStateParameter)
	setConfigBoolPtr(cfg, "validateSignature", oidc.ValidateSignatures)
	setConfigBoolPtr(cfg, "useJwksUrl", oidc.UseJWKSURL)
	setConfigBoolPtr(cfg, "supportsClientAssertions", oidc.SupportsClientAssertions)
	setConfigBoolPtr(cfg, "allowClientAssertionsReuse", oidc.AllowsClientAssertionsReused)
	setConfigBoolPtr(cfg, "allowClientIdAsAudienceForClientAssertion", oidc.AllowsClientIDAsAudienceForAssertions)

	return rep, nil
}

func boolPtrOrDefault(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}

func setBoolPtr(dst *bool, src *bool) {
	if src != nil {
		*dst = *src
	}
}

func setConfigString(cfg map[string]string, key, value string) {
	if value != "" {
		cfg[key] = value
	}
}

func setConfigBoolPtr(cfg map[string]string, key string, src *bool) {
	if src != nil {
		cfg[key] = strconv.FormatBool(*src)
	}
}
