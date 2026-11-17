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

package orchestration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kcp-dev/logicalcluster/v3"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

func TestOrganizationIntentAndObservedIdentityDoNotActivateAuthentication(t *testing.T) {
	engine, workspace, class := organizationFixture(t)
	observation, err := engine.Reconcile(t.Context(), workspace, "org-cluster")
	require.ErrorIs(t, err, ErrIdentityNotReady)
	require.Nil(t, observation.OIDC)
	require.Nil(t, observation.Authenticator)
	require.Equal(t, "organization", observation.Tenant.Spec.Organization)
	require.True(t, observation.TenantCreated)
	require.Equal(t, "account-uid", string(observation.AccountUID))
	tenant := observation.Tenant
	readyOrganizationTenant(tenant, class)
	require.NoError(t, engine.Organizations.Status().Update(t.Context(), tenant))
	observation, err = engine.Reconcile(t.Context(), workspace, "org-cluster")
	require.NoError(t, err)
	require.False(t, observation.TenantCreated)
	require.Equal(t, tenant.Status.IssuerURL, observation.OIDC.IssuerURL)
	require.Equal(t, tenant.Status.IssuerURL, observation.Authenticator.Issuer.URL)
	require.Equal(t, []string{"assigned-portal"}, observation.Authenticator.Issuer.Audiences)
	require.Equal(t, tenant.UID, observation.OIDC.Identity.Tenant.UID)
	info := &pmcorev1alpha1.AccountInfo{}
	require.NoError(t, workspace.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "account"}, info))
	require.Equal(t, "https://legacy.example", info.Spec.OIDC.IssuerURL)
	authConfigurations := &kcptenancyv1alpha1.WorkspaceAuthenticationConfigurationList{}
	require.NoError(t, engine.Organizations.List(t.Context(), authConfigurations))
	require.Empty(t, authConfigurations.Items)
	require.True(t, meta.IsStatusConditionTrue(tenant.Status.Conditions, "Ready"))
}

func TestOrganizationIntentRejectsForeignAndRecreatedBindings(t *testing.T) {
	for _, edit := range []func(*testing.T, *OrganizationReconciler, ctrlruntimeclient.Client){
		func(t *testing.T, _ *OrganizationReconciler, workspace ctrlruntimeclient.Client) {
			info := &pmcorev1alpha1.AccountInfo{}
			require.NoError(t, workspace.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "account"}, info))
			info.Spec.Organization.OriginClusterId = "foreign-root"
			require.NoError(t, workspace.Update(t.Context(), info))
		},
		func(t *testing.T, engine *OrganizationReconciler, _ ctrlruntimeclient.Client) {
			rootWorkspace := &kcptenancyv1alpha1.Workspace{}
			require.NoError(t, engine.Organizations.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "organization"}, rootWorkspace))
			rootWorkspace.OwnerReferences[0].UID = "previous-account"
			require.NoError(t, engine.Organizations.Update(t.Context(), rootWorkspace))
		},
		func(t *testing.T, engine *OrganizationReconciler, _ ctrlruntimeclient.Client) {
			rootWorkspace := &kcptenancyv1alpha1.Workspace{}
			require.NoError(t, engine.Organizations.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "organization"}, rootWorkspace))
			rootWorkspace.Spec.Cluster = "another-cluster"
			require.NoError(t, engine.Organizations.Update(t.Context(), rootWorkspace))
		},
	} {
		engine, workspace, _ := organizationFixture(t)
		edit(t, engine, workspace)
		_, err := engine.Reconcile(t.Context(), workspace, "org-cluster")
		require.Error(t, err)
		tenants := &pmidentityv1alpha1.IdentityTenantList{}
		require.NoError(t, engine.Organizations.List(t.Context(), tenants))
		require.Empty(t, tenants.Items)
	}
	engine, workspace, _ := organizationFixture(t)
	observation, err := engine.Reconcile(t.Context(), workspace, "org-cluster")
	require.ErrorIs(t, err, ErrIdentityNotReady)
	observation.Tenant.Annotations[workspaceUIDAnnotation] = "previous-logical-cluster"
	require.NoError(t, engine.Organizations.Update(t.Context(), observation.Tenant))
	_, err = engine.Reconcile(t.Context(), workspace, "org-cluster")
	require.ErrorContains(t, err, "implicit adoption is forbidden")
}

