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
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
)

// Tokens are what a login returns. kcp authenticates a user by the ID
// token, the services of Platform Mesh by either.
type Tokens struct {
	IDToken     string `json:"id_token"`
	AccessToken string `json:"access_token"`
}

// loginForm finds where the login page of Keycloak posts the credentials to.
var loginForm = regexp.MustCompile(`<form[^>]*\baction="([^"]+)"`)

// Login implements Client. It is the authorization code flow with PKCE, the
// way a browser or `kubectl oidc-login` goes: open the login page, post the
// credentials to its form, and trade the code of the redirect for tokens.
// The password grant would be shorter, but the clients of Platform Mesh do
// not allow it.
func (c *adminClient) Login(ctx context.Context, realm, clientID, redirectURI, email, password string) (*Tokens, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	// The session of the login page is a cookie, and the redirect with the
	// code goes to an address nobody listens on, so it is not followed.
	browser := &http.Client{
		Timeout:   c.http.Timeout,
		Transport: c.http.Transport,
		Jar:       jar,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if strings.HasPrefix(req.URL.String(), redirectURI) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	oidc := c.baseURL + "/realms/" + url.PathEscape(realm) + "/protocol/openid-connect"
	fail := func(format string, args ...any) (*Tokens, error) {
		return nil, fmt.Errorf("logging in to realm %s as %s: %s", realm, email, fmt.Sprintf(format, args...))
	}

	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(random)
	challenge := sha256.Sum256([]byte(verifier))

	// 1. The login page.
	query := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {"openid email"},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
	}
	page, status, _, err := fetch(ctx, browser, http.MethodGet, oidc+"/auth?"+query.Encode(), nil)
	if err != nil {
		return fail("opening the login page: %v", err)
	}
	form := loginForm.FindStringSubmatch(page)
	if status != http.StatusOK || form == nil {
		return fail("the login page answered %d without a login form: %s", status, excerpt(page))
	}

	// 2. The credentials. Keycloak answers a good login with a redirect
	//    to redirectURI carrying the code, and a bad one with the page
	//    again.
	credentials := url.Values{"username": {email}, "password": {password}}
	page, status, location, err := fetch(ctx, browser, http.MethodPost, html.UnescapeString(form[1]), credentials)
	if err != nil {
		return fail("posting the credentials: %v", err)
	}
	if location == nil || !strings.HasPrefix(location.String(), redirectURI) {
		return fail("the login was not accepted (%d, redirect to %v): %s", status, location, excerpt(page))
	}
	code := location.Query().Get("code")
	if code == "" {
		return fail("the redirect has no code: %s", location.Query().Get("error_description"))
	}

	// 3. The tokens.
	grant := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"code":          {code},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oidc+"/token", strings.NewReader(grant.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokens := &Tokens{}
	if err := c.send(req, tokens); err != nil {
		return fail("trading the code for tokens: %v", err)
	}
	if tokens.IDToken == "" || tokens.AccessToken == "" {
		return nil, errors.New("keycloak: the token response lacks the ID or the access token")
	}
	return tokens, nil
}

// fetch sends a request like a browser would, a form when it has one, and
// returns the body, the status and where the response redirects to.
func fetch(ctx context.Context, browser *http.Client, method, target string, form url.Values) (string, int, *url.URL, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return "", 0, nil, err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	res, err := browser.Do(req)
	if err != nil {
		return "", 0, nil, err
	}
	defer res.Body.Close() //nolint:errcheck
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", 0, nil, err
	}
	// No Location is not an error here.
	location, _ := res.Location()
	return string(data), res.StatusCode, location, nil
}

// excerpt is the start of a page, for an error message.
func excerpt(page string) string {
	page = strings.Join(strings.Fields(page), " ")
	if len(page) > 300 {
		page = page[:300] + "..."
	}
	return page
}
