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
	"net"
	"net/url"
	"regexp"
	"strings"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/security-operator/internal/util"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	mcruntime "sigs.k8s.io/multicluster-runtime"
)

var idpRegistrationEmailDomainPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*$`)

// SetupIdPRegistrationValidatingWebhookWithManager registers validation for IdPRegistration.
func SetupIdPRegistrationValidatingWebhookWithManager(mgr ctrl.Manager) error {
	return mcruntime.NewWebhookManagedBy(mgr, &pmcorev1alpha1.IdPRegistration{}).
		WithValidator(&idpRegistrationValidator{}).
		Complete()
}

var _ admission.Validator[*pmcorev1alpha1.IdPRegistration] = (*idpRegistrationValidator)(nil)

type idpRegistrationValidator struct{}

func (v *idpRegistrationValidator) ValidateCreate(_ context.Context, reg *pmcorev1alpha1.IdPRegistration) (admission.Warnings, error) {
	return nil, validateIdPRegistrationSpec(reg.Spec)
}

func (v *idpRegistrationValidator) ValidateUpdate(_ context.Context, _, newObj *pmcorev1alpha1.IdPRegistration) (admission.Warnings, error) {
	return nil, validateIdPRegistrationSpec(newObj.Spec)
}

func (v *idpRegistrationValidator) ValidateDelete(context.Context, *pmcorev1alpha1.IdPRegistration) (admission.Warnings, error) {
	return nil, nil
}

func validateIdPRegistrationSpec(spec pmcorev1alpha1.IdPRegistrationSpec) error {
	alias := strings.TrimSpace(spec.Alias)
	if alias == "" {
		return fmt.Errorf("alias must not be empty")
	}

	if spec.Type != pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC {
		return fmt.Errorf("unsupported provider type %q", spec.Type)
	}

	if spec.OIDC == nil {
		return fmt.Errorf("oidc config is required")
	}

	if strings.TrimSpace(spec.OIDC.ClientID) == "" {
		return fmt.Errorf("oidc.clientId is required")
	}

	if strings.TrimSpace(spec.OIDC.ClientSecretRef.Name) == "" {
		return fmt.Errorf("oidc.clientSecret or oidc.clientSecretRef.name is required")
	}

	hasDiscovery := strings.TrimSpace(spec.OIDC.DiscoveryURL) != ""
	hasManual := strings.TrimSpace(spec.OIDC.Issuer) != "" ||
		strings.TrimSpace(spec.OIDC.AuthorizationURL) != "" ||
		strings.TrimSpace(spec.OIDC.TokenURL) != ""

	if hasDiscovery && hasManual {
		return fmt.Errorf("discoveryUrl and manual endpoint configuration are mutually exclusive")
	}

	if hasDiscovery {
		if err := validateDiscoveryURL(spec.OIDC.DiscoveryURL); err != nil {
			return err
		}
	} else {
		if strings.TrimSpace(spec.OIDC.Issuer) == "" ||
			strings.TrimSpace(spec.OIDC.AuthorizationURL) == "" ||
			strings.TrimSpace(spec.OIDC.TokenURL) == "" {
			return fmt.Errorf("either discoveryUrl or issuer, authorizationUrl, and tokenUrl must be set")
		}
		for _, u := range []string{spec.OIDC.Issuer, spec.OIDC.AuthorizationURL, spec.OIDC.TokenURL, spec.OIDC.JWKSURL} {
			if u == "" {
				continue
			}
			if err := validateHTTPSURL(u); err != nil {
				return err
			}
		}
	}

	if spec.EmailDomainRouting != nil {
		if len(spec.EmailDomainRouting.Domains) == 0 {
			return fmt.Errorf("emailDomainRouting.domains must not be empty when emailDomainRouting is set")
		}
		for _, domain := range util.NormalizeEmailDomains(spec.EmailDomainRouting.Domains) {
			if !idpRegistrationEmailDomainPattern.MatchString(domain) {
				return fmt.Errorf("invalid email domain %q", domain)
			}
		}
	}

	return nil
}

func validateDiscoveryURL(raw string) error {
	if err := validateHTTPSURL(raw); err != nil {
		return fmt.Errorf("discoveryUrl: %w", err)
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("discoveryUrl: invalid URL")
	}
	if isBlockedHost(parsed.Hostname()) {
		return fmt.Errorf("discoveryUrl must not target internal or private addresses")
	}
	return nil
}

func validateHTTPSURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid URL %q", raw)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("URL %q must use https", raw)
	}
	if isBlockedHost(parsed.Hostname()) {
		return fmt.Errorf("URL %q must not target internal or private addresses", raw)
	}
	return nil
}

func isBlockedHost(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}
