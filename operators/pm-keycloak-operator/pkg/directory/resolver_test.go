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

package directory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	"go.platform-mesh.io/golang-commons/idpdirectory"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kcp-dev/logicalcluster/v3"
)

func TestKCPResolverBindsImmutableTenantAndClass(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*pmidentityv1alpha1.IdentityProviderClass, *pmidentityv1alpha1.IdentityTenant)
		deny   bool
	}{
		{name: "ready"},
		{name: "recreated tenant", change: func(_ *pmidentityv1alpha1.IdentityProviderClass, tenant *pmidentityv1alpha1.IdentityTenant) {
			tenant.UID = "new-uid"
		}, deny: true},
		{name: "other cluster", change: func(_ *pmidentityv1alpha1.IdentityProviderClass, tenant *pmidentityv1alpha1.IdentityTenant) {
			tenant.Annotations[logicalcluster.AnnotationKey] = "other-cluster"
		}, deny: true},
		{name: "other class", change: func(_ *pmidentityv1alpha1.IdentityProviderClass, tenant *pmidentityv1alpha1.IdentityTenant) {
			tenant.Spec.ProviderClassRef = "other"
		}, deny: true},
		{name: "other provider", change: func(class *pmidentityv1alpha1.IdentityProviderClass, _ *pmidentityv1alpha1.IdentityTenant) {
			class.Spec.ControllerName = "another.example"
		}, deny: true},
		{name: "welcome tenant", change: func(_ *pmidentityv1alpha1.IdentityProviderClass, tenant *pmidentityv1alpha1.IdentityTenant) {
			tenant.Spec.Purpose = pmidentityv1alpha1.IdentityTenantPurposeWelcome
		}, deny: true},
		{name: "recreated class", change: func(class *pmidentityv1alpha1.IdentityProviderClass, _ *pmidentityv1alpha1.IdentityTenant) {
			class.UID = "replacement-class"
		}, deny: true},
		{name: "stale tenant", change: func(_ *pmidentityv1alpha1.IdentityProviderClass, tenant *pmidentityv1alpha1.IdentityTenant) {
			tenant.Generation++
		}, deny: true},
		{name: "unfiltered shared issuer", change: func(class *pmidentityv1alpha1.IdentityProviderClass, _ *pmidentityv1alpha1.IdentityTenant) {
			class.Spec.AuthenticationPolicy.IssuerMode = pmidentityv1alpha1.IssuerModeShared
		}, deny: true},
		{name: "directory unsupported", change: func(class *pmidentityv1alpha1.IdentityProviderClass, _ *pmidentityv1alpha1.IdentityTenant) {
			class.Status.Capabilities = class.Status.Capabilities[:len(class.Status.Capabilities)-1]
		}, deny: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			class, tenant := directoryIdentity()
			if test.change != nil {
				test.change(class, tenant)
			}
			scheme := runtime.NewScheme()
			require.NoError(t, pmidentityv1alpha1.AddToScheme(scheme))
			classClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(class).Build()
			tenantClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tenant).Build()
			resolver := &KCPResolver{
				ClassName: "default", ClassReader: classClient,
				ReaderForCluster: func(_ context.Context, cluster logicalcluster.Name) (ctrlruntimeclient.Reader, error) {
					require.Equal(t, logicalcluster.Name("tenant-cluster"), cluster)
					return tenantClient, nil
				},
			}
			got, err := resolver.Resolve(t.Context(), idpdirectory.Tenant{LogicalCluster: "tenant-cluster", Name: "org-a", UID: "tenant-uid"})
			if test.deny {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "provider-realm", got.Realm)
			require.Equal(t, "https://issuer.example/realms/provider-realm", got.IssuerURL)
		})
	}
}

func directoryIdentity() (*pmidentityv1alpha1.IdentityProviderClass, *pmidentityv1alpha1.IdentityTenant) {
	conditions := []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, ObservedGeneration: 1}}
	class := &pmidentityv1alpha1.IdentityProviderClass{
		ObjectMeta: metav1.ObjectMeta{Name: "default", UID: "class-uid", Generation: 1},
		Spec: pmidentityv1alpha1.IdentityProviderClassSpec{
			ControllerName:       KeycloakControllerName,
			ConfigurationRef:     pmidentityv1alpha1.IdentityProviderObjectReference{APIGroup: "keycloak.platform-mesh.io", Kind: "ProviderConfiguration", Name: "default"},
			AuthenticationPolicy: pmidentityv1alpha1.AuthenticationPolicy{IssuerMode: pmidentityv1alpha1.IssuerModeDedicated},
		},
		Status: pmidentityv1alpha1.IdentityProviderClassStatus{
			ObservedGeneration: 1, ObservedConfigurationRevision: "config-v1", ContractVersion: pmidentityv1alpha1.ContractVersion,
			Conditions: conditions,
			Capabilities: []pmidentityv1alpha1.IdentityCapability{
				pmidentityv1alpha1.IdentityCapabilityOIDC, pmidentityv1alpha1.IdentityCapabilityDynamicClientRegistration,
				pmidentityv1alpha1.IdentityCapabilityClientManagement, pmidentityv1alpha1.IdentityCapabilityTenantProvisioning,
				pmidentityv1alpha1.IdentityCapabilityDirectory,
			},
		},
	}
	tenant := &pmidentityv1alpha1.IdentityTenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "org-a", UID: "tenant-uid", Generation: 1,
			Annotations: map[string]string{logicalcluster.AnnotationKey: "tenant-cluster"},
		},
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
			ObservedGeneration: 1, ObservedConfigurationRevision: "config-v1", BoundProviderClassUID: class.UID,
			ControllerName: KeycloakControllerName, ExternalID: "provider-realm",
			IssuerURL: "https://issuer.example/realms/provider-realm", SubjectMode: pmidentityv1alpha1.SubjectModePublic,
			Conditions: conditions, Clients: []pmidentityv1alpha1.ManagedClient{{
				Name: "cli", ClientID: "client-id", ObservedGeneration: 1, Conditions: conditions,
			}},
		},
	}
	return class, tenant
}
