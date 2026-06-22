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
	"io"
	"net/http"
	"strings"

	"go.platform-mesh.io/security-operator/pkg/clientreg"
)

const maxErrorBodySize = 4096

type AdminClient struct {
	httpClient *http.Client
	baseURL    string
	realm      string
}

func NewAdminClient(httpClient *http.Client, baseURL, realm string) *AdminClient {
	return &AdminClient{
		httpClient: httpClient,
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		realm:      realm,
	}
}

func (c *AdminClient) TokenForRegistration(ctx context.Context) (string, error) {
	url := fmt.Sprintf("%s/admin/realms/%s/clients-initial-access", c.baseURL, c.realm)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return "", fmt.Errorf("failed to create initial access token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to request initial access token: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return "", readErrorResponse(resp, "create initial access token")
	}

	var response struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("failed to parse initial access token response: %w", err)
	}

	return response.Token, nil
}

func (c *AdminClient) RefreshToken(ctx context.Context, clientID string) (string, error) {
	clientUUID, err := c.resolveClientUUID(ctx, clientID)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/admin/realms/%s/clients/%s/registration-access-token", c.baseURL, c.realm, clientUUID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create token regeneration request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to request token regeneration: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", readErrorResponse(resp, "regenerate registration access token")
	}

	var response struct {
		RegistrationAccessToken string `json:"registrationAccessToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("failed to parse token regeneration response: %w", err)
	}

	return response.RegistrationAccessToken, nil
}

func (c *AdminClient) RegistrationEndpoint() string {
	return fmt.Sprintf("%s/realms/%s/clients-registrations/openid-connect", c.baseURL, c.realm)
}

func (c *AdminClient) RealmExists(ctx context.Context, realmName string) (bool, error) {
	url := fmt.Sprintf("%s/admin/realms/%s", c.baseURL, realmName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create get realm request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to get realm %q: %w", realmName, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}

	return false, readErrorResponse(resp, "get realm")
}

func (c *AdminClient) CreateOrUpdateRealm(ctx context.Context, config RealmConfig) (created bool, err error) {
	body, err := json.Marshal(config)
	if err != nil {
		return false, fmt.Errorf("failed to marshal realm config: %w", err)
	}

	createURL := fmt.Sprintf("%s/admin/realms", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, createURL, bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("failed to create realm request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to create realm: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusCreated {
		return true, nil
	}

	if resp.StatusCode == http.StatusConflict {
		return false, c.updateRealm(ctx, config.Realm, body)
	}

	return false, readErrorResponse(resp, "create realm")
}

func (c *AdminClient) updateRealm(ctx context.Context, realmName string, body []byte) error {
	url := fmt.Sprintf("%s/admin/realms/%s", c.baseURL, realmName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create realm update request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to update realm: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return readErrorResponse(resp, "update realm")
	}

	return nil
}

func (c *AdminClient) DeleteRealm(ctx context.Context, realmName string) error {
	url := fmt.Sprintf("%s/admin/realms/%s", c.baseURL, realmName)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create realm delete request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete realm: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return readErrorResponse(resp, "delete realm")
	}

	return nil
}

func (c *AdminClient) GetClientByName(ctx context.Context, clientName string) (*ClientInfo, error) {
	clients, err := c.ListClients(ctx)
	if err != nil {
		return nil, err
	}

	for _, client := range clients {
		if client.Name == clientName {
			return &client, nil
		}
	}

	return nil, nil
}

func (c *AdminClient) resolveClientUUID(ctx context.Context, clientID string) (string, error) {
	clients, err := c.ListClients(ctx)
	if err != nil {
		return "", err
	}

	for _, client := range clients {
		if client.ClientID == clientID {
			return client.ID, nil
		}
	}

	return "", fmt.Errorf("client with client_id %q not found", clientID)
}

func (c *AdminClient) ListClients(ctx context.Context) ([]ClientInfo, error) {
	url := fmt.Sprintf("%s/admin/realms/%s/clients", c.baseURL, c.realm)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create get clients request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get clients: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, readErrorResponse(resp, "get clients")
	}

	var clients []ClientInfo
	if err := json.NewDecoder(resp.Body).Decode(&clients); err != nil {
		return nil, fmt.Errorf("failed to parse clients response: %w", err)
	}

	return clients, nil
}

func readErrorResponse(resp *http.Response, operation string) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
	return fmt.Errorf("failed to %s: status %d body: %s", operation, resp.StatusCode, body)
}

type RealmConfig struct {
	Realm                       string      `json:"realm"`
	DisplayName                 string      `json:"displayName,omitempty"`
	Enabled                     bool        `json:"enabled"`
	LoginWithEmailAllowed       bool        `json:"loginWithEmailAllowed,omitempty"`
	RegistrationEmailAsUsername bool        `json:"registrationEmailAsUsername,omitempty"`
	RegistrationAllowed         bool        `json:"registrationAllowed,omitempty"`
	SSOSessionIdleTimeout       int         `json:"ssoSessionIdleTimeout,omitempty"`
	AccessTokenLifespan         int         `json:"accessTokenLifespan,omitempty"`
	SMTPServer                  *SMTPConfig `json:"smtpServer,omitempty"`
}

type SMTPConfig struct {
	Host     string `json:"host,omitempty"`
	Port     string `json:"port,omitempty"`
	From     string `json:"from,omitempty"`
	SSL      bool   `json:"ssl,omitempty"`
	StartTLS bool   `json:"starttls,omitempty"`
	Auth     bool   `json:"auth,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
}

