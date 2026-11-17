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

package authentication

import (
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/require"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	"go.platform-mesh.io/golang-commons/identity"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCredentialOutputRequiresAnExplicitNamespace(t *testing.T) {
	class, tenant := readyIdentity()
	tenant.Spec.Clients[0].CredentialsSecretRef = &corev1.LocalObjectReference{Name: "client-output"}
	tenant.Status.Clients[0].CredentialsSecretRef = &corev1.SecretReference{Name: "client-output"}
	_, err := Resolve(class, tenant, "")
	require.ErrorContains(t, err, "credential output")
	tenant.Status.Clients[0].CredentialsSecretRef.Namespace = "default"
	_, err = Resolve(class, tenant, "")
	require.NoError(t, err)
}

func TestResolveUsesObservedIdentity(t *testing.T) {
	class, tenant := readyIdentity()
	authenticator, err := Resolve(class, tenant, "provider-specific-ca")
	require.NoError(t, err)
	require.Equal(t, "https://external.example/oidc/org-a/", authenticator.Issuer.URL)
	require.Equal(t, []string{"provider-assigned-id"}, authenticator.Issuer.Audiences)
	require.Equal(t, "provider-specific-ca", authenticator.Issuer.CertificateAuthority)
	require.Equal(t, identity.UsernameExpression, authenticator.ClaimMappings.Username.Expression)
	require.Empty(t, authenticator.ClaimMappings.Username.Claim)
	require.Equal(t, "[]", authenticator.ClaimMappings.Groups.Expression)
}

func TestResolveRejectsIncompleteIdentity(t *testing.T) {
	for name, modify := range map[string]func(*pmidentityv1alpha1.IdentityProviderClass, *pmidentityv1alpha1.IdentityTenant){
		"class recreated": func(c *pmidentityv1alpha1.IdentityProviderClass, _ *pmidentityv1alpha1.IdentityTenant) {
			c.UID = "replacement-uid"
		},
		"wrong controller": func(_ *pmidentityv1alpha1.IdentityProviderClass, it *pmidentityv1alpha1.IdentityTenant) {
			it.Status.ControllerName = "another.example"
		},
		"stale tenant": func(_ *pmidentityv1alpha1.IdentityProviderClass, it *pmidentityv1alpha1.IdentityTenant) {
			it.Generation++
		},
		"stale condition": func(_ *pmidentityv1alpha1.IdentityProviderClass, it *pmidentityv1alpha1.IdentityTenant) {
			it.Status.Conditions[0].ObservedGeneration = 0
		},
		"stale client": func(_ *pmidentityv1alpha1.IdentityProviderClass, it *pmidentityv1alpha1.IdentityTenant) {
			it.Status.Clients[0].ObservedGeneration = 0
		},
		"missing client": func(_ *pmidentityv1alpha1.IdentityProviderClass, it *pmidentityv1alpha1.IdentityTenant) {
			it.Status.Clients = nil
		},
		"empty audiences": func(_ *pmidentityv1alpha1.IdentityProviderClass, it *pmidentityv1alpha1.IdentityTenant) {
			it.Status.Clients[0].ClientID = ""
		},
		"removed client": func(_ *pmidentityv1alpha1.IdentityProviderClass, it *pmidentityv1alpha1.IdentityTenant) {
			it.Status.Clients[0].DeletionPending = true
		},
		"invalid issuer": func(_ *pmidentityv1alpha1.IdentityProviderClass, it *pmidentityv1alpha1.IdentityTenant) {
			it.Status.IssuerURL = "http://insecure.example"
		},
		"no issuer": func(_ *pmidentityv1alpha1.IdentityProviderClass, it *pmidentityv1alpha1.IdentityTenant) {
			it.Status.IssuerURL = ""
		},
		"unmanaged tenant": func(_ *pmidentityv1alpha1.IdentityProviderClass, it *pmidentityv1alpha1.IdentityTenant) {
			it.Status.ExternalID = ""
		},
		"pairwise subjects": func(_ *pmidentityv1alpha1.IdentityProviderClass, it *pmidentityv1alpha1.IdentityTenant) {
			it.Status.SubjectMode = pmidentityv1alpha1.SubjectModePairwise
		},
		"unknown subject mode": func(_ *pmidentityv1alpha1.IdentityProviderClass, it *pmidentityv1alpha1.IdentityTenant) {
			it.Status.SubjectMode = ""
		},
		"wrong contract": func(c *pmidentityv1alpha1.IdentityProviderClass, _ *pmidentityv1alpha1.IdentityTenant) {
			c.Status.ContractVersion = "v99"
		},
		"configuration changed": func(c *pmidentityv1alpha1.IdentityProviderClass, _ *pmidentityv1alpha1.IdentityTenant) {
			c.Status.ObservedConfigurationRevision = "new-revision"
		},
		"registration only": func(c *pmidentityv1alpha1.IdentityProviderClass, _ *pmidentityv1alpha1.IdentityTenant) {
			c.Status.Capabilities = []pmidentityv1alpha1.IdentityCapability{pmidentityv1alpha1.IdentityCapabilityOIDC, pmidentityv1alpha1.IdentityCapabilityDynamicClientRegistration}
		},
		"deleting tenant": func(_ *pmidentityv1alpha1.IdentityProviderClass, it *pmidentityv1alpha1.IdentityTenant) {
			now := metav1.Now()
			it.DeletionTimestamp = &now
		},
	} {
		t.Run(name, func(t *testing.T) {
			class, tenant := readyIdentity()
			modify(class, tenant)
			_, err := Resolve(class, tenant, "")
			require.Error(t, err)
		})
	}
}

