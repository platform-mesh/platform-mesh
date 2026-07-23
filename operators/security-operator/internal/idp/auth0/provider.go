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
package auth0

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/auth0/go-auth0/v2/management"
	mgmtclient "github.com/auth0/go-auth0/v2/management/client"
	"github.com/auth0/go-auth0/v2/management/core"
	"github.com/auth0/go-auth0/v2/management/option"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"go.platform-mesh.io/security-operator/internal/idp"
	"go.platform-mesh.io/security-operator/internal/idp/dcr"
)

const defaultUserConnection = "Username-Password-Authentication"

// dcr.TokenRefresher is not implemented and not needed. Auth0 never builds a RetryTransport, so nothing consumes RefreshToken(ctx, clientID)
var (
	_ idp.Provider       = (*ManagementClient)(nil)
	_ oauth2.TokenSource = managementTokenSource{}
)

type ManagementClient struct {
	mgmt         *mgmtclient.Management
	baseURL      string
	oauth2Config clientcredentials.Config

	mu sync.Mutex
	ts oauth2.TokenSource
}

func NewManagementClient(ctx context.Context, baseURL, clientID, clientSecret string, opts ...option.RequestOption) *ManagementClient {
	baseURL = strings.TrimSuffix(baseURL, "/")
	if !strings.Contains(baseURL, "://") {
		baseURL = "https://" + baseURL
	}

	c := &ManagementClient{
		baseURL: baseURL,
		oauth2Config: clientcredentials.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			TokenURL:     baseURL + "/oauth/token",
			AuthStyle:    oauth2.AuthStyleInParams,
			EndpointParams: url.Values{
				"audience": {baseURL + "/api/v2/"},
			},
		},
	}
	c.ts = c.oauth2Config.TokenSource(ctx)

	options := append([]option.RequestOption{
		option.WithBaseURL(baseURL + "/api/v2"),
		option.WithTokenSource(managementTokenSource{c}),
	}, opts...)
	c.mgmt = mgmtclient.NewWithOptions(options...)

	return c
}

func (c *ManagementClient) token() (*oauth2.Token, error) {
	c.mu.Lock()
	ts := c.ts
	c.mu.Unlock()
	return ts.Token()
}

type managementTokenSource struct {
	c *ManagementClient
}

func (s managementTokenSource) Token() (*oauth2.Token, error) {
	return s.c.token()
}

func (c *ManagementClient) CreateTokenProvider(_ string) dcr.TokenProviderFunc {
	return func(_ context.Context) (string, error) {
		token, err := c.token()
		if err != nil {
			return "", fmt.Errorf("failed to get management token: %w", err)
		}
		return token.AccessToken, nil
	}
}

func (c *ManagementClient) dcrClient() dcr.Client {
	return dcr.NewClient(dcr.WithTokenProvider(c.CreateTokenProvider("")))
}

func (c *ManagementClient) RegistrationEndpoint(orgID string, clientID string) string {
	return c.baseURL + "/oidc/register"
}

func (c *ManagementClient) CreateClient(ctx context.Context, orgID string, metadata dcr.ClientMetadata) (dcr.ClientInformation, error) {
	return c.dcrClient().Register(ctx, c.RegistrationEndpoint(orgID, metadata.ClientID), metadata)
}

func (c *ManagementClient) GetClient(ctx context.Context, orgID string, clientID, registrationURI, registrationToken string) (dcr.ClientInformation, error) {
	return c.dcrClient().Read(ctx, clientID, registrationURI, registrationToken)
}

func (c *ManagementClient) UpdateClient(ctx context.Context, orgID string, registrationURI, registrationToken string, metadata dcr.ClientMetadata) (dcr.ClientInformation, error) {
	return c.dcrClient().Update(ctx, registrationURI, registrationToken, metadata)
}

func (c *ManagementClient) DeleteClient(ctx context.Context, orgID string, clientID, registrationURI, registrationToken string) error {
	return c.dcrClient().Delete(ctx, clientID, registrationURI, registrationToken)
}

func (c *ManagementClient) CreateOrganization(ctx context.Context, orgID string, _ idp.OrganizationConfig) error {
	if _, err := c.mgmt.Organizations.Create(ctx, &management.CreateOrganizationRequestContent{
		Name: orgID,
	}); err != nil {
		return fmt.Errorf("failed to create organization %q: %w", orgID, err)
	}
	return nil
}

