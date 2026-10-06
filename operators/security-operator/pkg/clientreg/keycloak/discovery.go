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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"go.platform-mesh.io/security-operator/internal/util"
)

// OIDCDiscovery holds endpoints resolved from an OIDC discovery document.
type OIDCDiscovery struct {
	Issuer           string
	AuthorizationURL string
	TokenURL         string
	JWKSURL          string
}

// discoveryHostValidator validates discovery URL hostnames before fetch. Tests may override it.
var discoveryHostValidator = util.ValidateOutboundHost

// FetchOIDCDiscovery retrieves and validates an OIDC discovery document.
func FetchOIDCDiscovery(ctx context.Context, httpClient *http.Client, discoveryURL string) (OIDCDiscovery, error) {
	discoveryURL = strings.TrimSpace(discoveryURL)
	if err := util.ValidateDiscoveryURL(discoveryURL); err != nil {
		return OIDCDiscovery{}, err
	}
	parsed, err := url.Parse(discoveryURL)
	if err != nil {
		return OIDCDiscovery{}, fmt.Errorf("invalid discovery URL: %w", err)
	}
	if err := discoveryHostValidator(ctx, parsed.Hostname()); err != nil {
		return OIDCDiscovery{}, fmt.Errorf("discovery URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return OIDCDiscovery{}, fmt.Errorf("creating discovery request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return OIDCDiscovery{}, fmt.Errorf("fetching discovery document: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return OIDCDiscovery{}, fmt.Errorf("discovery document returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var doc struct {
		Issuer                string `json:"issuer"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
		JWKSURI               string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return OIDCDiscovery{}, fmt.Errorf("parsing discovery document: %w", err)
	}

	if doc.Issuer == "" || doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" {
		return OIDCDiscovery{}, fmt.Errorf("discovery document missing required endpoints")
	}

	discovery := OIDCDiscovery{
		Issuer:           doc.Issuer,
		AuthorizationURL: doc.AuthorizationEndpoint,
		TokenURL:         doc.TokenEndpoint,
		JWKSURL:          doc.JWKSURI,
	}
	if err := util.ValidateOIDCEndpointURLs(discovery.Issuer, discovery.AuthorizationURL, discovery.TokenURL, discovery.JWKSURL); err != nil {
		return OIDCDiscovery{}, fmt.Errorf("discovery document returned unsafe endpoints: %w", err)
	}

	return discovery, nil
}
