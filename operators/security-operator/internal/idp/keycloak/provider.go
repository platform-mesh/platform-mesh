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
	"net/url"
	"strings"

	"go.platform-mesh.io/security-operator/internal/idp"
	"go.platform-mesh.io/security-operator/internal/idp/dcr"
)

const maxErrorBodySize = 4096

type AdminClient struct {
	httpClient *http.Client
	cfg        Config
	baseURL    string
}

var (
	_ idp.Provider = (*AdminClient)(nil)
)

func New(httpClient *http.Client, baseURL string, cfg Config) *AdminClient {
	return &AdminClient{
		httpClient: httpClient,
		cfg:        cfg,
		baseURL:    strings.TrimSuffix(baseURL, "/"),
	}
}

func (c *AdminClient) CreateTokenProvider(orgID string) dcr.TokenProviderFunc {
	return func(ctx context.Context) (string, error) {
		url := fmt.Sprintf("/admin/realms/%s/clients-initial-access", orgID)

		responseData, _, err := c.doRequest(ctx, http.MethodPost, url, []byte("{}"))
		if err != nil {
			return "", fmt.Errorf("failed to request initial access token: %w", err)
		}

		var response struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(responseData, &response); err != nil {
			return "", fmt.Errorf("failed to parse initial access token response: %w", err)
		}

		return response.Token, nil
	}
}

func (c *AdminClient) CreateTokenRefresher(orgID string) dcr.TokenRefresherFunc {
	return func(ctx context.Context, clientID string) (newToken string, err error) {
		clientUUID, err := c.GetClientByID(ctx, orgID, clientID)
		if err != nil {
			return "", err
		}

		url := fmt.Sprintf("/admin/realms/%s/clients/%s/registration-access-token", orgID, clientUUID)

		responseData, statusCode, err := c.doRequest(ctx, http.MethodPost, url, nil)
		if err != nil {
			return "", fmt.Errorf("failed to request initial access token: %w", err)
		}

		if statusCode != http.StatusOK && statusCode != http.StatusCreated {
			return "", errorResponse(responseData, statusCode, "regenerate registration access token")
		}

		var response struct {
			RegistrationAccessToken string `json:"registrationAccessToken"`
		}
		if err := json.Unmarshal(responseData, &response); err != nil {
			return "", fmt.Errorf("failed to parse token regeneration response: %w", err)
		}

		return response.RegistrationAccessToken, nil
	}
}

func (c *AdminClient) RegistrationEndpoint(orgID string, clientID string) string {
	url := fmt.Sprintf("%s/realms/%s/clients-registrations/openid-connect", c.baseURL, orgID)

	if clientID != "" {
		url += "/" + clientID
	}

	return url
}

func (c *AdminClient) OrganizationExists(ctx context.Context, orgID string) (bool, error) {
	url := fmt.Sprintf("/admin/realms/%s", orgID)

	responseData, statusCode, err := c.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("failed to request initial access token: %w", err)
	}

	switch statusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, errorResponse(responseData, statusCode, "get realm")
	}
}

func (c *AdminClient) buildRealmConfig(orgID string, cfg idp.OrganizationConfig) ([]byte, error) {
	ttl := int(c.cfg.AccessTokenLifespan.Seconds())

	realmConfig := realmConfig{
		Realm:                       orgID,
		DisplayName:                 orgID,
		Enabled:                     true,
		LoginWithEmailAllowed:       true,
		RegistrationEmailAsUsername: true,
		RegistrationAllowed:         cfg.RegistrationAllowed,
		SSOSessionIdleTimeout:       ttl,
		AccessTokenLifespan:         ttl,
		SMTPServer:                  c.cfg.SMTP,
	}

	return json.Marshal(realmConfig)
}

func (c *AdminClient) CreateOrganization(ctx context.Context, orgID string, cfg idp.OrganizationConfig) error {
	body, err := c.buildRealmConfig(orgID, cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal realm config: %w", err)
	}

	responseData, statusCode, err := c.doRequest(ctx, http.MethodPost, "/admin/realms", body)
	if err != nil {
		return fmt.Errorf("failed to do request: %w", err)
	}

	if statusCode == http.StatusCreated {
		return nil
	}

	return errorResponse(responseData, statusCode, "create realm")
}

