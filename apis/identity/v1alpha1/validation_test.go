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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestAddToScheme(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("add identity API types to scheme: %v", err)
	}

	for _, object := range []runtime.Object{
		&IdentityProviderClass{},
		&IdentityProviderClassList{},
		&IdentityTenant{},
		&IdentityTenantList{},
		&Invitation{},
		&InvitationList{},
	} {
		gvks, _, err := scheme.ObjectKinds(object)
		if err != nil {
			t.Fatalf("get kinds for %T: %v", object, err)
		}
		if len(gvks) != 1 || gvks[0].GroupVersion() != SchemeGroupVersion {
			t.Errorf("%T registered with kinds %v, want exactly %s", object, gvks, SchemeGroupVersion)
		}
	}
}

func TestValidateTenant(t *testing.T) {
	base := IdentityTenant{
		Spec: IdentityTenantSpec{
			ProviderClassRef: "corporate-idp",
			Purpose:          IdentityTenantPurposeOrganization,
			Organization:     "example-org",
			DeletionPolicy:   DeletionPolicyRetain,
			Clients: []OIDCClient{{
				Name:                    "portal",
				Purpose:                 IdentityClientPurposePortal,
				RedirectURIs:            []string{"https://portal.example.com/callback"},
				PostLogoutRedirectURIs:  []string{"https://portal.example.com/"},
				GrantTypes:              []string{"authorization_code", "refresh_token"},
				ResponseTypes:           []string{"code"},
				Scopes:                  []string{"openid", "profile"},
				TokenEndpointAuthMethod: ClientTokenEndpointAuthMethodClientSecretBasic,
				CredentialsSecretRef:    &corev1.LocalObjectReference{Name: "portal-credentials"},
			}},
		},
	}

	if errs := ValidateTenant(&base); len(errs) != 0 {
		t.Fatalf("valid tenant returned errors: %v", errs)
	}
	loopbackTenant := cloneTenant(base)
	loopbackTenant.Spec.Clients[0].RedirectURIs = []string{
		"http://localhost/callback",
		"http://127.0.0.1:8080/callback",
		"http://[::1]:8080/callback",
	}
	if errs := ValidateTenant(loopbackTenant); len(errs) != 0 {
		t.Fatalf("loopback HTTP redirects returned errors: %v", errs)
	}

	tests := []struct {
		name      string
		mutate    func(*IdentityTenant)
		wantField string
	}{
		{
			name: "duplicate stable client name",
			mutate: func(tenant *IdentityTenant) {
				tenant.Spec.Clients = append(tenant.Spec.Clients, tenant.Spec.Clients[0])
			},
			wantField: "spec.clients[1].name",
		},
		{
			name: "wildcard redirect",
			mutate: func(tenant *IdentityTenant) {
				tenant.Spec.Clients[0].RedirectURIs = []string{"https://*.example.com/callback"}
			},
			wantField: "spec.clients[0].redirectUris[0]",
		},
		{
			name: "insecure non-loopback redirect",
			mutate: func(tenant *IdentityTenant) {
				tenant.Spec.Clients[0].RedirectURIs = []string{"http://portal.example.com/callback"}
			},
			wantField: "spec.clients[0].redirectUris[0]",
		},
		{
			name: "redirect with user info",
			mutate: func(tenant *IdentityTenant) {
				tenant.Spec.Clients[0].RedirectURIs = []string{"https://user@portal.example.com/callback"}
			},
			wantField: "spec.clients[0].redirectUris[0]",
		},
		{
			name: "redirect with fragment",
			mutate: func(tenant *IdentityTenant) {
				tenant.Spec.Clients[0].RedirectURIs = []string{"https://portal.example.com/callback#"}
			},
			wantField: "spec.clients[0].redirectUris[0]",
		},
		{
			name: "unsupported grant type",
			mutate: func(tenant *IdentityTenant) {
				tenant.Spec.Clients[0].GrantTypes = []string{"not-a-grant"}
			},
			wantField: "spec.clients[0].grantTypes[0]",
		},
		{
			name: "scope containing whitespace",
			mutate: func(tenant *IdentityTenant) {
				tenant.Spec.Clients[0].Scopes = []string{"openid profile"}
			},
			wantField: "spec.clients[0].scopes[0]",
		},
		{
			name: "confidential client without credentials reference",
			mutate: func(tenant *IdentityTenant) {
				tenant.Spec.Clients[0].CredentialsSecretRef = nil
			},
			wantField: "spec.clients[0].credentialsSecretRef",
		},
		{
			name: "clients sharing credentials secret",
			mutate: func(tenant *IdentityTenant) {
				client := tenant.Spec.Clients[0]
				client.Name = "cli"
				tenant.Spec.Clients = append(tenant.Spec.Clients, client)
			},
			wantField: "spec.clients[1].credentialsSecretRef.name",
		},
		{
			name: "nonstandard device grant",
			mutate: func(tenant *IdentityTenant) {
				tenant.Spec.Clients[0].GrantTypes = []string{"device_code"}
			},
			wantField: "spec.clients[0].grantTypes[0]",
		},
		{
			name: "authorization code without response",
			mutate: func(tenant *IdentityTenant) {
				tenant.Spec.Clients[0].ResponseTypes = nil
			},
			wantField: "spec.clients[0].grantTypes",
		},
		{
			name: "authorization code without callback",
			mutate: func(tenant *IdentityTenant) {
				tenant.Spec.Clients[0].RedirectURIs = nil
			},
			wantField: "spec.clients[0].grantTypes",
		},
		{
			name: "public client with client credentials grant",
			mutate: func(tenant *IdentityTenant) {
				tenant.Spec.Clients[0].TokenEndpointAuthMethod = ClientTokenEndpointAuthMethodNone
				tenant.Spec.Clients[0].GrantTypes = []string{"client_credentials"}
			},
			wantField: "spec.clients[0].grantTypes",
		},
		{
			name: "code response without matching grant",
			mutate: func(tenant *IdentityTenant) {
				tenant.Spec.Clients[0].GrantTypes = []string{"client_credentials"}
			},
			wantField: "spec.clients[0].responseTypes",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tenant := cloneTenant(base)
			test.mutate(tenant)
			if got := errorFields(ValidateTenant(tenant)); !contains(got, test.wantField) {
				t.Fatalf("validation fields %v do not include %q", got, test.wantField)
			}
		})
	}
	deviceTenant := cloneTenant(base)
	deviceTenant.Spec.Clients[0].TokenEndpointAuthMethod = ClientTokenEndpointAuthMethodNone
	deviceTenant.Spec.Clients[0].GrantTypes = []string{"urn:ietf:params:oauth:grant-type:device_code"}
	deviceTenant.Spec.Clients[0].ResponseTypes = nil
	if errs := ValidateTenant(deviceTenant); len(errs) != 0 {
		t.Fatalf("standard device grant returned errors: %v", errs)
	}
}

