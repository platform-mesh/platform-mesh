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

// Package oidc runs a mock OpenID Connect issuer inside the process, in
// place of Keycloak. It is github.com/oauth2-proxy/mockoidc: there is no
// login page and no password, every authorization request is answered at
// once with a code for the configured user. It is for development only.
package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/oauth2-proxy/mockoidc"

	"go.platform-mesh.io/platform-mesh/cmd/pm/pkg/certs"
)

const (
	// ClientID and ClientSecret are the one client the issuer knows. The
	// secret is public: it protects nothing.
	ClientID     = "platform-mesh"
	ClientSecret = "platform-mesh"

	configFile = "config.json"
)

// Options configure the issuer.
type Options struct {
	// Port is the localhost port to serve on, with TLS.
	Port int
	// BrowserPort is a second localhost port serving the same issuer
	// without TLS. The discovery document sends browsers there to log in,
	// so that nobody has to accept a self-signed certificate; everything
	// else (tokens, keys) stays on Port.
	BrowserPort int
	// Email is who every login is, unless the authorization request names
	// someone else with login_hint.
	Email string
	// Groups are the groups of every user.
	Groups []string
	// Dir keeps the CA, the serving certificate and the signing key, so
	// that the issuer is the same after a restart.
	Dir string
}

// Config is what a client needs to use the issuer. Start writes it to
// <Dir>/config.json.
type Config struct {
	IssuerURL    string `json:"issuerURL"`
	ClientID     string `json:"clientID"`
	ClientSecret string `json:"clientSecret"`
	// CAFile is the PEM CA of the issuer's serving certificate.
	CAFile string `json:"caFile"`
}

// Start serves the issuer until ctx is done.
func Start(ctx context.Context, o Options) (*Config, error) {
	if err := os.MkdirAll(o.Dir, 0o700); err != nil {
		return nil, err
	}
	caFile, serving, err := certs.Ensure(o.Dir)
	if err != nil {
		return nil, fmt.Errorf("serving certificate: %w", err)
	}
	key, err := ensureKey(filepath.Join(o.Dir, "signing.key"))
	if err != nil {
		return nil, fmt.Errorf("signing key: %w", err)
	}

	browserURL := "http://" + net.JoinHostPort("localhost", strconv.Itoa(o.BrowserPort))
	m, err := mockoidc.NewServer(key)
	if err != nil {
		return nil, err
	}
	m.ClientID, m.ClientSecret = ClientID, ClientSecret
	m.AccessTTL = time.Hour
	m.RefreshTTL = 24 * time.Hour
	// mockoidc hands out the queued user, or jane.doe: queue ours for
	// every authorization request.
	if err := m.AddMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if req.URL.Path == mockoidc.AuthorizationEndpoint {
				email := o.Email
				if hint := req.URL.Query().Get("login_hint"); hint != "" {
					email = hint
				}
				m.QueueUser(&mockoidc.MockUser{
					Subject:           email,
					Email:             email,
					EmailVerified:     true,
					PreferredUsername: strings.Split(email, "@")[0],
					Groups:            o.Groups,
				})
			}
			// mockoidc only takes the client secret from the form; the
			// portal sends it as basic auth.
			if req.URL.Path == mockoidc.TokenEndpoint {
				if id, secret, ok := req.BasicAuth(); ok && req.ParseForm() == nil {
					if req.Form.Get("client_id") == "" {
						req.Form.Set("client_id", id)
					}
					if req.Form.Get("client_secret") == "" {
						req.Form.Set("client_secret", secret)
					}
				}
			}
			if req.URL.Path == mockoidc.DiscoveryEndpoint {
				rec := httptest.NewRecorder()
				next.ServeHTTP(rec, req)
				doc := map[string]any{}
				if rec.Code == http.StatusOK && json.Unmarshal(rec.Body.Bytes(), &doc) == nil {
					doc["authorization_endpoint"] = browserURL + mockoidc.AuthorizationEndpoint
					if raw, err := json.Marshal(doc); err == nil {
						rw.Header().Set("Content-Type", "application/json")
						_, _ = rw.Write(raw)
						return
					}
				}
				for k, v := range rec.Header() {
					rw.Header()[k] = v
				}
				rw.WriteHeader(rec.Code)
				_, _ = rw.Write(rec.Body.Bytes())
				return
			}
			next.ServeHTTP(rw, req)
		})
	}); err != nil {
		return nil, err
	}

	tlsCfg := &tls.Config{Certificates: []tls.Certificate{serving}, MinVersion: tls.VersionTLS12}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(o.Port)))
	if err != nil {
		return nil, fmt.Errorf("--mock-oidc-port: %w", err)
	}
	browserLn, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(o.BrowserPort)))
	if err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("--mock-oidc-browser-port: %w", err)
	}
	if err := m.Start(tls.NewListener(ln, tlsCfg), tlsCfg); err != nil {
		return nil, err
	}
	browser := &http.Server{Handler: m.Server.Handler, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = browser.Serve(browserLn) }()
	go func() {
		<-ctx.Done()
		_ = m.Shutdown()
		_ = browser.Close()
	}()

	cfg := &Config{IssuerURL: m.Issuer(), ClientID: ClientID, ClientSecret: ClientSecret, CAFile: caFile}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(o.Dir, configFile), raw, 0o600); err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadConfig reads the Config a running issuer wrote to dir.
func LoadConfig(dir string) (*Config, error) {
	raw, err := os.ReadFile(filepath.Join(dir, configFile))
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	return cfg, json.Unmarshal(raw, cfg)
}

// Token logs in as email (the issuer's default user when empty) with the
// authorization code flow and returns the ID token.
func (c *Config) Token(ctx context.Context, email string) (string, error) {
	ca, err := os.ReadFile(c.CAFile)
	if err != nil {
		return "", err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return "", fmt.Errorf("%s holds no certificate", c.CAFile)
	}
	client := &http.Client{
		Timeout:       10 * time.Second,
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	const redirect = "http://localhost/callback"
	q := url.Values{
		"client_id":     {c.ClientID},
		"response_type": {"code"},
		"scope":         {"openid email groups profile"},
		"state":         {"pm"},
		"redirect_uri":  {redirect},
	}
	if email != "" {
		q.Set("login_hint", email)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.IssuerURL+"/authorize?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	_ = resp.Body.Close()
	loc, err := resp.Location()
	if err != nil {
		return "", fmt.Errorf("authorize: status %s, no redirect", resp.Status)
	}
	code := loc.Query().Get("code")
	if code == "" {
		return "", errors.New("authorize: no code in redirect")
	}

	form := url.Values{
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirect},
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, c.IssuerURL+"/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err = client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	var out struct {
		IDToken string `json:"id_token"`
		Error   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("token: status %s: %w", resp.Status, err)
	}
	if out.IDToken == "" {
		return "", fmt.Errorf("token: status %s: %s", resp.Status, out.Error)
	}
	return out.IDToken, nil
}

func ensureKey(path string) (*rsa.PrivateKey, error) {
	if raw, err := os.ReadFile(path); err == nil {
		if block, _ := pem.Decode(raw); block != nil {
			return x509.ParsePKCS1PrivateKey(block.Bytes)
		}
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	raw := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return key, os.WriteFile(path, raw, 0o600)
}