func (c *AdminClient) UpdateOrganization(ctx context.Context, orgID string, cfg idp.OrganizationConfig) error {
	body, err := c.buildRealmConfig(orgID, cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal realm config: %w", err)
	}

	responseData, statusCode, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("/admin/realms/%s", orgID), body)
	if err != nil {
		return fmt.Errorf("failed to do request: %w", err)
	}

	if statusCode == http.StatusNoContent || statusCode == http.StatusOK {
		return nil
	}

	return errorResponse(responseData, statusCode, "update realm")
}

func (c *AdminClient) EnsureOrganization(ctx context.Context, orgID string, cfg idp.OrganizationConfig) (created bool, err error) {
	body, err := c.buildRealmConfig(orgID, cfg)
	if err != nil {
		return false, fmt.Errorf("failed to marshal realm config: %w", err)
	}

	// try creating first
	_, statusCode, err := c.doRequest(ctx, http.MethodPost, "/admin/realms", body)
	if err != nil {
		return false, fmt.Errorf("failed to do request: %w", err)
	}

	if statusCode == http.StatusCreated {
		return true, nil
	}

	// exists already, so we update instead
	return false, c.UpdateOrganization(ctx, orgID, cfg)
}

func (c *AdminClient) DeleteOrganization(ctx context.Context, orgID string) error {
	url := fmt.Sprintf("/admin/realms/%s", orgID)

	responseData, statusCode, err := c.doRequest(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("failed to do request: %w", err)
	}

	if statusCode == http.StatusNoContent || statusCode == http.StatusOK || statusCode == http.StatusNotFound {
		return nil
	}

	return errorResponse(responseData, statusCode, "delete realm")
}

func (c *AdminClient) ClientExists(ctx context.Context, orgID string, clientName string) (bool, error) {
	client, err := c.getClient(ctx, orgID, func(client idp.Client) bool {
		return client.Name == clientName
	})
	if err != nil {
		return false, err
	}

	return client != nil, nil
}

// TODO: Harmonize: ByName() returns an error if nothing was found, but ByID() only returns nil. Both should be behaving similarily.
func (c *AdminClient) GetClientByName(ctx context.Context, orgID string, clientName string) (*idp.Client, error) {
	return c.getClient(ctx, orgID, func(client idp.Client) bool {
		return client.Name == clientName
	})
}

func (c *AdminClient) GetClientByID(ctx context.Context, orgID string, clientID string) (*idp.Client, error) {
	client, err := c.getClient(ctx, orgID, func(client idp.Client) bool {
		return client.ID == clientID
	})
	if err != nil {
		return nil, err
	}

	if client == nil {
		return nil, fmt.Errorf("client with client_id %q not found", clientID)
	}

	return client, nil
}

func (c *AdminClient) getClient(ctx context.Context, orgID string, pred func(idp.Client) bool) (*idp.Client, error) {
	clients, err := c.ListClients(ctx, orgID)
	if err != nil {
		return nil, err
	}

	for _, client := range clients {
		if pred(client) {
			return &client, nil
		}
	}

	return nil, nil
}

func (c *AdminClient) ListClients(ctx context.Context, orgID string) ([]idp.Client, error) {
	url := fmt.Sprintf("/admin/realms/%s/clients", orgID)

	responseData, statusCode, err := c.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to do request: %w", err)
	}

	if statusCode != http.StatusOK {
		return nil, errorResponse(responseData, statusCode, "get clients")
	}

	var clients []idp.Client
	if err := json.Unmarshal(responseData, &clients); err != nil {
		return nil, fmt.Errorf("failed to parse clients response: %w", err)
	}

	return clients, nil
}

func (c *AdminClient) CreateServiceAccountClient(ctx context.Context, orgID string, config idp.ServiceAccountClientConfig) (*idp.Client, error) {
	url := fmt.Sprintf("%s/admin/realms/%s/clients", c.baseURL, orgID)

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

	return &idp.Client{
		ID:       clientUUID,
		ClientID: config.ClientID,
		Name:     config.Name,
	}, nil
}