func TestSharedIssuerRequiresTrustedMembership(t *testing.T) {
	class, tenant := readyIdentity()
	class.Spec.AuthenticationPolicy.IssuerMode = pmidentityv1alpha1.IssuerModeShared
	_, err := Resolve(class, tenant, "")
	require.Error(t, err)
	class.Status.Capabilities = append(class.Status.Capabilities, pmidentityv1alpha1.IdentityCapabilitySharedIssuer)
	_, err = Resolve(class, tenant, "")
	require.Error(t, err)
	tenant.Status.TenantClaims = []pmidentityv1alpha1.TenantClaim{{Name: "membership", Values: []string{"org-a"}}}
	_, err = Resolve(class, tenant, "")
	require.Error(t, err)
	class.Spec.AuthenticationPolicy.AllowedTenantClaims = []string{"membership"}
	authenticator, err := Resolve(class, tenant, "")
	require.NoError(t, err)

	env, err := cel.NewEnv(cel.Variable("claims", cel.DynType))
	require.NoError(t, err)
	for _, rule := range authenticator.ClaimValidationRules {
		ast, issues := env.Compile(rule.Expression)
		require.NoError(t, issues.Err())
		program, err := env.Program(ast)
		require.NoError(t, err)
		for _, test := range []struct {
			value any
			want  bool
		}{
			{"org-a", true}, {[]string{"org-b", "org-a"}, true},
			{"org-b", false}, {[]string{"org-b"}, false},
			{nil, false}, {42, false}, {map[string]any{"org-a": true}, false},
		} {
			claims := map[string]any{"iss": tenant.Status.IssuerURL, "sub": "subject-a"}
			if test.value != nil {
				claims["membership"] = test.value
			}
			value, _, err := program.Eval(map[string]any{"claims": claims})
			require.NoError(t, err)
			if strings.Contains(rule.Expression, `"membership" in claims`) {
				require.Equal(t, test.want, value.Value())
			} else {
				require.Equal(t, true, value.Value())
			}
		}
	}
}

func TestGroupsCannotPopulateReservedDomains(t *testing.T) {
	class, tenant := readyIdentity()
	class.Spec.AuthenticationPolicy.GroupClaim = "groups"
	_, err := Resolve(class, tenant, "")
	require.Error(t, err)
	class.Spec.AuthenticationPolicy.GroupPrefix = "system:"
	_, err = Resolve(class, tenant, "")
	require.Error(t, err)
	class.Spec.AuthenticationPolicy.GroupPrefix = "oidc:"
	authenticator, err := Resolve(class, tenant, "")
	require.NoError(t, err)
	require.Equal(t, "oidc:", *authenticator.ClaimMappings.Groups.Prefix)
}

func readyIdentity() (*pmidentityv1alpha1.IdentityProviderClass, *pmidentityv1alpha1.IdentityTenant) {
	readyConditions := func() []metav1.Condition {
		return []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, ObservedGeneration: 1}}
	}
	class := &pmidentityv1alpha1.IdentityProviderClass{
		ObjectMeta: metav1.ObjectMeta{Name: "default", UID: "class-uid", Generation: 1},
		Spec: pmidentityv1alpha1.IdentityProviderClassSpec{
			ControllerName: "keycloak.platform-mesh.io",
			ConfigurationRef: pmidentityv1alpha1.IdentityProviderObjectReference{
				APIGroup: "keycloak.platform-mesh.io", Kind: "ProviderConfiguration", Name: "default",
			},
			AuthenticationPolicy: pmidentityv1alpha1.AuthenticationPolicy{IssuerMode: pmidentityv1alpha1.IssuerModeDedicated},
		},
		Status: pmidentityv1alpha1.IdentityProviderClassStatus{
			ObservedGeneration: 1, ContractVersion: pmidentityv1alpha1.ContractVersion,
			ObservedConfigurationRevision: "config-v1", Conditions: readyConditions(),
			Capabilities: []pmidentityv1alpha1.IdentityCapability{
				pmidentityv1alpha1.IdentityCapabilityOIDC, pmidentityv1alpha1.IdentityCapabilityDynamicClientRegistration,
				pmidentityv1alpha1.IdentityCapabilityClientManagement, pmidentityv1alpha1.IdentityCapabilityTenantProvisioning,
			},
		},
	}
	tenant := &pmidentityv1alpha1.IdentityTenant{
		ObjectMeta: metav1.ObjectMeta{Name: "org-a", UID: "tenant-uid", Generation: 1},
		Spec: pmidentityv1alpha1.IdentityTenantSpec{
			ProviderClassRef: "default", Purpose: pmidentityv1alpha1.IdentityTenantPurposeOrganization,
			Organization: "org-a", DeletionPolicy: pmidentityv1alpha1.DeletionPolicyDelete,
			Clients: []pmidentityv1alpha1.OIDCClient{{
				Name: "cli", Purpose: pmidentityv1alpha1.IdentityClientPurposeCLI,
				GrantTypes: []string{"authorization_code"}, ResponseTypes: []string{"code"},
				RedirectURIs:            []string{"http://127.0.0.1:8000/callback"},
				TokenEndpointAuthMethod: pmidentityv1alpha1.ClientTokenEndpointAuthMethodNone,
			}},
		},
		Status: pmidentityv1alpha1.IdentityTenantStatus{
			ObservedGeneration: 1, ControllerName: class.Spec.ControllerName, BoundProviderClassUID: class.UID,
			ExternalID: "external-tenant-id", IssuerURL: "https://external.example/oidc/org-a/",
			SubjectMode: pmidentityv1alpha1.SubjectModePublic, ObservedConfigurationRevision: "config-v1",
			Conditions: readyConditions(), Clients: []pmidentityv1alpha1.ManagedClient{{
				Name: "cli", ClientID: "provider-assigned-id", ObservedGeneration: 1, Conditions: readyConditions(),
			}},
		},
	}
	return class, tenant
}
