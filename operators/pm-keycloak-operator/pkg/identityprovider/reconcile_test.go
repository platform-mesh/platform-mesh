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

package identityprovider

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/clientreg"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/directory"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kcp-dev/logicalcluster/v3"
)

type testBackend struct {
	registered, updated, deleted, realmDeleted int
	registerError, deleteError                 error
	clients                                    map[string]clientreg.ClientInformation
}

func (b *testBackend) EnsureTenant(context.Context, string, string, *bool, bool) (TenantObservation, error) {
	return TenantObservation{Issuer: "https://idp.example/realms/org-a", RegistrationEndpoint: "https://idp.example/register"}, nil
}
func (b *testBackend) DeleteTenant(context.Context, string, string) error {
	b.realmDeleted++
	return nil
}
func (b *testBackend) Register(_ context.Context, _ string, metadata clientreg.ClientMetadata) (clientreg.ClientInformation, error) {
	b.registered++
	info := clientreg.ClientInformation{
		ClientID: "assigned-id", ClientSecret: "private-client-secret", RegistrationAccessToken: "private-registration-token",
		RegistrationClientURI: "https://idp.example/registration/assigned-id", ClientMetadata: metadata,
	}
	b.clients[info.ClientID] = info
	return info, b.registerError
}
func (b *testBackend) Read(_ context.Context, _ string, info clientreg.ClientInformation) (clientreg.ClientInformation, error) {
	found, ok := b.clients[info.ClientID]
	if !ok {
		return clientreg.ClientInformation{}, clientreg.NewHTTPError(404, "", clientreg.OperationRead)
	}
	return found, nil
}
func (b *testBackend) Update(_ context.Context, _ string, info clientreg.ClientInformation, metadata clientreg.ClientMetadata) (clientreg.ClientInformation, error) {
	b.updated++
	info.ClientMetadata = metadata
	info.RegistrationAccessToken = "rotated-management-token"
	b.clients[info.ClientID] = info
	return info, nil
}
func (b *testBackend) DeleteClient(_ context.Context, _ string, info clientreg.ClientInformation) error {
	if b.deleteError != nil {
		return b.deleteError
	}
	b.deleted++
	delete(b.clients, info.ClientID)
	return nil
}
func (b *testBackend) ExternalClientID(context.Context, string, string) (string, error) {
	return "stable-native-uuid", nil
}
func (b *testBackend) RecoverManagementToken(context.Context, string, string, string) (string, error) {
	return "recovered-token", nil
}
func (b *testBackend) MatchesMetadata(_ context.Context, _ string, info clientreg.ClientInformation, wanted clientreg.ClientMetadata) (bool, error) {
	return sameMetadata(wanted, info.ClientMetadata), nil
}

func TestDefiniteRegistrationRejectionCanRetry(t *testing.T) {
	for _, rejection := range []error{
		clientreg.ErrRegistrationNotAttempted,
		clientreg.NewHTTPError(403, "", clientreg.OperationRegister),
	} {
		r, class, tenant, backend := providerFixture(t)
		backend.registerError = rejection
		require.Error(t, r.Reconcile(t.Context(), class, tenant))
		_, journal, err := r.load(t.Context(), tenant)
		require.NoError(t, err)
		require.Empty(t, journal.Clients)
		backend.registerError = nil
		require.NoError(t, r.Reconcile(t.Context(), class, tenant))
		require.Equal(t, 2, backend.registered)
	}
}