func (c *AdminClient) GetClientSecret(ctx context.Context, orgID string, clientID string) (string, error) {
	// resolve public ID to internal UUID
	client, err := c.GetClientByID(ctx, orgID, clientID)
	if err != nil {
		return "", fmt.Errorf("failed to lookup client: %w", err)
	}

	url := fmt.Sprintf("/admin/realms/%s/clients/%s/client-secret", orgID, client.ID)

	responseData, statusCode, err := c.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to do request: %w", err)
	}

	if statusCode != http.StatusOK {
		return "", errorResponse(responseData, statusCode, "get client secret")
	}

	var result struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(responseData, &result); err != nil {
		return "", fmt.Errorf("failed to parse client secret response: %w", err)
	}

	return result.Value, nil
}

func (c *AdminClient) GetServiceAccountUser(ctx context.Context, orgID string, clientID string) (*idp.User, error) {
	// resolve public ID to internal UUID
	client, err := c.GetClientByID(ctx, orgID, clientID)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup client: %w", err)
	}

	url := fmt.Sprintf("/admin/realms/%s/clients/%s/service-account-user", orgID, client.ID)

	responseData, statusCode, err := c.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to do request: %w", err)
	}

	if statusCode != http.StatusOK {
		return nil, errorResponse(responseData, statusCode, "get service account user")
	}

	var user user
	if err := json.Unmarshal(responseData, &user); err != nil {
		return nil, fmt.Errorf("failed to parse service account user response: %w", err)
	}

	return user.ToPublic(), nil
}

func (c *AdminClient) GetOrganizationRole(ctx context.Context, orgID string, roleName string) (*idp.Role, error) {
	url := fmt.Sprintf("/admin/realms/%s/roles/%s", orgID, roleName)

	responseData, statusCode, err := c.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to do request: %w", err)
	}

	if statusCode == http.StatusNotFound {
		return nil, nil
	}
	if statusCode != http.StatusOK {
		return nil, errorResponse(responseData, statusCode, "get role")
	}

	var role role
	if err := json.Unmarshal(responseData, &role); err != nil {
		return nil, fmt.Errorf("failed to parse role response: %w", err)
	}

	return role.ToPublic(), nil
}

func (c *AdminClient) AssignRoleToUser(ctx context.Context, orgID string, userID string, r idp.Role) error {
	url := fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/realm", orgID, userID)

	body, err := json.Marshal([]role{createRole(r)})
	if err != nil {
		return fmt.Errorf("failed to marshal role: %w", err)
	}

	responseData, statusCode, err := c.doRequest(ctx, http.MethodPost, url, body)
	if err != nil {
		return fmt.Errorf("failed to do request: %w", err)
	}

	if statusCode == http.StatusNoContent || statusCode == http.StatusOK {
		return nil
	}

	return errorResponse(responseData, statusCode, "assign role to user")
}

func (c *AdminClient) GetClient(ctx context.Context, orgID string, clientID, registrationURI, registrationToken string) (dcr.ClientInformation, error) {
	return c.getDCRClient(orgID).Read(ctx, clientID, registrationURI, registrationToken)
}

func (c *AdminClient) CreateClient(ctx context.Context, orgID string, metadata dcr.ClientMetadata) (dcr.ClientInformation, error) {
	return c.getDCRClient(orgID).Register(ctx, c.RegistrationEndpoint(orgID, ""), metadata)
}

func (c *AdminClient) UpdateClient(ctx context.Context, orgID string, registrationURI, registrationToken string, metadata dcr.ClientMetadata) (dcr.ClientInformation, error) {
	return c.getDCRClient(orgID).Update(ctx, registrationURI, registrationToken, metadata)
}

func (c *AdminClient) DeleteClient(ctx context.Context, orgID string, clientID, registrationURI, registrationToken string) error {
	return c.getDCRClient(orgID).Delete(ctx, clientID, registrationURI, registrationToken)
}

func (c *AdminClient) getDCRClient(orgID string) dcr.Client {
	httpClient := &http.Client{
		Timeout:   c.cfg.HttpClientTimeoutSeconds,
		Transport: dcr.NewRetryTransport(nil, c.CreateTokenRefresher(orgID)),
	}

	return dcr.NewClient(
		dcr.WithHTTPClient(httpClient),
		dcr.WithTokenProvider(c.CreateTokenProvider(orgID)),
	)
}