func TestOrganizationIntentUpdatesCannotReuseStaleReadinessOrARecreatedClass(t *testing.T) {
	engine, workspace, class := organizationFixture(t)
	observation, err := engine.Reconcile(t.Context(), workspace, "org-cluster")
	require.ErrorIs(t, err, ErrIdentityNotReady)
	tenant := observation.Tenant
	readyOrganizationTenant(tenant, class)
	require.NoError(t, engine.Organizations.Status().Update(t.Context(), tenant))
	_, err = engine.Reconcile(t.Context(), workspace, "org-cluster")
	require.NoError(t, err)
	originalClients := engine.Clients
	engine.Clients = func(name string) ([]pmidentityv1alpha1.OIDCClient, error) {
		clients, err := originalClients(name)
		if err != nil {
			return nil, err
		}
		clients[0].RedirectURIs = []string{"https://portal.example/changed-callback"}
		return clients, nil
	}
	observation, err = engine.Reconcile(t.Context(), workspace, "org-cluster")
	require.ErrorIs(t, err, ErrIdentityNotReady)
	require.Equal(t, int64(2), observation.Tenant.Generation)
	require.Nil(t, observation.Authenticator)
	require.Equal(t, []string{"https://portal.example/changed-callback"}, observation.Tenant.Spec.Clients[0].RedirectURIs)
	tenant = observation.Tenant
	tenant.Status.BoundProviderClassUID = "previous-class"
	require.NoError(t, engine.Organizations.Status().Update(t.Context(), tenant))
	_, err = engine.Reconcile(t.Context(), workspace, "org-cluster")
	require.ErrorContains(t, err, "requires coordinated migration")
}

