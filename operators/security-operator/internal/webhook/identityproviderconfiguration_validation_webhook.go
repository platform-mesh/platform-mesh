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
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/coreos/go-oidc"
	"golang.org/x/oauth2/clientcredentials"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/util"
	"go.platform-mesh.io/security-operator/pkg/clientreg"
	"go.platform-mesh.io/security-operator/pkg/clientreg/keycloak"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	mcruntime "sigs.k8s.io/multicluster-runtime"
)

// SetupIdentityProviderConfigurationValidatingWebhookWithManager registers a validating webhook that prevents
// creation of an `IdentityProviderConfiguration` if the corresponding Keycloak realm already exists.
func SetupIdentityProviderConfigurationValidatingWebhookWithManager(ctx context.Context, mgr ctrl.Manager, cfg *config.Config) error {
	keycloakClient, err := newKeycloakAdminClient(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to create keycloak admin client for webhook: %w", err)
	}

	realmDenyList := slices.Clone(cfg.IDP.RealmDenyList)

	return mcruntime.NewWebhookManagedBy(mgr, &pmcorev1alpha1.IdentityProviderConfiguration{}).
		WithValidator(&identityProviderConfigurationValidator{keycloakClient: keycloakClient, realmDenyList: realmDenyList}).
		Complete()
}

var _ admission.Validator[*pmcorev1alpha1.IdentityProviderConfiguration] = (*identityProviderConfigurationValidator)(nil)
var _ realmChecker = (*keycloak.AdminClient)(nil)

type identityProviderConfigurationValidator struct {
	keycloakClient realmChecker
	realmDenyList  []string
}

type realmChecker interface {
	RealmExists(ctx context.Context, realmName string) (bool, error)
}

func (v *identityProviderConfigurationValidator) ValidateCreate(ctx context.Context, idp *pmcorev1alpha1.IdentityProviderConfiguration) (admission.Warnings, error) {
	realmName := strings.TrimSpace(idp.GetName())
	if realmName == "" {
		return nil, fmt.Errorf("realm name must not be empty")
	}
	if realmName == "master" {
		return nil, fmt.Errorf("creation of IdentityProviderConfiguration for realm 'master' is not allowed")
	}
	if slices.Contains(v.realmDenyList, realmName) {
		return nil, fmt.Errorf("creation of IdentityProviderConfiguration for realm %q is not allowed", realmName)
	}

	exists, err := v.keycloakClient.RealmExists(ctx, realmName)
	if err != nil {
		return nil, fmt.Errorf("failed to check realm existence in keycloak: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("keycloak realm %q already exists", realmName)
	}

	if err := validateUpstreamIdentityProviders(idp.Spec.UpstreamIdentityProviders); err != nil {
		return nil, err
	}

	return nil, nil
}

func (v *identityProviderConfigurationValidator) ValidateUpdate(ctx context.Context, oldObj, newObj *pmcorev1alpha1.IdentityProviderConfiguration) (admission.Warnings, error) {
	if err := validateUpstreamIdentityProviders(newObj.Spec.UpstreamIdentityProviders); err != nil {
		return nil, err
	}
	return nil, nil
}

func (v *identityProviderConfigurationValidator) ValidateDelete(ctx context.Context, obj *pmcorev1alpha1.IdentityProviderConfiguration) (admission.Warnings, error) {
	return nil, nil
}

func newKeycloakAdminClient(ctx context.Context, cfg *config.Config) (*keycloak.AdminClient, error) {
	issuer := fmt.Sprintf("%s/realms/master", cfg.Keycloak.BaseURL)
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}

	cCfg := clientcredentials.Config{
		ClientID:     cfg.Keycloak.ClientID,
		ClientSecret: cfg.Keycloak.ClientSecret,
		TokenURL:     provider.Endpoint().TokenURL,
	}

	adminHTTPClient := cCfg.Client(ctx)

	// Use the master realm for admin endpoint access.
	adminClient := keycloak.NewAdminClient(adminHTTPClient, cfg.Keycloak.BaseURL, "master")
	adminHTTPClient.Transport = clientreg.NewRetryTransport(adminHTTPClient.Transport, adminClient)

	return adminClient, nil
}

