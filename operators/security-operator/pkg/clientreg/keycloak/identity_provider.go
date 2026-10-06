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
	"strings"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/security-operator/internal/util"
)

// IdentityProviderRepresentation mirrors Keycloak's identity provider
// representation for the Admin REST API.
type IdentityProviderRepresentation struct {
	Alias                         string            `json:"alias"`
	DisplayName                   string            `json:"displayName,omitempty"`
	ProviderID                    string            `json:"providerId"`
	Enabled                       bool              `json:"enabled"`
	HideOnLogin                   bool              `json:"hideOnLogin,omitempty"`
	OrganizationID                string            `json:"organizationId,omitempty"`
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

// ClearOrganizationBrokerConfig removes Keycloak organization linkage from a broker.
func ClearOrganizationBrokerConfig(rep *IdentityProviderRepresentation) {
	rep.OrganizationID = ""
	if rep.Config == nil {
		return
	}
	delete(rep.Config, "kc.org.domain")
	delete(rep.Config, "kc.org.broker.redirect.mode.email-matches")
	delete(rep.Config, "kc.org.broker.login.hide-when-org-unknown")
}

// LinkIdentityProviderOrganization sets the Keycloak organization linkage on a
// broker representation, or clears it when no email domains are configured.
func LinkIdentityProviderOrganization(
	rep *IdentityProviderRepresentation,
	organizationID string,
	routing *pmcorev1alpha1.EmailDomainRouting,
	hideOnLoginPage *bool,
) {
	ClearOrganizationBrokerConfig(rep)

	if routing == nil {
		return
	}
	domains := util.NormalizeEmailDomains(routing.Domains)
	if len(domains) == 0 {
		return
	}

	if rep.Config == nil {
		rep.Config = map[string]string{}
	}

	rep.OrganizationID = organizationID
	setConfigString(rep.Config, "kc.org.domain", strings.Join(domains, ","))

	if boolPtrOrDefault(routing.AutoRedirect, false) {
		setConfigString(rep.Config, "kc.org.broker.redirect.mode.email-matches", "true")
		if hideOnLoginPage == nil {
			rep.HideOnLogin = true
		}
	}

	if boolPtrOrDefault(routing.HideUntilDomainMatch, false) {
		setConfigString(rep.Config, "kc.org.broker.login.hide-when-org-unknown", "true")
	}
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
