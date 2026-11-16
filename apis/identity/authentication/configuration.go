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

// Package authentication builds provider-neutral authentication from observed identity state.
// Callers must resolve the administrator-owned class from the installation workspace.
package authentication

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	"go.platform-mesh.io/golang-commons/identity"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"

	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

// Resolve rejects stale, incomplete and unsupported identity profiles rather than
// publishing authentication with empty audiences or an inferred provider issuer.
// certificateAuthority is selected by the administrator for this provider, not
// implicitly copied from the platform ingress certificate.
func Resolve(class *pmidentityv1alpha1.IdentityProviderClass, tenant *pmidentityv1alpha1.IdentityTenant, certificateAuthority string) (kcptenancyv1alpha1.JWTAuthenticator, error) {
	var zero kcptenancyv1alpha1.JWTAuthenticator
	if errs := pmidentityv1alpha1.ValidateProviderClass(class); len(errs) != 0 {
		return zero, fmt.Errorf("invalid identity provider class: %w", errs.ToAggregate())
	}
	if errs := pmidentityv1alpha1.ValidateTenant(tenant); len(errs) != 0 {
		return zero, fmt.Errorf("invalid identity tenant: %w", errs.ToAggregate())
	}
	if class.Name != tenant.Spec.ProviderClassRef || class.UID == "" || class.UID != tenant.Status.BoundProviderClassUID ||
		class.Spec.ControllerName != tenant.Status.ControllerName {
		return zero, errors.New("identity tenant is not bound to the resolved provider class")
	}
	if class.DeletionTimestamp != nil || tenant.DeletionTimestamp != nil ||
		!ready(class.Status.Conditions, class.Generation, class.Status.ObservedGeneration) ||
		!ready(tenant.Status.Conditions, tenant.Generation, tenant.Status.ObservedGeneration) {
		return zero, errors.New("identity provider class and tenant must be ready at their current generations")
	}
	if class.Status.ContractVersion != pmidentityv1alpha1.ContractVersion ||
		class.Status.ObservedConfigurationRevision == "" ||
		class.Status.ObservedConfigurationRevision != tenant.Status.ObservedConfigurationRevision {
		return zero, errors.New("identity provider contract or configuration revision is not current")
	}
	for _, capability := range []pmidentityv1alpha1.IdentityCapability{
		pmidentityv1alpha1.IdentityCapabilityOIDC,
		pmidentityv1alpha1.IdentityCapabilityDynamicClientRegistration,
		pmidentityv1alpha1.IdentityCapabilityClientManagement,
		pmidentityv1alpha1.IdentityCapabilityTenantProvisioning,
	} {
		if !slices.Contains(class.Status.Capabilities, capability) {
			return zero, fmt.Errorf("identity provider does not support required capability %s", capability)
		}
	}
	if tenant.Status.SubjectMode != pmidentityv1alpha1.SubjectModePublic {
		return zero, errors.New("identity provider subject mode requires an unsupported cross-client mapping")
	}
	if tenant.Status.ExternalID == "" {
		return zero, errors.New("identity tenant has no external identity")
	}
	if err := (identity.Principal{Issuer: tenant.Status.IssuerURL, Subject: "issuer-validation"}).Validate(); err != nil {
		return zero, fmt.Errorf("invalid observed issuer: %w", err)
	}
	if tenant.Status.DiscoveryURL != "" {
		if err := (identity.Principal{Issuer: tenant.Status.DiscoveryURL, Subject: "discovery-validation"}).Validate(); err != nil {
			return zero, fmt.Errorf("invalid observed discovery URL: %w", err)
		}
		if tenant.Status.DiscoveryURL == tenant.Status.IssuerURL {
			return zero, errors.New("discovery URL must differ from the issuer URL")
		}
	}
	audiences, err := clientAudiences(tenant)
	if err != nil {
		return zero, err
	}
	claimRules, err := tenantRules(class, tenant)
	if err != nil {
		return zero, err
	}
	jwt := kcptenancyv1alpha1.JWTAuthenticator{
		Issuer: kcptenancyv1alpha1.Issuer{
			URL: tenant.Status.IssuerURL, DiscoveryURL: tenant.Status.DiscoveryURL,
			CertificateAuthority: certificateAuthority,
			Audiences:            audiences, AudienceMatchPolicy: kcptenancyv1alpha1.AudienceMatchPolicyMatchAny,
		},
		ClaimMappings: kcptenancyv1alpha1.ClaimMappings{
			Username: kcptenancyv1alpha1.PrefixedClaimOrExpression{Expression: identity.UsernameExpression},
			Groups:   kcptenancyv1alpha1.PrefixedClaimOrExpression{Expression: "[]"},
		},
		ClaimValidationRules: append([]kcptenancyv1alpha1.ClaimValidationRule{{
			Expression: identity.ClaimValidationExpression, Message: "OIDC issuer and subject must form a valid canonical principal",
		}}, claimRules...),
		UserValidationRules: []kcptenancyv1alpha1.UserValidationRule{{
			Expression: `!user.username.startsWith("system:") && !user.groups.exists(group, group.startsWith("system:"))`,
			Message:    "OIDC identities must not use reserved Kubernetes identity domains",
		}},
	}
	policy := class.Spec.AuthenticationPolicy
	if policy.GroupClaim != "" {
		jwt.ClaimMappings.Groups = kcptenancyv1alpha1.PrefixedClaimOrExpression{
			Claim: policy.GroupClaim, Prefix: ptr.To(policy.GroupPrefix),
		}
	}
	return jwt, nil
}