func validateUpstreamIdentityProviders(providers []pmcorev1alpha1.UpstreamIdentityProvider) error {
	seen := make(map[string]struct{}, len(providers))
	seenDomains := make(map[string]string, len(providers))

	for i := range providers {
		provider := &providers[i]
		alias := strings.TrimSpace(provider.Alias)
		if alias == "" {
			return fmt.Errorf("upstream identity provider alias must not be empty")
		}
		if _, ok := seen[alias]; ok {
			return fmt.Errorf("duplicate upstream identity provider alias %q", alias)
		}
		seen[alias] = struct{}{}

		if provider.Type == "" {
			return fmt.Errorf("upstream identity provider %q: type is required", alias)
		}
		if provider.Type != pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC {
			return fmt.Errorf("upstream identity provider %q: unsupported type %q", alias, provider.Type)
		}
		if provider.OIDC == nil {
			return fmt.Errorf("upstream identity provider %q: oidc config is required", alias)
		}

		if err := validateOIDCUpstreamConfig(alias, provider.OIDC); err != nil {
			return err
		}

		if err := validateUpstreamEmailDomainRouting(alias, provider, seenDomains); err != nil {
			return err
		}
	}

	return nil
}

var emailDomainPattern = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)

func validateUpstreamEmailDomainRouting(
	alias string,
	provider *pmcorev1alpha1.UpstreamIdentityProvider,
	seenDomains map[string]string,
) error {
	routing := provider.EmailDomainRouting
	if routing == nil {
		return nil
	}

	domains := util.NormalizeEmailDomains(routing.Domains)
	if len(domains) == 0 {
		return fmt.Errorf(
			"upstream identity provider %q: emailDomainRouting.domains must not be empty",
			alias,
		)
	}

	for _, domain := range domains {
		if !emailDomainPattern.MatchString(domain) {
			return fmt.Errorf("upstream identity provider %q: invalid email domain %q", alias, domain)
		}
		// Backends match routing domains case-insensitively, so detect
		// cross-provider duplicates on the lowercased domain.
		key := strings.ToLower(domain)
		if otherAlias, ok := seenDomains[key]; ok {
			return fmt.Errorf(
				"email domain %q is already assigned to upstream identity provider %q",
				domain,
				otherAlias,
			)
		}
		seenDomains[key] = alias
	}

	return nil
}

func validateOIDCUpstreamConfig(alias string, oidc *pmcorev1alpha1.OIDCUpstreamConfig) error {
	hasDiscovery := strings.TrimSpace(oidc.DiscoveryURL) != ""
	hasManual := strings.TrimSpace(oidc.Issuer) != "" ||
		strings.TrimSpace(oidc.AuthorizationURL) != "" ||
		strings.TrimSpace(oidc.TokenURL) != ""

	if hasDiscovery && hasManual {
		return fmt.Errorf(
			"upstream identity provider %q: discoveryUrl and manual endpoint configuration are mutually exclusive",
			alias,
		)
	}
	if !hasDiscovery {
		if strings.TrimSpace(oidc.Issuer) == "" ||
			strings.TrimSpace(oidc.AuthorizationURL) == "" ||
			strings.TrimSpace(oidc.TokenURL) == "" {
			return fmt.Errorf(
				"upstream identity provider %q: either discoveryUrl or issuer, authorizationUrl, and tokenUrl must be set",
				alias,
			)
		}
	}

	if strings.TrimSpace(oidc.ClientID) == "" {
		return fmt.Errorf("upstream identity provider %q: clientId is required", alias)
	}

	if oidcClientAuthRequiresSecret(oidc.ClientAuthentication) &&
		strings.TrimSpace(oidc.ClientSecretRef.Name) == "" {
		return fmt.Errorf(
			"upstream identity provider %q: clientSecretRef.name is required for client authentication method %q",
			alias,
			oidc.ClientAuthentication,
		)
	}

	return nil
}

func oidcClientAuthRequiresSecret(method string) bool {
	switch strings.TrimSpace(method) {
	case "", "client_secret_basic", "client_secret_post", "client_secret_jwt":
		return true
	default:
		return false
	}
}