type ClientInfo struct {
	ID       string `json:"id"`
	ClientID string `json:"clientId"`
	Name     string `json:"name"`
	Secret   string `json:"secret"`
}

type ServiceAccountClientConfig struct {
	ClientID               string `json:"clientId"`
	Name                   string `json:"name,omitempty"`
	Enabled                bool   `json:"enabled"`
	ServiceAccountsEnabled bool   `json:"serviceAccountsEnabled"`
	PublicClient           bool   `json:"publicClient"`
}

type UserInfo struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type RoleInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *AdminClient) CreateServiceAccountClient(ctx context.Context, config ServiceAccountClientConfig) (*ClientInfo, error) {
	url := fmt.Sprintf("%s/admin/realms/%s/clients", c.baseURL, c.realm)

	body, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal client config: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create client request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusCreated {
		return nil, readErrorResponse(resp, "create client")
	}

	location := resp.Header.Get("Location")
	if location == "" {
		return nil, fmt.Errorf("no location header in create client response")
	}

	parts := strings.Split(location, "/")
	clientUUID := parts[len(parts)-1]

	return &ClientInfo{
		ID:       clientUUID,
		ClientID: config.ClientID,
		Name:     config.Name,
	}, nil
}

func (c *AdminClient) GetClientSecret(ctx context.Context, clientUUID string) (string, error) {
	url := fmt.Sprintf("%s/admin/realms/%s/clients/%s/client-secret", c.baseURL, c.realm, clientUUID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create get client secret request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get client secret: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return "", readErrorResponse(resp, "get client secret")
	}

	var result struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to parse client secret response: %w", err)
	}

	return result.Value, nil
}

func (c *AdminClient) GetServiceAccountUser(ctx context.Context, clientUUID string) (*UserInfo, error) {
	url := fmt.Sprintf("%s/admin/realms/%s/clients/%s/service-account-user", c.baseURL, c.realm, clientUUID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create get service account user request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get service account user: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, readErrorResponse(resp, "get service account user")
	}

	var user UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to parse service account user response: %w", err)
	}

	return &user, nil
}

func (c *AdminClient) GetRealmRole(ctx context.Context, roleName string) (*RoleInfo, error) {
	url := fmt.Sprintf("%s/admin/realms/%s/roles/%s", c.baseURL, c.realm, roleName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create get role request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get role: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, readErrorResponse(resp, "get role")
	}

	var role RoleInfo
	if err := json.NewDecoder(resp.Body).Decode(&role); err != nil {
		return nil, fmt.Errorf("failed to parse role response: %w", err)
	}

	return &role, nil
}

func (c *AdminClient) AssignRealmRoleToUser(ctx context.Context, userID string, role RoleInfo) error {
	url := fmt.Sprintf("%s/admin/realms/%s/users/%s/role-mappings/realm", c.baseURL, c.realm, userID)

	body, err := json.Marshal([]RoleInfo{role})
	if err != nil {
		return fmt.Errorf("failed to marshal role: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create assign role request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to assign role: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return readErrorResponse(resp, "assign role to user")
	}

	return nil
}

var (
	_ clientreg.TokenProvider  = (*AdminClient)(nil)
	_ clientreg.TokenRefresher = (*AdminClient)(nil)
)