func TestTenantClientLifecycleAndCredentialSeparation(t *testing.T) {
	r, class, tenant, backend := providerFixture(t)
	require.NoError(t, r.Reconcile(t.Context(), class, tenant))
	require.True(t, meta.IsStatusConditionTrue(tenant.Status.Conditions, "Ready"))
	require.Equal(t, "assigned-id", tenant.Status.Clients[0].ClientID)
	output := &corev1.Secret{}
	require.NoError(t, r.Tenants.Get(t.Context(), ctrlruntimeclient.ObjectKey{Namespace: "default", Name: "portal-credentials"}, output))
	require.Equal(t, "private-client-secret", string(output.Data["client_secret"]))
	require.NotContains(t, output.Data, "registration_access_token")
	_, journal, err := r.load(t.Context(), tenant)
	require.NoError(t, err)
	require.Equal(t, "private-registration-token", journal.Clients["portal"].Info.RegistrationAccessToken)
	require.Equal(t, 1, backend.registered)

	tenant.Spec.Clients[0].DisplayName = "Changed display"
	tenant.Generation++
	require.NoError(t, r.Tenants.Update(t.Context(), tenant))
	require.NoError(t, r.Reconcile(t.Context(), class, tenant))
	require.Equal(t, 1, backend.updated)
	require.Equal(t, 1, backend.registered)
	_, journal, err = r.load(t.Context(), tenant)
	require.NoError(t, err)
	require.Equal(t, "rotated-management-token", journal.Clients["portal"].Info.RegistrationAccessToken)

	tenant.Spec.Clients = nil
	tenant.Generation++
	require.NoError(t, r.Tenants.Update(t.Context(), tenant))
	backend.deleteError = errors.New("provider temporarily unavailable")
	require.Error(t, r.Reconcile(t.Context(), class, tenant))
	require.True(t, tenant.Status.Clients[0].DeletionPending)
	_, journal, err = r.load(t.Context(), tenant)
	require.NoError(t, err)
	require.Contains(t, journal.Clients, "portal")

	backend.deleteError = nil
	restarted := *r
	require.NoError(t, restarted.Reconcile(t.Context(), class, tenant))
	require.Equal(t, 1, backend.deleted)
	require.Empty(t, tenant.Status.Clients)
	require.True(t, apierrors.IsNotFound(r.Tenants.Get(t.Context(), ctrlruntimeclient.ObjectKeyFromObject(output), output)))
}

func TestAmbiguousRegistrationIsNeverReplayed(t *testing.T) {
	r, class, tenant, backend := providerFixture(t)
	backend.registerError = errors.New("response lost after registration")
	require.Error(t, r.Reconcile(t.Context(), class, tenant))
	backend.registerError = nil
	require.ErrorContains(t, r.Reconcile(t.Context(), class, tenant), "ambiguous")
	require.Equal(t, 1, backend.registered)
	require.False(t, meta.IsStatusConditionTrue(tenant.Status.Conditions, "Ready"))

	// Deleting the owned tenant can safely clean the entire realm even when
	// a client's registration response could not be recovered.
	require.NoError(t, r.Tenants.Delete(t.Context(), tenant))
	require.NoError(t, r.Tenants.Get(t.Context(), ctrlruntimeclient.ObjectKeyFromObject(tenant), tenant))
	require.NoError(t, r.Reconcile(t.Context(), class, tenant))
	require.Equal(t, 1, backend.realmDeleted)
}

func TestMissingInventoryDoesNotRecreateRegisteredClients(t *testing.T) {
	r, class, tenant, backend := providerFixture(t)
	require.NoError(t, r.Reconcile(t.Context(), class, tenant))
	secret, _, err := r.load(t.Context(), tenant)
	require.NoError(t, err)
	require.NoError(t, r.Private.Delete(t.Context(), secret))
	require.ErrorContains(t, r.Reconcile(t.Context(), class, tenant), "inventory is missing")
	require.Equal(t, 1, backend.registered)
}

func TestChangedProviderIdentityAndForeignOutputsAreRejected(t *testing.T) {
	r, class, tenant, backend := providerFixture(t)
	require.NoError(t, r.Reconcile(t.Context(), class, tenant))
	r.ProviderIdentity = "another-provider"
	require.ErrorContains(t, r.Reconcile(t.Context(), class, tenant), "binding changed")
	require.Equal(t, 1, backend.registered)

	r, class, tenant, _ = providerFixture(t)
	foreign := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "portal-credentials", Namespace: "default"},
		Data:       map[string][]byte{"untouched": []byte("foreign-data")},
	}
	require.NoError(t, r.Tenants.Create(t.Context(), foreign))
	require.ErrorContains(t, r.Reconcile(t.Context(), class, tenant), "owned by another")
	require.NoError(t, r.Tenants.Get(t.Context(), ctrlruntimeclient.ObjectKeyFromObject(foreign), foreign))
	require.Equal(t, "foreign-data", string(foreign.Data["untouched"]))
}

type failFinalizerRemoval struct {
	ctrlruntimeclient.Client
	fail bool
}

func (c *failFinalizerRemoval) Update(ctx context.Context, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.UpdateOption) error {
	if c.fail && !obj.GetDeletionTimestamp().IsZero() && !controllerutil.ContainsFinalizer(obj, Finalizer) {
		return errors.New("interrupted finalizer update")
	}
	return c.Client.Update(ctx, obj, opts...)
}

