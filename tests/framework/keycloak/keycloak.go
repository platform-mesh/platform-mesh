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

// Package keycloak manages users of the Keycloak of a deployed Platform
// Mesh through its admin REST API. An organization has a realm of its own,
// named like the organization; the users who have not picked an organization
// yet are in the realm "welcome".
//
//	kc := pm.Keycloak(t)
//	id, err := kc.EnsureUser(ctx, "acme", keycloak.User{Email: "owner@example.com", Password: "..."})
package keycloak

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// WelcomeRealm is the realm users register in before they have an
// organization.
const WelcomeRealm = "welcome"

// ErrNotFound is returned by User when the realm has no such user.
var ErrNotFound = errors.New("user not found")

// User is a user of a realm. Platform Mesh realms use the email as the
// username.
type User struct {
	// ID is set by Keycloak.
	ID        string `json:"id,omitempty"`
	Email     string `json:"email"`
	FirstName string `json:"firstName,omitempty"`
	LastName  string `json:"lastName,omitempty"`
	// Enabled, EmailVerified and RequiredActions are what Keycloak
	// reports; EnsureUser enables and verifies the user and clears the
	// actions.
	Enabled         bool     `json:"enabled"`
	EmailVerified   bool     `json:"emailVerified"`
	RequiredActions []string `json:"requiredActions"`
	// Password is only written, by EnsureUser; Keycloak never returns it.
	Password string `json:"-"`
}

// Client is what the tests can do with the Keycloak of a deployed Platform
// Mesh. New returns the implementation, which talks to the admin REST API.
type Client interface {
	// User returns the user with the email in the realm, or ErrNotFound.
	User(ctx context.Context, realm, email string) (*User, error)

	// EnsureUser makes the user able to log in to the realm with
	// u.Password: it creates the user unless there is one with u.Email,
	// sets the password, marks the email verified and clears the required
	// actions (an invited user has to update the password first). It
	// returns the user's ID.
	EnsureUser(ctx context.Context, realm string, u User) (string, error)

	// DeleteUser removes the user with the email from the realm; a user
	// that is not there is not an error.
	DeleteUser(ctx context.Context, realm, email string) error

	// HasRealm tells whether there is a realm of the name.
	HasRealm(ctx context.Context, realm string) (bool, error)

	// Login logs the user in to the realm with the password, through the
	// public client clientID and its redirect URI, the way a browser does,
	// and returns the tokens. It needs no admin and changes nothing.
	Login(ctx context.Context, realm, clientID, redirectURI, email, password string) (*Tokens, error)
}

// adminClient implements Client as the admin user of the master realm.
type adminClient struct {
	baseURL  string
	user     string
	password string
	http     *http.Client
}

var _ Client = &adminClient{}

// New returns a client for the Keycloak at baseURL (for example
// https://portal.example.com/keycloak), authenticating as the admin user of
// the master realm. roots are the CAs its certificate is checked against;
// nil means the system's.
func New(baseURL, adminUser, adminPassword string, roots *x509.CertPool) Client {
	return &adminClient{
		baseURL:  strings.TrimSuffix(baseURL, "/"),
		user:     adminUser,
		password: adminPassword,
		http: &http.Client{
			Timeout:   30 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}},
		},
	}
}

// User implements Client.
func (c *adminClient) User(ctx context.Context, realm, email string) (*User, error) {
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	return c.find(ctx, token, realm, email)
}

// EnsureUser implements Client.
func (c *adminClient) EnsureUser(ctx context.Context, realm string, u User) (string, error) {
	if u.Email == "" || u.Password == "" {
		return "", errors.New("keycloak: user needs an email and a password")
	}
	token, err := c.token(ctx)
	if err != nil {
		return "", err
	}
	users := c.realmURL(realm) + "/users"

	existing, err := c.find(ctx, token, realm, u.Email)
	if errors.Is(err, ErrNotFound) {
		u.Enabled, u.EmailVerified, u.RequiredActions = true, true, []string{}
		if err := c.do(ctx, http.MethodPost, users, token, u, nil); err != nil {
			return "", fmt.Errorf("creating user %s in realm %s: %w", u.Email, realm, err)
		}
		existing, err = c.find(ctx, token, realm, u.Email)
	}
	if err != nil {
		return "", err
	}

	// Also for a user just created: the defaults of a realm can add
	// required actions to it.
	password := map[string]any{"type": "password", "value": u.Password, "temporary": false}
	if err := c.do(ctx, http.MethodPut, users+"/"+existing.ID+"/reset-password", token, password, nil); err != nil {
		return "", fmt.Errorf("setting the password of %s in realm %s: %w", u.Email, realm, err)
	}
	update := map[string]any{"enabled": true, "emailVerified": true, "requiredActions": []string{}}
	if u.FirstName != "" {
		update["firstName"] = u.FirstName
	}
	if u.LastName != "" {
		update["lastName"] = u.LastName
	}
	// Keycloak replaces the user with the representation it is sent, so
	// the email goes along.
	update["email"] = existing.Email
	if err := c.do(ctx, http.MethodPut, users+"/"+existing.ID, token, update, nil); err != nil {
		return "", fmt.Errorf("updating user %s in realm %s: %w", u.Email, realm, err)
	}
	return existing.ID, nil
}

// DeleteUser implements Client.
func (c *adminClient) DeleteUser(ctx context.Context, realm, email string) error {
	token, err := c.token(ctx)
	if err != nil {
		return err
	}
	existing, err := c.find(ctx, token, realm, email)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := c.do(ctx, http.MethodDelete, c.realmURL(realm)+"/users/"+existing.ID, token, nil, nil); err != nil {
		return fmt.Errorf("deleting user %s in realm %s: %w", email, realm, err)
	}
	return nil
}

// HasRealm implements Client.
func (c *adminClient) HasRealm(ctx context.Context, realm string) (bool, error) {
	token, err := c.token(ctx)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.realmURL(realm), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := c.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("looking up realm %s: %w", realm, err)
	}
	defer res.Body.Close() //nolint:errcheck
	switch res.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("looking up realm %s: %s", realm, res.Status)
	}
}

func (c *adminClient) realmURL(realm string) string {
	return c.baseURL + "/admin/realms/" + url.PathEscape(realm)
}

func (c *adminClient) find(ctx context.Context, token, realm, email string) (*User, error) {
	q := url.Values{"email": {email}, "exact": {"true"}}
	var found []User
	if err := c.do(ctx, http.MethodGet, c.realmURL(realm)+"/users?"+q.Encode(), token, nil, &found); err != nil {
		return nil, fmt.Errorf("looking up user %s in realm %s: %w", email, realm, err)
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("%w: %s in realm %s", ErrNotFound, email, realm)
	}
	return &found[0], nil
}

// token logs the admin in. Admin tokens live for a minute, so every call
// of the exported methods gets its own.
func (c *adminClient) token(ctx context.Context) (string, error) {
	form := url.Values{
		"grant_type": {"password"},
		"client_id":  {"admin-cli"},
		"username":   {c.user},
		"password":   {c.password},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/realms/master/protocol/openid-connect/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := c.send(req, &out); err != nil {
		return "", fmt.Errorf("logging in to Keycloak as %s: %w", c.user, err)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("logging in to Keycloak as %s: no access token in the response", c.user)
	}
	return out.AccessToken, nil
}

func (c *adminClient) do(ctx context.Context, method, target, token string, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.send(req, out)
}

func (c *adminClient) send(req *http.Request, out any) error {
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close() //nolint:errcheck
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("%s %s: %s: %s", req.Method, req.URL.Path, res.Status, strings.TrimSpace(string(data)))
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}
