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

package v1alpha1

import (
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

var (
	kindNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)
	scopeToken      = regexp.MustCompile(`^[\x21\x23-\x5B\x5D-\x7E]+$`)
	claimName       = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.:-]*$`)
)

// ValidateProviderClass checks provider-class fields that need semantic
// validation beyond the structural schema markers.
func ValidateProviderClass(class *IdentityProviderClass) field.ErrorList {
	if class == nil {
		return field.ErrorList{field.Required(field.NewPath(""), "provider class must not be nil")}
	}

	var errs field.ErrorList
	specPath := field.NewPath("spec")
	if class.Spec.ControllerName == "" {
		errs = append(errs, field.Required(specPath.Child("controllerName"), "controller name is required"))
	} else if problems := validation.IsDNS1123Subdomain(class.Spec.ControllerName); len(problems) > 0 {
		errs = append(errs, field.Invalid(specPath.Child("controllerName"), class.Spec.ControllerName, strings.Join(problems, "; ")))
	}
	refPath := specPath.Child("configurationRef")
	if class.Spec.ConfigurationRef.APIGroup == "" {
		errs = append(errs, field.Required(refPath.Child("apiGroup"), "API group is required"))
	} else if problems := validation.IsDNS1123Subdomain(class.Spec.ConfigurationRef.APIGroup); len(problems) > 0 {
		errs = append(errs, field.Invalid(refPath.Child("apiGroup"), class.Spec.ConfigurationRef.APIGroup, strings.Join(problems, "; ")))
	}
	if !kindNamePattern.MatchString(class.Spec.ConfigurationRef.Kind) {
		errs = append(errs, field.Invalid(refPath.Child("kind"), class.Spec.ConfigurationRef.Kind, "must be a kind name"))
	}
	if problems := validation.IsDNS1123Subdomain(class.Spec.ConfigurationRef.Name); len(problems) > 0 {
		errs = append(errs, field.Invalid(refPath.Child("name"), class.Spec.ConfigurationRef.Name, strings.Join(problems, "; ")))
	}

	switch class.Spec.AuthenticationPolicy.IssuerMode {
	case IssuerModeDedicated, IssuerModeShared:
	default:
		errs = append(errs, field.NotSupported(specPath.Child("authenticationPolicy", "issuerMode"), class.Spec.AuthenticationPolicy.IssuerMode, []string{string(IssuerModeDedicated), string(IssuerModeShared)}))
	}
	if claim := class.Spec.AuthenticationPolicy.GroupClaim; claim != "" && !claimName.MatchString(claim) {
		errs = append(errs, field.Invalid(specPath.Child("authenticationPolicy", "groupClaim"), claim, "must be a non-empty claim name without whitespace"))
	}
	policy := class.Spec.AuthenticationPolicy
	if policy.GroupClaim != "" && policy.GroupPrefix == "" {
		errs = append(errs, field.Required(specPath.Child("authenticationPolicy", "groupPrefix"), "a group claim requires a group prefix"))
	}
	if strings.HasPrefix(policy.GroupPrefix, "system:") {
		errs = append(errs, field.Invalid(specPath.Child("authenticationPolicy", "groupPrefix"), policy.GroupPrefix, "system: is reserved"))
	}
	claimSeen := make(map[string]struct{}, len(policy.AllowedTenantClaims))
	for i, claim := range policy.AllowedTenantClaims {
		path := specPath.Child("authenticationPolicy", "allowedTenantClaims").Index(i)
		if !claimName.MatchString(claim) {
			errs = append(errs, field.Invalid(path, claim, "must be a non-empty claim name without whitespace"))
		}
		if _, exists := claimSeen[claim]; exists {
			errs = append(errs, field.Duplicate(path, claim))
		}
		claimSeen[claim] = struct{}{}
	}

	for _, endpoint := range []struct {
		name   string
		config *IdentityEndpointConfig
	}{
		{name: "directory", config: class.Spec.Directory},
		{name: "validation", config: class.Spec.Validation},
	} {
		if endpoint.config == nil {
			continue
		}
		errs = append(errs, validateEndpointURL(specPath.Child(endpoint.name, "url"), endpoint.config.URL)...)
	}
	return errs
}

// ValidateTenant checks client identifiers, OAuth metadata, and exact redirect
// URI constraints for an IdentityTenant.
func ValidateTenant(tenant *IdentityTenant) field.ErrorList {
	if tenant == nil {
		return field.ErrorList{field.Required(field.NewPath(""), "identity tenant must not be nil")}
	}

	var errs field.ErrorList
	specPath := field.NewPath("spec")
	if problems := validation.IsDNS1123Subdomain(tenant.Spec.ProviderClassRef); len(problems) > 0 {
		errs = append(errs, field.Invalid(specPath.Child("providerClassRef"), tenant.Spec.ProviderClassRef, strings.Join(problems, "; ")))
	}
	switch tenant.Spec.Purpose {
	case IdentityTenantPurposeWelcome:
	case IdentityTenantPurposeOrganization:
		if tenant.Spec.Organization == "" {
			errs = append(errs, field.Required(specPath.Child("organization"), "organization is required for an Organization tenant"))
		}
	default:
		errs = append(errs, field.NotSupported(specPath.Child("purpose"), tenant.Spec.Purpose, []string{string(IdentityTenantPurposeWelcome), string(IdentityTenantPurposeOrganization)}))
	}
	if tenant.Spec.Organization != "" {
		if problems := validation.IsDNS1123Subdomain(tenant.Spec.Organization); len(problems) > 0 {
			errs = append(errs, field.Invalid(specPath.Child("organization"), tenant.Spec.Organization, strings.Join(problems, "; ")))
		}
	}
	switch tenant.Spec.DeletionPolicy {
	case "", DeletionPolicyDelete, DeletionPolicyRetain:
	default:
		errs = append(errs, field.NotSupported(specPath.Child("deletionPolicy"), tenant.Spec.DeletionPolicy, []string{string(DeletionPolicyDelete), string(DeletionPolicyRetain)}))
	}

	seen := make(map[string]struct{}, len(tenant.Spec.Clients))
	seenSecrets := make(map[string]struct{}, len(tenant.Spec.Clients))
	for i := range tenant.Spec.Clients {
		client := &tenant.Spec.Clients[i]
		clientPath := specPath.Child("clients").Index(i)
		if problems := validation.IsDNS1123Label(client.Name); len(problems) > 0 {
			errs = append(errs, field.Invalid(clientPath.Child("name"), client.Name, strings.Join(problems, "; ")))
		}
		if _, exists := seen[client.Name]; exists {
			errs = append(errs, field.Duplicate(clientPath.Child("name"), client.Name))
		} else {
			seen[client.Name] = struct{}{}
		}
		switch client.Purpose {
		case IdentityClientPurposePortal, IdentityClientPurposeCLI, IdentityClientPurposeApplication:
		default:
			errs = append(errs, field.NotSupported(clientPath.Child("purpose"), client.Purpose, []string{string(IdentityClientPurposePortal), string(IdentityClientPurposeCLI), string(IdentityClientPurposeApplication)}))
		}

		switch client.TokenEndpointAuthMethod {
		case ClientTokenEndpointAuthMethodNone, ClientTokenEndpointAuthMethodClientSecretBasic, ClientTokenEndpointAuthMethodClientSecretPost:
		default:
			errs = append(errs, field.NotSupported(clientPath.Child("tokenEndpointAuthMethod"), client.TokenEndpointAuthMethod, []string{string(ClientTokenEndpointAuthMethodNone), string(ClientTokenEndpointAuthMethodClientSecretBasic), string(ClientTokenEndpointAuthMethodClientSecretPost)}))
		}
		if client.CredentialsSecretRef == nil {
			if client.TokenEndpointAuthMethod == ClientTokenEndpointAuthMethodClientSecretBasic || client.TokenEndpointAuthMethod == ClientTokenEndpointAuthMethodClientSecretPost {
				errs = append(errs, field.Required(clientPath.Child("credentialsSecretRef"), "confidential clients require a credentials Secret"))
			}
		} else {
			secretName := client.CredentialsSecretRef.Name
			secretPath := clientPath.Child("credentialsSecretRef", "name")
			if problems := validation.IsDNS1123Subdomain(secretName); len(problems) > 0 {
				errs = append(errs, field.Invalid(secretPath, secretName, strings.Join(problems, "; ")))
			}
			if _, exists := seenSecrets[secretName]; exists {
				errs = append(errs, field.Duplicate(secretPath, secretName))
			}
			seenSecrets[secretName] = struct{}{}
		}
		if len(client.GrantTypes) == 0 {
			errs = append(errs, field.Required(clientPath.Child("grantTypes"), "at least one grant type is required"))
		}
		errs = append(errs, validateUniqueSupportedValues(clientPath.Child("grantTypes"), client.GrantTypes, map[string]struct{}{
			"authorization_code": {},
			"client_credentials": {},
			"implicit":           {},
			"password":           {},
			"refresh_token":      {},
			"urn:ietf:params:oauth:grant-type:device_code": {},
		})...)
		grants := make(map[string]bool, len(client.GrantTypes))
		for _, grant := range client.GrantTypes {
			grants[grant] = true
		}
		hasCode := false
		for _, response := range client.ResponseTypes {
			parts := strings.Fields(response)
			for _, part := range parts {
				switch part {
				case "code":
					hasCode = true
					if !grants["authorization_code"] {
						errs = append(errs, field.Invalid(clientPath.Child("responseTypes"), client.ResponseTypes, "code responses require the authorization_code grant"))
					}
				case "token", "id_token":
					if !grants["implicit"] {
						errs = append(errs, field.Invalid(clientPath.Child("responseTypes"), client.ResponseTypes, "token and id_token responses require the implicit grant"))
					}
				}
			}
		}
		if grants["authorization_code"] && (!hasCode || len(client.RedirectURIs) == 0) {
			errs = append(errs, field.Invalid(clientPath.Child("grantTypes"), client.GrantTypes, "authorization_code requires a code response and a redirect URI"))
		}
		if client.TokenEndpointAuthMethod == ClientTokenEndpointAuthMethodNone && (grants["client_credentials"] || grants["password"]) {
			errs = append(errs, field.Invalid(clientPath.Child("grantTypes"), client.GrantTypes, "client_credentials and password grants require client authentication"))
		}
		errs = append(errs, validateUniqueSupportedValues(clientPath.Child("responseTypes"), client.ResponseTypes, map[string]struct{}{
			"code":                {},
			"token":               {},
			"id_token":            {},
			"code token":          {},
			"code id_token":       {},
			"id_token token":      {},
			"code id_token token": {},
		})...)

		scopeSeen := make(map[string]struct{}, len(client.Scopes))
		for j, scope := range client.Scopes {
			path := clientPath.Child("scopes").Index(j)
			if !scopeToken.MatchString(scope) {
				errs = append(errs, field.Invalid(path, scope, "must be a valid OAuth scope token"))
			}
			if _, exists := scopeSeen[scope]; exists {
				errs = append(errs, field.Duplicate(path, scope))
			} else {
				scopeSeen[scope] = struct{}{}
			}
		}
		for _, uris := range []struct {
			name  string
			paths []string
		}{
			{name: "redirectUris", paths: client.RedirectURIs},
			{name: "postLogoutRedirectUris", paths: client.PostLogoutRedirectURIs},
		} {
			for j, rawURL := range uris.paths {
				errs = append(errs, validateRedirectURL(clientPath.Child(uris.name).Index(j), rawURL)...)
			}
		}
	}
	return errs
}

// ValidateInvitation validates the invitation's delivery address and the
// workspace-independent tenant and client references.
func ValidateInvitation(invitation *Invitation) field.ErrorList {
	if invitation == nil {
		return field.ErrorList{field.Required(field.NewPath(""), "invitation must not be nil")}
	}

	var errs field.ErrorList
	specPath := field.NewPath("spec")
	addr, err := mail.ParseAddress(invitation.Spec.Email)
	if err != nil || addr.Address != invitation.Spec.Email {
		errs = append(errs, field.Invalid(specPath.Child("email"), invitation.Spec.Email, "must be a single email address without a display name"))
	}
	if problems := validation.IsDNS1123Subdomain(invitation.Spec.TenantRef); len(problems) > 0 {
		errs = append(errs, field.Invalid(specPath.Child("tenantRef"), invitation.Spec.TenantRef, strings.Join(problems, "; ")))
	}
	if problems := validation.IsDNS1123Label(invitation.Spec.ClientName); len(problems) > 0 {
		errs = append(errs, field.Invalid(specPath.Child("clientName"), invitation.Spec.ClientName, strings.Join(problems, "; ")))
	}
	return errs
}

func validateEndpointURL(path *field.Path, rawURL string) field.ErrorList {
	u, err := url.Parse(rawURL)
	if err != nil || u == nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.Contains(rawURL, "#") {
		return field.ErrorList{field.Invalid(path, rawURL, "must be an absolute URL without user information or a fragment")}
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && (scheme != "http" || !isLoopbackHost(u.Hostname())) {
		return field.ErrorList{field.Invalid(path, rawURL, "must use HTTPS, except HTTP is allowed for loopback hosts")}
	}
	return nil
}

func validateRedirectURL(path *field.Path, rawURL string) field.ErrorList {
	if strings.Contains(rawURL, "*") {
		return field.ErrorList{field.Invalid(path, rawURL, "wildcard redirect URLs are not allowed")}
	}
	return validateEndpointURL(path, rawURL)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validateUniqueSupportedValues(path *field.Path, values []string, supported map[string]struct{}) field.ErrorList {
	var errs field.ErrorList
	seen := make(map[string]struct{}, len(values))
	for i, value := range values {
		itemPath := path.Index(i)
		if _, ok := supported[value]; !ok {
			errs = append(errs, field.NotSupported(itemPath, value, mapKeys(supported)))
		}
		if _, exists := seen[value]; exists {
			errs = append(errs, field.Duplicate(itemPath, value))
		} else {
			seen[value] = struct{}{}
		}
	}
	return errs
}

func mapKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return keys
}
