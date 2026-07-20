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

package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/security-operator/internal/config"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type fakeRealmChecker struct {
	exists bool
	err    error
}

func (f fakeRealmChecker) RealmExists(ctx context.Context, realmName string) (bool, error) {
	return f.exists, f.err
}

func TestIdentityProviderConfigurationValidator_ValidateCreate(t *testing.T) {
	tests := []struct {
		name            string
		realmName       string
		realmDenyList   []string
		checker         fakeRealmChecker
		wantErr         bool
		wantErrContains string
	}{
		{
			name:      "master realm is denied",
			realmName: "master",
			wantErr:   true,
		},
		{
			name:          "realm from deny list is denied",
			realmName:     "forbidden-realm",
			realmDenyList: []string{"orgs", "forbidden-realm"},
			wantErr:       true,
		},
		{
			name:      "existing realm is denied",
			realmName: "org-1",
			checker:   fakeRealmChecker{exists: true},
			wantErr:   true,
		},
		{
			name:            "realm checker error",
			realmName:       "org-1",
			checker:         fakeRealmChecker{err: fmt.Errorf("connection refused")},
			wantErr:         true,
			wantErrContains: "failed to check realm existence",
		},
		{
			name:      "non-existing realm is allowed",
			realmName: "org-2",
			wantErr:   false,
		},
		{
			name:      "empty realm name is denied",
			realmName: "  ",
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &identityProviderConfigurationValidator{
				keycloakClient: tt.checker,
				realmDenyList:  tt.realmDenyList,
			}
			_, err := v.ValidateCreate(t.Context(), &pmcorev1alpha1.IdentityProviderConfiguration{
				ObjectMeta: metav1.ObjectMeta{Name: tt.realmName},
			})
			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrContains != "" {
					assert.Contains(t, err.Error(), tt.wantErrContains)
				}
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestIdentityProviderConfigurationValidator_ValidateUpdate(t *testing.T) {
	v := &identityProviderConfigurationValidator{keycloakClient: fakeRealmChecker{exists: true}}

	validOIDC := pmcorev1alpha1.UpstreamIdentityProvider{
		Alias: "dex",
		Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
		OIDC: &pmcorev1alpha1.OIDCUpstreamConfig{
			DiscoveryURL: "https://dex.example/.well-known/openid-configuration",
			ClientID:     "broker",
			ClientSecretRef: corev1.SecretReference{
				Name: "dex-secret",
			},
			ClientAuthentication: "client_secret_post",
		},
	}

	tests := []struct {
		name    string
		spec    pmcorev1alpha1.IdentityProviderConfigurationSpec
		wantErr bool
	}{
		{
			name: "empty upstream list allowed",
			spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
				Clients: []pmcorev1alpha1.IdentityProviderClientConfig{{
					ClientName:   "portal",
					ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
					RedirectURIs: []string{"https://example.com/*"},
				}},
			},
		},
		{
			name: "valid discovery config",
			spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
				Clients: []pmcorev1alpha1.IdentityProviderClientConfig{{
					ClientName:   "portal",
					ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
					RedirectURIs: []string{"https://example.com/*"},
				}},
				UpstreamIdentityProviders: []pmcorev1alpha1.UpstreamIdentityProvider{validOIDC},
			},
		},
		{
			name: "duplicate alias denied",
			spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
				Clients: []pmcorev1alpha1.IdentityProviderClientConfig{{
					ClientName:   "portal",
					ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
					RedirectURIs: []string{"https://example.com/*"},
				}},
				UpstreamIdentityProviders: []pmcorev1alpha1.UpstreamIdentityProvider{
					validOIDC,
					validOIDC,
				},
			},
			wantErr: true,
		},
		{
			name: "discovery and manual mutually exclusive",
			spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
				Clients: []pmcorev1alpha1.IdentityProviderClientConfig{{
					ClientName:   "portal",
					ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
					RedirectURIs: []string{"https://example.com/*"},
				}},
				UpstreamIdentityProviders: []pmcorev1alpha1.UpstreamIdentityProvider{{
					Alias: "dex",
					Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
					OIDC: &pmcorev1alpha1.OIDCUpstreamConfig{
						DiscoveryURL:     "https://dex.example/.well-known/openid-configuration",
						Issuer:           "https://dex.example",
						AuthorizationURL: "https://dex.example/auth",
						TokenURL:         "https://dex.example/token",
						ClientID:         "broker",
						ClientSecretRef:  corev1.SecretReference{Name: "dex-secret"},
					},
				}},
			},
			wantErr: true,
		},
		{
			name: "missing client secret for secret auth",
			spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
				Clients: []pmcorev1alpha1.IdentityProviderClientConfig{{
					ClientName:   "portal",
					ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
					RedirectURIs: []string{"https://example.com/*"},
				}},
				UpstreamIdentityProviders: []pmcorev1alpha1.UpstreamIdentityProvider{{
					Alias: "dex",
					Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
					OIDC: &pmcorev1alpha1.OIDCUpstreamConfig{
						DiscoveryURL:         "https://dex.example/.well-known/openid-configuration",
						ClientID:             "broker",
						ClientAuthentication: "client_secret_post",
					},
				}},
			},
			wantErr: true,
		},
		{
			name: "email domain routing requires domains",
			spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
				Clients: []pmcorev1alpha1.IdentityProviderClientConfig{{
					ClientName:   "portal",
					ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
					RedirectURIs: []string{"https://example.com/*"},
				}},
				UpstreamIdentityProviders: []pmcorev1alpha1.UpstreamIdentityProvider{{
					Alias: "dex",
					Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
					EmailDomainRouting: &pmcorev1alpha1.EmailDomainRouting{
						AutoRedirect: func() *bool {
							v := true
							return &v
						}(),
					},
					OIDC: &pmcorev1alpha1.OIDCUpstreamConfig{
						DiscoveryURL:    "https://dex.example/.well-known/openid-configuration",
						ClientID:        "broker",
						ClientSecretRef: corev1.SecretReference{Name: "dex-secret"},
					},
				}},
			},
			wantErr: true,
		},
		{
			name: "duplicate email domain denied",
			spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
				Clients: []pmcorev1alpha1.IdentityProviderClientConfig{{
					ClientName:   "portal",
					ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
					RedirectURIs: []string{"https://example.com/*"},
				}},
				UpstreamIdentityProviders: []pmcorev1alpha1.UpstreamIdentityProvider{
					{
						Alias: "dex",
						Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
						EmailDomainRouting: &pmcorev1alpha1.EmailDomainRouting{
							Domains: []string{"corp.example.com"},
							AutoRedirect: func() *bool {
								v := true
								return &v
							}(),
						},
						OIDC: &pmcorev1alpha1.OIDCUpstreamConfig{
							DiscoveryURL:    "https://dex.example/.well-known/openid-configuration",
							ClientID:        "broker",
							ClientSecretRef: corev1.SecretReference{Name: "dex-secret"},
						},
					},
					{
						Alias: "okta",
						Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
						EmailDomainRouting: &pmcorev1alpha1.EmailDomainRouting{
							Domains: []string{"corp.example.com"},
							AutoRedirect: func() *bool {
								v := true
								return &v
							}(),
						},
						OIDC: &pmcorev1alpha1.OIDCUpstreamConfig{
							DiscoveryURL:    "https://okta.example/.well-known/openid-configuration",
							ClientID:        "broker",
							ClientSecretRef: corev1.SecretReference{Name: "okta-secret"},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "email domains without autoRedirect allowed",
			spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
				Clients: []pmcorev1alpha1.IdentityProviderClientConfig{{
					ClientName:   "portal",
					ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
					RedirectURIs: []string{"https://example.com/*"},
				}},
				UpstreamIdentityProviders: []pmcorev1alpha1.UpstreamIdentityProvider{{
					Alias: "dex",
					Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
					EmailDomainRouting: &pmcorev1alpha1.EmailDomainRouting{
						Domains: []string{"corp.example.com"},
					},
					OIDC: &pmcorev1alpha1.OIDCUpstreamConfig{
						DiscoveryURL:    "https://dex.example/.well-known/openid-configuration",
						ClientID:        "broker",
						ClientSecretRef: corev1.SecretReference{Name: "dex-secret"},
					},
				}},
			},
		},
		{
			name: "valid email domain redirect config",
			spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
				Clients: []pmcorev1alpha1.IdentityProviderClientConfig{{
					ClientName:   "portal",
					ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
					RedirectURIs: []string{"https://example.com/*"},
				}},
				UpstreamIdentityProviders: []pmcorev1alpha1.UpstreamIdentityProvider{{
					Alias: "dex",
					Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
					EmailDomainRouting: &pmcorev1alpha1.EmailDomainRouting{
						Domains: []string{"portal.localhost"},
						AutoRedirect: func() *bool {
							v := true
							return &v
						}(),
					},
					OIDC: &pmcorev1alpha1.OIDCUpstreamConfig{
						DiscoveryURL:    "https://dex.example/.well-known/openid-configuration",
						ClientID:        "broker",
						ClientSecretRef: corev1.SecretReference{Name: "dex-secret"},
					},
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := v.ValidateUpdate(t.Context(),
				&pmcorev1alpha1.IdentityProviderConfiguration{},
				&pmcorev1alpha1.IdentityProviderConfiguration{Spec: tt.spec},
			)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestIdentityProviderConfigurationValidator_ValidateDelete(t *testing.T) {
	v := &identityProviderConfigurationValidator{keycloakClient: fakeRealmChecker{}}
	_, err := v.ValidateDelete(t.Context(), &pmcorev1alpha1.IdentityProviderConfiguration{})
	require.NoError(t, err)
}

func TestNewKeycloakAdminClient(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) *httptest.Server
		wantErr     bool
	}{
		{
			name: "OIDC discovery fails",
			setupServer: func(t *testing.T) *httptest.Server {
				srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
				srv.Close() // nothing listening → connection refused
				return srv
			},
			wantErr: true,
		},
		{
			name: "success",
			setupServer: func(t *testing.T) *httptest.Server {
				var srvURL string
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
						"issuer":                 srvURL + "/realms/master",
						"token_endpoint":         srvURL + "/realms/master/protocol/openid-connect/token",
						"authorization_endpoint": srvURL + "/realms/master/protocol/openid-connect/auth",
						"jwks_uri":               srvURL + "/realms/master/protocol/openid-connect/certs",
					})
				}))
				srvURL = srv.URL
				return srv
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			defer srv.Close()

			cfg := &config.Config{}
			cfg.Keycloak.BaseURL = srv.URL
			cfg.Keycloak.ClientID = "test-client"
			cfg.Keycloak.ClientSecret = "test-secret"

			client, err := newKeycloakAdminClient(t.Context(), cfg)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, client)
		})
	}
}

func TestSetupIdentityProviderConfigurationValidatingWebhookWithManager_AdminClientError(t *testing.T) {
	cfg := &config.Config{}
	cfg.Keycloak.BaseURL = "http://127.0.0.1:1"
	err := SetupIdentityProviderConfigurationValidatingWebhookWithManager(t.Context(), nil, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create keycloak admin client for webhook")
}