func (c *AdminClient) GetUserByEmail(ctx context.Context, orgID, email string) (*idp.User, error) {
	users, err := c.listUsers(ctx, orgID, url.Values{
		"email":               {email},
		"max":                 {"1"},
		"briefRepresentation": {"true"},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to do request: %w", err)
	}

	if len(users) == 0 {
		return nil, nil
	}

	return users[0].ToPublic(), nil
}

func (c *AdminClient) ListUsers(ctx context.Context, orgID string) ([]idp.User, error) {
	users, err := c.listUsers(ctx, orgID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to do request: %w", err)
	}

	publicUsers := make([]idp.User, 0, len(users))
	for _, u := range users {
		public := u.ToPublic()
		publicUsers = append(publicUsers, *public)
	}

	return publicUsers, nil
}

func (c *AdminClient) listUsers(ctx context.Context, orgID string, query url.Values) ([]user, error) {
	url := fmt.Sprintf("/admin/realms/%s/users?%s", orgID, query.Encode())

	responseData, statusCode, err := c.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to do request: %w", err)
	}

	if statusCode != http.StatusOK {
		return nil, errorResponse(responseData, statusCode, "get users")
	}

	var users []user
	if err = json.Unmarshal(responseData, &users); err != nil {
		return nil, err
	}

	return users, nil
}

func (c *AdminClient) IssuerURL(orgID string) string {
	return fmt.Sprintf("%s/realms/%s", c.baseURL, orgID)
}

func (c *AdminClient) JWKSURL(orgID string) string {
	return fmt.Sprintf("%s/realms/%s/protocol/openid-connect/certs", c.baseURL, orgID)
}

func (c *AdminClient) AuthorizationEndpoint(orgID string) string {
	return fmt.Sprintf("%s/realms/%s/protocol/openid-connect/auth", c.baseURL, orgID)
}

func (c *AdminClient) TokenEndpoint(orgID string) string {
	return fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token", c.baseURL, orgID)
}

func (c *AdminClient) CreateUser(ctx context.Context, orgID string, clientID string, email string, inviteLink string) error {
	newUser := user{
		Email:           email,
		RequiredActions: []string{RequiredActionUpdatePassword, RequiredActionVerifyEmail},
		Enabled:         true,
	}

	if c.cfg.SetDefaultPassword {
		newUser.RequiredActions = []string{RequiredActionUpdatePassword}
		newUser.Credentials = []userCredential{{
			Type:      UserDefaultPasswordType,
			Value:     UserDefaultPasswordValue,
			Temporary: true,
		}}
	}

	// create users
	var buffer bytes.Buffer
	if err := json.NewEncoder(&buffer).Encode(&newUser); err != nil {
		return err
	}

	u := fmt.Sprintf("/admin/realms/%s/users", orgID)

	responseData, statusCode, err := c.doRequest(ctx, http.MethodPost, u, buffer.Bytes())
	if err != nil {
		return fmt.Errorf("failed to do request: %w", err)
	}

	if statusCode != http.StatusCreated {
		return errorResponse(responseData, statusCode, "create user")
	}

	// send invite email
	queryParams := url.Values{
		"redirect_uri": {inviteLink},
		"client_id":    {clientID},
	}

	u = fmt.Sprintf("/admin/realms/%s/users/%s/execute-actions-email?%s", orgID, newUser.ID, queryParams.Encode())

	_, statusCode, err = c.doRequest(ctx, http.MethodPut, u, nil)
	if err != nil {
		return fmt.Errorf("failed to do request: %w", err)
	}

	if statusCode != http.StatusNoContent {
		return errorResponse(responseData, statusCode, "send invite")
	}

	return nil
}

func (c *AdminClient) doRequest(ctx context.Context, method string, url string, body []byte) ([]byte, int, error) {
	fullUrl := c.baseURL + url

	req, err := http.NewRequestWithContext(ctx, method, fullUrl, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to do request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read response body: %w", err)
	}

	return content, resp.StatusCode, nil
}

func errorResponse(body []byte, statusCode int, operation string) error {
	return fmt.Errorf("failed to %s: status %d body: %s", operation, statusCode, body)
}

func readErrorResponse(resp *http.Response, operation string) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
	return errorResponse(body, resp.StatusCode, operation)
}
