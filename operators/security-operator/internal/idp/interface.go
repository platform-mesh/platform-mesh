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

package idp

import (
	"context"

	"go.platform-mesh.io/security-operator/internal/idp/dcr"
)

// Provider defines the interface for an external OIDC provider
type Provider interface {
	// Client Registration (DCR - RFC 7591/7592)
	GetClient(ctx context.Context, orgID string, clientID, registrationURI, registrationToken string) (dcr.ClientInformation, error)
	CreateClient(ctx context.Context, orgID string, metadata dcr.ClientMetadata) (dcr.ClientInformation, error)
	UpdateClient(ctx context.Context, orgID string, registrationURI, registrationToken string, metadata dcr.ClientMetadata) (dcr.ClientInformation, error)
	DeleteClient(ctx context.Context, orgID string, clientID, registrationURI, registrationToken string) error

	GetClientByName(ctx context.Context, orgID string, clientName string) (*Client, error)
	GetClientByID(ctx context.Context, orgID string, clientID string) (*Client, error)
	ClientExists(ctx context.Context, orgID string, clientName string) (bool, error)

	// Realm (org) Management (provider-specific)
	CreateOrganization(ctx context.Context, orgID string, config OrganizationConfig) error
	UpdateOrganization(ctx context.Context, orgID string, config OrganizationConfig) error
	DeleteOrganization(ctx context.Context, orgID string) error
	OrganizationExists(ctx context.Context, orgID string) (bool, error)
	EnsureOrganization(ctx context.Context, orgID string, config OrganizationConfig) (created bool, err error)

	// Token Management
	CreateTokenProvider(orgID string) dcr.TokenProviderFunc
	CreateTokenRefresher(orgID string) dcr.TokenRefresherFunc

	// User Management (SCIM or Userinfo)
	GetUserByEmail(ctx context.Context, orgID, email string) (*User, error)
	ListUsers(ctx context.Context, orgID string) ([]User, error)
	CreateUser(ctx context.Context, orgID string, clientID string, email string, inviteLink string) error

	// Configuration
	IssuerURL(orgID string) string
	JWKSURL(orgID string) string
	AuthorizationEndpoint(orgID string) string
	TokenEndpoint(orgID string) string

	RegistrationEndpoint(orgID string, clientID string) string
	GetOrganizationRole(ctx context.Context, orgID string, roleName string) (*Role, error)
	ListClients(ctx context.Context, orgID string) ([]Client, error)
	CreateServiceAccountClient(ctx context.Context, orgID string, config ServiceAccountClientConfig) (*Client, error)
	GetClientSecret(ctx context.Context, orgID string, clientID string) (string, error)
	GrantServiceAccountRole(ctx context.Context, orgID string, clientID string, role Role) error
}

type OrganizationConfig struct {
	RegistrationAllowed bool
}

type ServiceAccountClientConfig struct {
	ClientID               string
	Name                   string
	Enabled                bool
	ServiceAccountsEnabled bool
	PublicClient           bool
}

type Client struct {
	ID       string // UUID
	ClientID string
	Name     string
	Secret   string
}

type Role struct {
	ID     string
	Name   string
	Scopes []string
}

type User struct {
	ID              string
	Username        string
	Email           string
	RequiredActions []string
	Enabled         bool
	Credentials     []UserCredential
}

type UserCredential struct {
	Type      string
	Value     string
	Temporary bool
}
