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
	return nil, validateIdPRegistration(reg)
}

func (v *idpRegistrationValidator) ValidateUpdate(_ context.Context, oldObj, newObj *pmcorev1alpha1.IdPRegistration) (admission.Warnings, error) {
	if strings.TrimSpace(oldObj.Spec.Alias) != strings.TrimSpace(newObj.Spec.Alias) {
		return nil, fmt.Errorf("spec.alias is immutable")
	}
	return nil, validateIdPRegistration(newObj)
}

func (v *idpRegistrationValidator) ValidateDelete(context.Context, *pmcorev1alpha1.IdPRegistration) (admission.Warnings, error) {
	return nil, nil
}

func validateIdPRegistration(reg *pmcorev1alpha1.IdPRegistration) error {
	spec := reg.Spec
	alias := strings.TrimSpace(spec.Alias)
	if alias == "" {
		return fmt.Errorf("alias must not be empty")
	}
	if strings.TrimSpace(reg.Name) != alias {
		return fmt.Errorf("spec.alias must match metadata.name")
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
		if err := util.ValidateDiscoveryURL(spec.OIDC.DiscoveryURL); err != nil {
			return err
		}
	} else {
		if strings.TrimSpace(spec.OIDC.Issuer) == "" ||
			strings.TrimSpace(spec.OIDC.AuthorizationURL) == "" ||
			strings.TrimSpace(spec.OIDC.TokenURL) == "" {
			return fmt.Errorf("either discoveryUrl or issuer, authorizationUrl, and tokenUrl must be set")
		}
		if strings.TrimSpace(spec.OIDC.JWKSURL) == "" {
			return fmt.Errorf("oidc.jwksUrl is required for manual OIDC configuration")
		}
		for _, u := range []string{spec.OIDC.Issuer, spec.OIDC.AuthorizationURL, spec.OIDC.TokenURL, spec.OIDC.JWKSURL} {
			if u == "" {
				continue
			}
			if err := util.ValidateHTTPSURL(u); err != nil {
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
