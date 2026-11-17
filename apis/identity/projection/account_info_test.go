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

package projection

import (
	"testing"

	"github.com/stretchr/testify/require"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kcp-dev/logicalcluster/v3"
)

func TestPublicProjectionUsesObservedIdentityAndDetachedClientMetadata(t *testing.T) {
	class, tenant := projectionFixture()
	output, err := AccountInfo(class, tenant)
	require.NoError(t, err)
	require.Equal(t, tenant.Status.IssuerURL, output.IssuerURL)
	require.Equal(t, tenant.UID, output.Identity.Tenant.UID)
	require.Equal(t, "tenant-cluster", output.Identity.Tenant.LogicalCluster)
	require.Equal(t, class.UID, output.Identity.ProviderClassUID)
	require.Equal(t, tenant.Status.DiscoveryURL, output.Identity.DiscoveryURL)
	client := output.Clients["portal"]
	require.Equal(t, tenant.Status.Clients[0].ClientID, client.ClientID)
	require.Equal(t, "consumer-namespace", client.CredentialsSecretRef.Namespace)
	client.Scopes[0] = "modified"
	client.CredentialsSecretRef.Name = "modified"
	require.Equal(t, "openid", tenant.Spec.Clients[0].Scopes[0])
	require.Equal(t, "consumer-output", tenant.Status.Clients[0].CredentialsSecretRef.Name)
}

func TestPublicProjectionRejectsStaleOrUnboundIdentity(t *testing.T) {
	for _, edit := range []func(*pmidentityv1alpha1.IdentityTenant){
		func(tenant *pmidentityv1alpha1.IdentityTenant) { tenant.UID = "" },
		func(tenant *pmidentityv1alpha1.IdentityTenant) { tenant.Annotations = nil },
		func(tenant *pmidentityv1alpha1.IdentityTenant) { tenant.Status.ObservedConfigurationRevision = "old" },
		func(tenant *pmidentityv1alpha1.IdentityTenant) {
			tenant.Status.Clients[0].CredentialsSecretRef.Namespace = ""
		},
	} {
		class, tenant := projectionFixture()
		edit(tenant)
		_, err := AccountInfo(class, tenant)
		require.Error(t, err)
	}
}

func projectionFixture() (*pmidentityv1alpha1.IdentityProviderClass, *pmidentityv1alpha1.IdentityTenant) {
	conditions := []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, ObservedGeneration: 1}}
	class := &pmidentityv1alpha1.IdentityProviderClass{
		ObjectMeta: metav1.ObjectMeta{Name: "default", UID: "class-uid", Generation: 1},
		Spec: pmidentityv1alpha1.IdentityProviderClassSpec{
			ControllerName: "independent-provider.example",
			ConfigurationRef: pmidentityv1alpha1.IdentityProviderObjectReference{
				APIGroup: "independent-provider.example", Kind: "ProviderConfiguration", Name: "default",
			},
			AuthenticationPolicy: pmidentityv1alpha1.AuthenticationPolicy{IssuerMode: pmidentityv1alpha1.IssuerModeDedicated},
		},
		Status: pmidentityv1alpha1.IdentityProviderClassStatus{
			ContractVersion: pmidentityv1alpha1.ContractVersion, ObservedGeneration: 1, ObservedConfigurationRevision: "config-v1",
			Conditions: conditions,
			Capabilities: []pmidentityv1alpha1.IdentityCapability{
				pmidentityv1alpha1.IdentityCapabilityOIDC, pmidentityv1alpha1.IdentityCapabilityDynamicClientRegistration,
				pmidentityv1alpha1.IdentityCapabilityClientManagement, pmidentityv1alpha1.IdentityCapabilityTenantProvisioning,
			},
		},
	}
	tenant := &pmidentityv1alpha1.IdentityTenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "org-a", UID: "tenant-uid", Generation: 1, Annotations: map[string]string{logicalcluster.AnnotationKey: "tenant-cluster"},
		},
		Spec: pmidentityv1alpha1.IdentityTenantSpec{
			ProviderClassRef: "default", Purpose: pmidentityv1alpha1.IdentityTenantPurposeOrganization, Organization: "org-a",
			Clients: []pmidentityv1alpha1.OIDCClient{{
				Name: "portal", Purpose: pmidentityv1alpha1.IdentityClientPurposePortal, Scopes: []string{"openid", "email"},
				GrantTypes: []string{"authorization_code"}, ResponseTypes: []string{"code"}, RedirectURIs: []string{"https://portal.example/callback"},
				TokenEndpointAuthMethod: pmidentityv1alpha1.ClientTokenEndpointAuthMethodClientSecretBasic,
				CredentialsSecretRef:    &corev1.LocalObjectReference{Name: "consumer-output"},
			}},
		},
		Status: pmidentityv1alpha1.IdentityTenantStatus{
			ObservedGeneration: 1, ObservedConfigurationRevision: "config-v1", ControllerName: class.Spec.ControllerName,
			BoundProviderClassUID: class.UID, IssuerURL: "https://independent.example/issuer/org-a/",
			DiscoveryURL: "https://independent.example/discovery/org-a", ExternalID: "external-tenant",
			SubjectMode: pmidentityv1alpha1.SubjectModePublic, Conditions: conditions,
			Clients: []pmidentityv1alpha1.ManagedClient{{
				Name: "portal", ClientID: "provider-assigned-id", ObservedGeneration: 1, Conditions: conditions,
				CredentialsSecretRef: &corev1.SecretReference{Name: "consumer-output", Namespace: "consumer-namespace"},
			}},
		},
	}
	return class, tenant
}