func (c *ManagementClient) UpdateOrganization(ctx context.Context, orgID string, _ idp.OrganizationConfig) error {
	org, err := c.getOrganizationByName(ctx, orgID)
	if err != nil {
		return err
	}
	if org == nil {
		return fmt.Errorf("organization %q not found for update", orgID)
	}

	name := orgID
	if _, err := c.mgmt.Organizations.Update(ctx, deref(org.ID), &management.UpdateOrganizationRequestContent{
		Name: &name,
	}); err != nil {
		return fmt.Errorf("failed to update organization %q: %w", orgID, err)
	}

	return nil
}

func (c *ManagementClient) EnsureOrganization(ctx context.Context, orgID string, cfg idp.OrganizationConfig) (created bool, err error) {
	_, err = c.mgmt.Organizations.Create(ctx, &management.CreateOrganizationRequestContent{
		Name: orgID,
	})
	if err == nil {
		return true, nil
	}
	if !isStatus(err, http.StatusConflict) {
		return false, fmt.Errorf("failed to create organization %q: %w", orgID, err)
	}

	return false, c.UpdateOrganization(ctx, orgID, cfg)
}

func (c *ManagementClient) DeleteOrganization(ctx context.Context, orgID string) error {
	org, err := c.getOrganizationByName(ctx, orgID)
	if err != nil {
		return err
	}
	if org == nil {
		return nil
	}

	if err := c.mgmt.Organizations.Delete(ctx, deref(org.ID)); err != nil && !isStatus(err, http.StatusNotFound) {
		return fmt.Errorf("failed to delete organization %q: %w", orgID, err)
	}

	return nil
}

func (c *ManagementClient) OrganizationExists(ctx context.Context, orgID string) (bool, error) {
	org, err := c.getOrganizationByName(ctx, orgID)
	if err != nil {
		return false, err
	}
	return org != nil, nil
}