func TestFinalizationRecoversAfterPrivateInventoryDeletion(t *testing.T) {
	r, class, tenant, _ := providerFixture(t)
	require.NoError(t, r.Reconcile(t.Context(), class, tenant))
	require.NoError(t, r.Tenants.Delete(t.Context(), tenant))
	require.NoError(t, r.Tenants.Get(t.Context(), ctrlruntimeclient.ObjectKeyFromObject(tenant), tenant))
	wrapper := &failFinalizerRemoval{Client: r.Tenants, fail: true}
	r.Tenants = wrapper
	require.ErrorContains(t, r.Reconcile(t.Context(), class, tenant), "interrupted finalizer")
	require.NoError(t, r.Tenants.Get(t.Context(), ctrlruntimeclient.ObjectKeyFromObject(tenant), tenant))
	wrapper.fail = false
	require.NoError(t, r.Reconcile(t.Context(), class, tenant))
	require.True(t, apierrors.IsNotFound(r.Tenants.Get(t.Context(), ctrlruntimeclient.ObjectKeyFromObject(tenant), tenant)))
}

func TestRetainPreservesRemoteResourcesAndRecoverableManagementState(t *testing.T) {
	r, class, tenant, backend := providerFixture(t)
	tenant.Spec.DeletionPolicy = pmidentityv1alpha1.DeletionPolicyRetain
	require.NoError(t, r.Tenants.Update(t.Context(), tenant))
	require.NoError(t, r.Reconcile(t.Context(), class, tenant))
	require.NoError(t, r.Tenants.Delete(t.Context(), tenant))
	require.NoError(t, r.Tenants.Get(t.Context(), ctrlruntimeclient.ObjectKeyFromObject(tenant), tenant))
	require.NoError(t, r.Reconcile(t.Context(), class, tenant))
	require.Equal(t, 0, backend.realmDeleted)
	_, journal, err := r.load(t.Context(), tenant)
	require.NoError(t, err)
	require.True(t, journal.Retained)
	output := &corev1.Secret{}
	require.NoError(t, r.Tenants.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "portal-credentials", Namespace: "default"}, output))
	require.Nil(t, metav1.GetControllerOf(output))
}

func providerFixture(t *testing.T) (*Reconciler, *pmidentityv1alpha1.IdentityProviderClass, *pmidentityv1alpha1.IdentityTenant, *testBackend) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, pmidentityv1alpha1.AddToScheme(scheme))
	class := &pmidentityv1alpha1.IdentityProviderClass{
		ObjectMeta: metav1.ObjectMeta{Name: "default", UID: "class-uid", Generation: 1},
		Spec: pmidentityv1alpha1.IdentityProviderClassSpec{
			ControllerName: directory.KeycloakControllerName,
			ConfigurationRef: pmidentityv1alpha1.IdentityProviderObjectReference{
				APIGroup: "keycloak.platform-mesh.io", Kind: "KeycloakProviderConfiguration", Name: "default",
			},
			AuthenticationPolicy: pmidentityv1alpha1.AuthenticationPolicy{IssuerMode: pmidentityv1alpha1.IssuerModeDedicated},
		},
		Status: pmidentityv1alpha1.IdentityProviderClassStatus{
			ContractVersion: pmidentityv1alpha1.ContractVersion, ObservedGeneration: 1,
			ObservedConfigurationRevision: "config-v1",
			Conditions:                    []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, ObservedGeneration: 1}},
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
				Name: "portal", Purpose: pmidentityv1alpha1.IdentityClientPurposePortal, DisplayName: "Portal",
				GrantTypes: []string{"authorization_code"}, ResponseTypes: []string{"code"}, Scopes: []string{"openid"},
				RedirectURIs:            []string{"https://portal.example/callback"},
				TokenEndpointAuthMethod: pmidentityv1alpha1.ClientTokenEndpointAuthMethodClientSecretBasic,
				CredentialsSecretRef:    &corev1.LocalObjectReference{Name: "portal-credentials"},
			}},
		},
	}
	tenantClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tenant).
		WithStatusSubresource(&pmidentityv1alpha1.IdentityTenant{}).Build()
	privateClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	backend := &testBackend{clients: map[string]clientreg.ClientInformation{}}
	return &Reconciler{
		Tenants: tenantClient, Private: privateClient, Cluster: "tenant-cluster",
		RuntimeNamespace: "provider", OutputNamespace: "default", ProviderIdentity: "provider-v1", Backend: backend,
	}, class, tenant, backend
}
