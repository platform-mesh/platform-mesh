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
	"time"

	"go.platform-mesh.io/security-operator/internal/idp"
)

const (
	RequiredActionVerifyEmail    string = "VERIFY_EMAIL"
	RequiredActionUpdatePassword string = "UPDATE_PASSWORD"
	UserDefaultPasswordType      string = "password"
	UserDefaultPasswordValue     string = "password"
)

type Config struct {
	AccessTokenLifespan      time.Duration
	SetDefaultPassword       bool
	HttpClientTimeoutSeconds time.Duration
	SMTP                     *SMTPConfig
}

type realmConfig struct {
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

type user struct {
	ID              string           `json:"id,omitempty"`
	Username        string           `json:"username,omitempty"`
	Email           string           `json:"email,omitempty"`
	RequiredActions []string         `json:"requiredActions,omitempty"`
	Enabled         bool             `json:"enabled,omitempty"`
	Credentials     []userCredential `json:"credentials,omitempty"`
}

func (u *user) ToPublic() *idp.User {
	credentials := make([]idp.UserCredential, 0, len(u.Credentials))
	for _, c := range u.Credentials {
		public := c.ToPublic()
		credentials = append(credentials, *public)
	}

	return &idp.User{
		ID:              u.ID,
		Email:           u.Email,
		RequiredActions: u.RequiredActions,
		Enabled:         u.Enabled,
		Credentials:     credentials,
	}
}

type userCredential struct {
	Type      string `json:"type"`
	Value     string `json:"value"`
	Temporary bool   `json:"temporary"`
}

func (c *userCredential) ToPublic() *idp.UserCredential {
	return &idp.UserCredential{
		Type:      c.Type,
		Value:     c.Value,
		Temporary: c.Temporary,
	}
}

type client struct {
	ID       string `json:"id"`
	ClientID string `json:"clientId"`
	Name     string `json:"name"`
	Secret   string `json:"secret"`
}

type SMTPConfig struct {
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	From     string `json:"from,omitempty"`
	SSL      bool   `json:"ssl,omitempty"`
	StartTLS bool   `json:"starttls,omitempty"`
	Auth     bool   `json:"auth,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
}

type serviceAccountClientConfig struct {
	ClientID               string `json:"clientId"`
	Name                   string `json:"name,omitempty"`
	Enabled                bool   `json:"enabled"`
	ServiceAccountsEnabled bool   `json:"serviceAccountsEnabled"`
	PublicClient           bool   `json:"publicClient"`
}

type role struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func createRole(ri idp.Role) role {
	return role{
		ID:   ri.ID,
		Name: ri.Name,
	}
}

func (r *role) ToPublic() *idp.Role {
	return &idp.Role{
		ID:   r.ID,
		Name: r.Name,
	}
}