func organizationFixture(t *testing.T) (*OrganizationReconciler, ctrlruntimeclient.Client, *pmidentityv1alpha1.IdentityProviderClass) {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		pmcorev1alpha1.AddToScheme, pmidentityv1alpha1.AddToScheme,
		kcpcorev1alpha1.AddToScheme, kcptenancyv1alpha1.AddToScheme,
	} {
		require.NoError(t, add(scheme))
	}
	location := pmcorev1alpha1.AccountLocation{
		Name: "organization", Type: pmcorev1alpha1.AccountTypeOrg,
		GeneratedClusterId: "org-cluster", OriginClusterId: "organizations-root",
	}
	info := &pmcorev1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account", UID: "accountinfo-uid", Annotations: map[string]string{logicalcluster.AnnotationKey: "org-cluster"}},
		Spec: pmcorev1alpha1.AccountInfoSpec{
			Account: location, Organization: location, OIDC: &pmcorev1alpha1.OIDCInfo{IssuerURL: "https://legacy.example"},
		},
	}
	lc := &kcpcorev1alpha1.LogicalCluster{ObjectMeta: metav1.ObjectMeta{
		Name: "cluster", UID: "logical-cluster-uid", Annotations: map[string]string{logicalcluster.AnnotationKey: "org-cluster"},
	}}
	account := &pmcorev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "organization", UID: "account-uid", Annotations: map[string]string{logicalcluster.AnnotationKey: "organizations-root"}},
		Spec:       pmcorev1alpha1.AccountSpec{Type: pmcorev1alpha1.AccountTypeOrg},
	}
	rootWorkspace := &kcptenancyv1alpha1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: account.Name, UID: "workspace-uid", Annotations: map[string]string{logicalcluster.AnnotationKey: "organizations-root"}},
		Spec:       kcptenancyv1alpha1.WorkspaceSpec{Cluster: "org-cluster"},
	}
	require.NoError(t, controllerutil.SetOwnerReference(account, rootWorkspace, scheme))
	class := &pmidentityv1alpha1.IdentityProviderClass{
		ObjectMeta: metav1.ObjectMeta{Name: "default", UID: "class-uid", Generation: 1},
		Spec: pmidentityv1alpha1.IdentityProviderClassSpec{
			ControllerName:       "independent-provider.example",
			ConfigurationRef:     pmidentityv1alpha1.IdentityProviderObjectReference{APIGroup: "independent-provider.example", Kind: "Configuration", Name: "default"},
			AuthenticationPolicy: pmidentityv1alpha1.AuthenticationPolicy{IssuerMode: pmidentityv1alpha1.IssuerModeDedicated},
		},
		Status: pmidentityv1alpha1.IdentityProviderClassStatus{
			ContractVersion: pmidentityv1alpha1.ContractVersion, ObservedGeneration: 1, ObservedConfigurationRevision: "current",
			Capabilities: []pmidentityv1alpha1.IdentityCapability{
				pmidentityv1alpha1.IdentityCapabilityOIDC, pmidentityv1alpha1.IdentityCapabilityDynamicClientRegistration,
				pmidentityv1alpha1.IdentityCapabilityClientManagement, pmidentityv1alpha1.IdentityCapabilityTenantProvisioning,
			},
			Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, ObservedGeneration: 1}},
		},
	}
	orgs := fake.NewClientBuilder().WithScheme(scheme).WithObjects(account, rootWorkspace).
		WithStatusSubresource(&pmidentityv1alpha1.IdentityTenant{}).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, client ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.CreateOption) error {
				obj.SetUID("tenant-uid")
				obj.SetGeneration(1)
				annotations := obj.GetAnnotations()
				annotations[logicalcluster.AnnotationKey] = "organizations-root"
				obj.SetAnnotations(annotations)
				return client.Create(ctx, obj, opts...)
			},
			Update: func(ctx context.Context, client ctrlruntimeclient.WithWatch, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.UpdateOption) error {
				if tenant, ok := obj.(*pmidentityv1alpha1.IdentityTenant); ok {
					before := &pmidentityv1alpha1.IdentityTenant{}
					if err := client.Get(ctx, ctrlruntimeclient.ObjectKeyFromObject(obj), before); err != nil {
						return err
					}
					if !equality.Semantic.DeepEqual(before.Spec, tenant.Spec) {
						tenant.Generation = before.Generation + 1
					}
				}
				return client.Update(ctx, obj, opts...)
			},
		}).Build()
	return &OrganizationReconciler{
		Organizations: orgs, Classes: fake.NewClientBuilder().WithScheme(scheme).WithObjects(class).Build(),
		OrganizationCluster: "organizations-root", ClassName: class.Name,
		Clients: func(string) ([]pmidentityv1alpha1.OIDCClient, error) {
			return []pmidentityv1alpha1.OIDCClient{{
				Name: "portal", Purpose: pmidentityv1alpha1.IdentityClientPurposePortal,
				RedirectURIs: []string{"https://portal.example/callback"}, GrantTypes: []string{"authorization_code"},
				ResponseTypes: []string{"code"}, Scopes: []string{"openid"}, TokenEndpointAuthMethod: pmidentityv1alpha1.ClientTokenEndpointAuthMethodNone,
			}}, nil
		},
	}, fake.NewClientBuilder().WithScheme(scheme).WithObjects(info, lc).Build(), class
}

func readyOrganizationTenant(tenant *pmidentityv1alpha1.IdentityTenant, class *pmidentityv1alpha1.IdentityProviderClass) {
	ready := []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, ObservedGeneration: tenant.Generation}}
	tenant.Status = pmidentityv1alpha1.IdentityTenantStatus{
		ObservedGeneration: tenant.Generation, BoundProviderClassUID: class.UID, ControllerName: class.Spec.ControllerName,
		ObservedConfigurationRevision: class.Status.ObservedConfigurationRevision, ExternalID: "external-organization",
		IssuerURL: "https://independent.example/actual/issuer/", SubjectMode: pmidentityv1alpha1.SubjectModePublic,
		Conditions: ready,
		Clients: []pmidentityv1alpha1.ManagedClient{{
			Name: "portal", ClientID: "assigned-portal", ObservedGeneration: tenant.Generation, Conditions: ready,
		}},
	}
}