func ready(conditions []metav1.Condition, generation, observed int64) bool {
	condition := meta.FindStatusCondition(conditions, "Ready")
	return generation > 0 && generation == observed && condition != nil &&
		condition.Status == metav1.ConditionTrue && condition.ObservedGeneration == generation
}

func clientAudiences(tenant *pmidentityv1alpha1.IdentityTenant) ([]string, error) {
	managed := make(map[string]pmidentityv1alpha1.ManagedClient, len(tenant.Status.Clients))
	for _, client := range tenant.Status.Clients {
		if _, exists := managed[client.Name]; exists {
			return nil, fmt.Errorf("duplicate managed client %s", client.Name)
		}
		managed[client.Name] = client
	}
	audiences := make([]string, 0, len(tenant.Spec.Clients))
	for _, desired := range tenant.Spec.Clients {
		client, exists := managed[desired.Name]
		if !exists || client.ClientID == "" || client.DeletionPending ||
			!ready(client.Conditions, tenant.Generation, client.ObservedGeneration) {
			return nil, fmt.Errorf("identity client %s is not ready at the current generation", desired.Name)
		}
		if (desired.CredentialsSecretRef == nil) != (client.CredentialsSecretRef == nil) ||
			(desired.CredentialsSecretRef != nil &&
				(client.CredentialsSecretRef.Name != desired.CredentialsSecretRef.Name ||
					len(validation.IsDNS1123Label(client.CredentialsSecretRef.Namespace)) != 0)) {
			return nil, fmt.Errorf("identity client %s credential output is not current", desired.Name)
		}
		if slices.Contains(audiences, client.ClientID) {
			return nil, fmt.Errorf("identity client %s shares another client's identifier", desired.Name)
		}
		audiences = append(audiences, client.ClientID)
	}
	if len(audiences) == 0 {
		return nil, errors.New("identity tenant has no client audiences")
	}
	slices.Sort(audiences)
	return audiences, nil
}

func tenantRules(class *pmidentityv1alpha1.IdentityProviderClass, tenant *pmidentityv1alpha1.IdentityTenant) ([]kcptenancyv1alpha1.ClaimValidationRule, error) {
	if class.Spec.AuthenticationPolicy.IssuerMode == pmidentityv1alpha1.IssuerModeShared {
		if !slices.Contains(class.Status.Capabilities, pmidentityv1alpha1.IdentityCapabilitySharedIssuer) || len(tenant.Status.TenantClaims) == 0 {
			return nil, errors.New("shared issuer requires tenant enforcement capability and resolved claim constraints")
		}
	}
	rules := make([]kcptenancyv1alpha1.ClaimValidationRule, 0, len(tenant.Status.TenantClaims))
	seen := map[string]bool{}
	for _, claim := range tenant.Status.TenantClaims {
		if !slices.Contains(class.Spec.AuthenticationPolicy.AllowedTenantClaims, claim.Name) || seen[claim.Name] || len(claim.Values) == 0 {
			return nil, errors.New("tenant claim constraint is not administrator-approved, is duplicated, or has no values")
		}
		seen[claim.Name] = true
		values := make([]string, 0, len(claim.Values))
		for _, value := range claim.Values {
			if value == "" || slices.Contains(values, strconv.Quote(value)) {
				return nil, errors.New("tenant claim constraint contains empty or duplicate values")
			}
			values = append(values, strconv.Quote(value))
		}
		name := strconv.Quote(claim.Name)
		access := "claims[" + name + "]"
		allowed := "[" + strings.Join(values, ", ") + "]"
		expression := name + " in claims && ((type(" + access + ") == string && " + access + " in " + allowed +
			") || (type(" + access + ") == list && " + access +
			".exists(value, type(value) == string && value in " + allowed + ")))"
		rules = append(rules, kcptenancyv1alpha1.ClaimValidationRule{
			Expression: expression, Message: "token must satisfy the organization membership constraint",
		})
	}
	return rules, nil
}