func (c *ManagementClient) getOrganizationByName(ctx context.Context, name string) (*management.GetOrganizationByNameResponseContent, error) {
	org, err := c.mgmt.Organizations.GetByName(ctx, name)
	if err != nil {
		if isStatus(err, http.StatusNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get organization %q: %w", name, err)
	}
	return org, nil
}

func (c *ManagementClient) CreateTokenRefresher(orgID string) dcr.TokenRefresherFunc {
	return func(ctx context.Context, clientID string) (newToken string, err error) {
		c.mu.Lock()
		c.ts = c.oauth2Config.TokenSource(ctx)
		ts := c.ts
		c.mu.Unlock()

		token, err := ts.Token()
		if err != nil {
			return "", fmt.Errorf("failed to refresh management token: %w", err)
		}
		return token.AccessToken, nil
	}
}

func (c *ManagementClient) GetUserByEmail(ctx context.Context, orgID, email string) (*idp.User, error) {
	users, err := c.mgmt.Users.ListUsersByEmail(ctx, &management.ListUsersByEmailRequestParameters{
		Email: email,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query users: %w", err)
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("user not found")
	}
	return toUser(users[0]), nil
}

func (c *ManagementClient) ListUsers(ctx context.Context, orgID string) ([]idp.User, error) {
	page, err := c.mgmt.Users.List(ctx, &management.ListUsersRequestParameters{})
	if err != nil {
		return nil, fmt.Errorf("failed to query users: %w", err)
	}

	users := make([]idp.User, 0)
	iter := page.Iterator()
	for iter.Next(ctx) {
		users = append(users, *toUser(iter.Current()))
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failed to query users: %w", err)
	}

	return users, nil
}

func (c *ManagementClient) CreateUser(ctx context.Context, orgID string, clientID string, email string, inviteLink string) error {
	verified := false
	if _, err := c.mgmt.Users.Create(ctx, &management.CreateUserRequestContent{
		Email:         &email,
		EmailVerified: &verified,
		Connection:    defaultUserConnection,
	}); err != nil {
		return fmt.Errorf("failed to create user %q: %w", email, err)
	}
	return nil
}

func (c *ManagementClient) ListClients(ctx context.Context, orgID string) ([]idp.Client, error) {
	page, err := c.mgmt.Clients.List(ctx, &management.ListClientsRequestParameters{})
	if err != nil {
		return nil, fmt.Errorf("failed to list clients: %w", err)
	}

	clients := make([]idp.Client, 0)
	iter := page.Iterator()
	for iter.Next(ctx) {
		clients = append(clients, toClient(iter.Current()))
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failed to list clients: %w", err)
	}

	return clients, nil
}

func (c *ManagementClient) ClientExists(ctx context.Context, orgID string, clientName string) (bool, error) {
	client, err := c.findClient(ctx, orgID, func(client idp.Client) bool {
		return client.Name == clientName
	})
	if err != nil {
		return false, err
	}

	return client != nil, nil
}

func (c *ManagementClient) GetClientByName(ctx context.Context, orgID string, clientName string) (*idp.Client, error) {
	return c.findClient(ctx, orgID, func(client idp.Client) bool {
		return client.Name == clientName
	})
}

func (c *ManagementClient) GetClientByID(ctx context.Context, orgID string, clientID string) (*idp.Client, error) {
	client, err := c.findClient(ctx, orgID, func(client idp.Client) bool {
		return client.ClientID == clientID
	})
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("client with client_id %q not found", clientID)
	}
	return client, nil
}

func (c *ManagementClient) findClient(ctx context.Context, orgID string, pred func(idp.Client) bool) (*idp.Client, error) {
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

func (c *ManagementClient) CreateServiceAccountClient(ctx context.Context, orgID string, config idp.ServiceAccountClientConfig) (*idp.Client, error) {
	name := config.Name
	if name == "" {
		name = config.ClientID
	}

	resp, err := c.mgmt.Clients.Create(ctx, &management.CreateClientRequestContent{
		Name:                    name,
		AppType:                 management.ClientAppTypeEnumNonInteractive.Ptr(),
		GrantTypes:              []string{dcr.GrantTypeClientCredentials},
		TokenEndpointAuthMethod: management.ClientTokenEndpointAuthMethodEnumClientSecretPost.Ptr(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create service account client %q: %w", name, err)
	}

	return &idp.Client{
		ID:       deref(resp.ClientID),
		ClientID: deref(resp.ClientID),
		Name:     deref(resp.Name),
		Secret:   deref(resp.ClientSecret),
	}, nil
}

func (c *ManagementClient) GetClientSecret(ctx context.Context, orgID string, clientID string) (string, error) {
	client, err := c.mgmt.Clients.Get(ctx, clientID, &management.GetClientRequestParameters{})
	if err != nil {
		return "", fmt.Errorf("failed to get client %q: %w", clientID, err)
	}
	return deref(client.ClientSecret), nil
}

func (c *ManagementClient) GetServiceAccountUser(ctx context.Context, orgID string, clientID string) (*idp.User, error) {
	return nil, fmt.Errorf("service account users are not supported by the Auth0 provider")
}

func (c *ManagementClient) GetOrganizationRole(ctx context.Context, orgID string, roleName string) (*idp.Role, error) {
	page, err := c.mgmt.Roles.List(ctx, &management.ListRolesRequestParameters{
		NameFilter: &roleName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list roles: %w", err)
	}

	iter := page.Iterator()
	for iter.Next(ctx) {
		role := iter.Current()
		if deref(role.Name) == roleName {
			return &idp.Role{ID: deref(role.ID), Name: deref(role.Name)}, nil
		}
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failed to list roles: %w", err)
	}

	return nil, nil
}

func (c *ManagementClient) AssignRoleToUser(ctx context.Context, orgID string, serviceAccountUserID string, adminRole idp.Role) error {
	if err := c.mgmt.Users.Roles.Assign(ctx, serviceAccountUserID, &management.AssignUserRolesRequestContent{
		Roles: []string{adminRole.ID},
	}); err != nil {
		return fmt.Errorf("failed to assign role %q to user %q: %w", adminRole.Name, serviceAccountUserID, err)
	}
	return nil
}

func (c *ManagementClient) IssuerURL(_ string) string {
	return c.baseURL + "/"
}

func (c *ManagementClient) JWKSURL(_ string) string {
	return c.baseURL + "/.well-known/jwks.json"
}

func (c *ManagementClient) AuthorizationEndpoint(_ string) string {
	return c.baseURL + "/authorize"
}

func (c *ManagementClient) TokenEndpoint(_ string) string {
	return c.baseURL + "/oauth/token"
}

func toUser(u *management.UserResponseSchema) *idp.User {
	return &idp.User{
		ID:       deref(u.UserID),
		Username: deref(u.Username),
		Email:    deref(u.Email),
		Enabled:  u.Blocked == nil || !*u.Blocked,
	}
}

func toClient(cl *management.Client) idp.Client {
	return idp.Client{
		ID:       deref(cl.ClientID),
		ClientID: deref(cl.ClientID),
		Name:     deref(cl.Name),
		Secret:   deref(cl.ClientSecret),
	}
}

func isStatus(err error, statusCode int) bool {
	if apiErr, ok := errors.AsType[*core.APIError](err); ok {
		return apiErr.StatusCode == statusCode
	}
	return false
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
