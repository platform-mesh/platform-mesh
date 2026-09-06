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
)

// OIDCDiscovery holds endpoints resolved from an OIDC discovery document.
type OIDCDiscovery struct {
	Issuer           string
	AuthorizationURL string
	TokenURL         string
	JWKSURL          string
}

// FetchOIDCDiscovery retrieves and validates an OIDC discovery document.
func FetchOIDCDiscovery(ctx context.Context, httpClient *http.Client, discoveryURL string) (OIDCDiscovery, error) {
	parsed, err := url.Parse(strings.TrimSpace(discoveryURL))
	if err != nil {
		return OIDCDiscovery{}, fmt.Errorf("invalid discovery URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return OIDCDiscovery{}, fmt.Errorf("discovery URL must use https")
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

	return OIDCDiscovery{
		Issuer:           doc.Issuer,
		AuthorizationURL: doc.AuthorizationEndpoint,
		TokenURL:         doc.TokenEndpoint,
		JWKSURL:          doc.JWKSURI,
	}, nil
}