func TestValidateProviderClass(t *testing.T) {
	class := &IdentityProviderClass{
		Spec: IdentityProviderClassSpec{
			ControllerName: "identity.example.com",
			ConfigurationRef: IdentityProviderObjectReference{
				APIGroup: "identity.example.com",
				Kind:     "ProviderConfiguration",
				Name:     "primary",
			},
			AuthenticationPolicy: AuthenticationPolicy{
				IssuerMode:          IssuerModeShared,
				GroupClaim:          "groups",
				GroupPrefix:         "tenant:",
				AllowedTenantClaims: []string{"tenant_id"},
			},
			Directory: &IdentityEndpointConfig{URL: "https://directory.example.com"},
		},
	}
	if errs := ValidateProviderClass(class); len(errs) != 0 {
		t.Fatalf("valid provider class returned errors: %v", errs)
	}

	class.Spec.AuthenticationPolicy.GroupPrefix = ""
	if got := errorFields(ValidateProviderClass(class)); !contains(got, "spec.authenticationPolicy.groupPrefix") {
		t.Fatalf("validation fields %v do not include missing group prefix", got)
	}
	class.Spec.AuthenticationPolicy.GroupPrefix = "system:masters"
	class.Spec.AuthenticationPolicy.AllowedTenantClaims = []string{"tenant id", "tenant_id", "tenant_id"}
	if got := errorFields(ValidateProviderClass(class)); !contains(got, "spec.authenticationPolicy.groupPrefix") || !contains(got, "spec.authenticationPolicy.allowedTenantClaims[0]") || !contains(got, "spec.authenticationPolicy.allowedTenantClaims[2]") {
		t.Fatalf("validation fields %v do not include invalid group prefix and tenant claims", got)
	}
	class.Spec.AuthenticationPolicy.GroupPrefix = "tenant:"
	class.Spec.AuthenticationPolicy.AllowedTenantClaims = []string{"tenant_id"}

	class.Spec.Directory.URL = "http://directory.example.com"
	class.Spec.Validation = &IdentityEndpointConfig{URL: "https://user@example.com/validate"}
	errs := ValidateProviderClass(class)
	if got := errorFields(errs); !contains(got, "spec.directory.url") || !contains(got, "spec.validation.url") {
		t.Fatalf("validation fields %v do not include both invalid endpoint URLs", got)
	}
}

func TestValidateInvitation(t *testing.T) {
	valid := &Invitation{
		Spec: InvitationSpec{
			Email:      "person@example.com",
			TenantRef:  "example-org",
			ClientName: "portal",
			ExpiresAt:  &metav1.Time{},
		},
	}
	if errs := ValidateInvitation(valid); len(errs) != 0 {
		t.Fatalf("valid invitation returned errors: %v", errs)
	}

	valid.Spec.Email = "Person <person@example.com>"
	valid.Spec.TenantRef = "root:orgs:example-org"
	if got := errorFields(ValidateInvitation(valid)); !contains(got, "spec.email") || !contains(got, "spec.tenantRef") {
		t.Fatalf("validation fields %v do not include invalid email and tenant reference", got)
	}
}

func errorFields(errs field.ErrorList) []string {
	fields := make([]string, 0, len(errs))
	for _, err := range errs {
		fields = append(fields, err.Field)
	}
	return fields
}

func contains(errors []string, wanted string) bool {
	for _, path := range errors {
		if strings.Contains(path, wanted) {
			return true
		}
	}
	return false
}

func cloneTenant(tenant IdentityTenant) *IdentityTenant {
	clone := tenant
	clone.Spec.Clients = append([]OIDCClient(nil), tenant.Spec.Clients...)
	for i := range clone.Spec.Clients {
		clone.Spec.Clients[i].RedirectURIs = append([]string(nil), tenant.Spec.Clients[i].RedirectURIs...)
		clone.Spec.Clients[i].PostLogoutRedirectURIs = append([]string(nil), tenant.Spec.Clients[i].PostLogoutRedirectURIs...)
		clone.Spec.Clients[i].GrantTypes = append([]string(nil), tenant.Spec.Clients[i].GrantTypes...)
		clone.Spec.Clients[i].ResponseTypes = append([]string(nil), tenant.Spec.Clients[i].ResponseTypes...)
		clone.Spec.Clients[i].Scopes = append([]string(nil), tenant.Spec.Clients[i].Scopes...)
	}
	return &clone
}
