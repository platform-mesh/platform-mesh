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
	"time"

	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/apis/identity/projection"
	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kcp-dev/logicalcluster/v3"
)

type testInvitationBackend struct {
	created, ensured, dispatched int
	dispatchFailure              error
	afterEnsure                  func()
}

func (b *testInvitationBackend) EnsureInvitationUser(_ context.Context, _, _, _, existing string) (string, error) {
	b.ensured++
	if existing == "" {
		b.created++
	}
	if b.afterEnsure != nil {
		b.afterEnsure()
	}
	return "invited-subject", nil
}

func (b *testInvitationBackend) DispatchInvitation(context.Context, string, string, string, string, string, int) error {
	b.dispatched++
	return b.dispatchFailure
}

func TestInvitationReconcilePersistsProvisioningAndRetriesDeliveryWithoutAcceptance(t *testing.T) {
	engine, class, invitation, backend := invitationFixture(t)
	backend.dispatchFailure = errors.New("SMTP unavailable")
	require.ErrorContains(t, engine.Reconcile(t.Context(), class, invitation), "SMTP unavailable")
	require.Equal(t, pmidentityv1alpha1.InvitationPhaseFailed, invitation.Status.Phase)
	require.True(t, meta.IsStatusConditionTrue(invitation.Status.Conditions, "UserProvisioned"))
	require.False(t, meta.IsStatusConditionTrue(invitation.Status.Conditions, "IdentityVerified"))
	require.Equal(t, "invited-subject", invitation.Status.Principal.Subject)
	backend.dispatchFailure = nil
	restarted := *engine
	require.NoError(t, restarted.Reconcile(t.Context(), class, invitation))
	require.Equal(t, pmidentityv1alpha1.InvitationPhaseDispatched, invitation.Status.Phase)
	require.False(t, meta.IsStatusConditionTrue(invitation.Status.Conditions, "IdentityVerified"))
	require.Equal(t, 1, backend.created)
	require.Equal(t, 2, backend.dispatched)
	rv := invitation.ResourceVersion
	require.NoError(t, restarted.Reconcile(t.Context(), class, invitation))
	require.Equal(t, rv, invitation.ResourceVersion)
	require.Equal(t, 2, backend.dispatched)

	engine.Now = func() time.Time { return invitation.CreationTimestamp.Add(25 * time.Hour) }
	require.NoError(t, engine.Reconcile(t.Context(), class, invitation))
	require.Equal(t, pmidentityv1alpha1.InvitationPhaseExpired, invitation.Status.Phase)
	require.Equal(t, 2, backend.dispatched)
}

func TestInvitationDeletionDoesNotRequireProviderAndNeverDeletesAUser(t *testing.T) {
	engine, class, invitation, backend := invitationFixture(t)
	require.NoError(t, engine.Reconcile(t.Context(), class, invitation))
	require.NoError(t, engine.Requests.Delete(t.Context(), invitation))
	require.NoError(t, engine.Requests.Get(t.Context(), ctrlruntimeclient.ObjectKeyFromObject(invitation), invitation))
	engine.Backend, engine.Tenants = nil, nil
	require.NoError(t, engine.Reconcile(t.Context(), nil, invitation))
	require.Equal(t, 1, backend.created)
	require.Equal(t, 1, backend.dispatched)
	secrets := &corev1.SecretList{}
	require.NoError(t, engine.Private.List(t.Context(), secrets))
	require.Len(t, secrets.Items, 1) // The unrelated tenant inventory remains.
	require.True(t, apierrors.IsNotFound(engine.Requests.Get(t.Context(), ctrlruntimeclient.ObjectKeyFromObject(invitation), invitation)))
}

func TestInvitationMissingInventoryDoesNotRepeatMail(t *testing.T) {
	engine, class, invitation, backend := invitationFixture(t)
	require.NoError(t, engine.Reconcile(t.Context(), class, invitation))
	owner := "invitation/" + engine.Cluster.String() + "/" + string(invitation.UID)
	secret := &corev1.Secret{}
	require.NoError(t, engine.Private.Get(t.Context(), ctrlruntimeclient.ObjectKey{Namespace: engine.RuntimeNamespace, Name: privateName(owner)}, secret))
	require.NoError(t, engine.Private.Delete(t.Context(), secret))
	require.ErrorContains(t, engine.Reconcile(t.Context(), class, invitation), "refusing to repeat delivery")
	require.Equal(t, 1, backend.dispatched)
}

func TestInvitationRejectsRetargetingAndExpiredDelivery(t *testing.T) {
	for _, edit := range []func(*pmcorev1alpha1.AccountInfo){
		func(info *pmcorev1alpha1.AccountInfo) { info.Spec.OIDC.Identity.Tenant.UID = "replacement-tenant" },
		func(info *pmcorev1alpha1.AccountInfo) {
			info.Spec.OIDC.Identity.Tenant.LogicalCluster = "foreign-cluster"
		},
		func(info *pmcorev1alpha1.AccountInfo) { info.Spec.OIDC.IssuerURL = "https://foreign.example" },
		func(info *pmcorev1alpha1.AccountInfo) { info.Spec.Organization.OriginClusterId = "foreign-cluster" },
	} {
		engine, class, invitation, backend := invitationFixture(t)
		info := &pmcorev1alpha1.AccountInfo{}
		require.NoError(t, engine.Requests.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "account"}, info))
		edit(info)
		require.NoError(t, engine.Requests.Update(t.Context(), info))
		require.Error(t, engine.Reconcile(t.Context(), class, invitation))
		require.Zero(t, backend.created)
		require.Zero(t, backend.dispatched)
	}
	engine, class, invitation, backend := invitationFixture(t)
	backend.afterEnsure = func() {
		engine.Now = func() time.Time { return invitation.CreationTimestamp.Add(25 * time.Hour) }
	}
	require.NoError(t, engine.Reconcile(t.Context(), class, invitation))
	require.Equal(t, pmidentityv1alpha1.InvitationPhaseExpired, invitation.Status.Phase)
	require.Zero(t, backend.dispatched)
}

func invitationFixture(t *testing.T) (*InvitationReconciler, *pmidentityv1alpha1.IdentityProviderClass, *pmidentityv1alpha1.Invitation, *testInvitationBackend) {
	t.Helper()
	provider, class, tenant, _ := providerFixture(t)
	class.Status.Capabilities = []pmidentityv1alpha1.IdentityCapability{
		pmidentityv1alpha1.IdentityCapabilityOIDC, pmidentityv1alpha1.IdentityCapabilityDynamicClientRegistration,
		pmidentityv1alpha1.IdentityCapabilityClientManagement, pmidentityv1alpha1.IdentityCapabilityTenantProvisioning,
		pmidentityv1alpha1.IdentityCapabilityInvitations,
	}
	require.NoError(t, provider.Reconcile(t.Context(), class, tenant))
	require.NoError(t, pmcorev1alpha1.AddToScheme(provider.Tenants.Scheme()))
	oidc, err := projection.AccountInfo(class, tenant)
	require.NoError(t, err)
	info := &pmcorev1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account", Annotations: map[string]string{logicalcluster.AnnotationKey: "organization-cluster"}},
		Spec: pmcorev1alpha1.AccountInfoSpec{
			OIDC:    oidc,
			Account: pmcorev1alpha1.AccountLocation{GeneratedClusterId: "organization-cluster"},
			Organization: pmcorev1alpha1.AccountLocation{
				Name: tenant.Spec.Organization, OriginClusterId: provider.Cluster.String(), Type: pmcorev1alpha1.AccountTypeOrg,
			},
		},
	}
	invitation := &pmidentityv1alpha1.Invitation{
		ObjectMeta: metav1.ObjectMeta{
			Name: "invitation", UID: "invitation-uid", Generation: 1, CreationTimestamp: metav1.Now(),
			Annotations: map[string]string{logicalcluster.AnnotationKey: "organization-cluster"},
		},
		Spec: pmidentityv1alpha1.InvitationSpec{Email: "person@example.org", TenantRef: tenant.Name, ClientName: "portal"},
	}
	requests := fake.NewClientBuilder().WithScheme(provider.Tenants.Scheme()).WithObjects(info, invitation).
		WithStatusSubresource(&pmidentityv1alpha1.Invitation{}).Build()
	backend := &testInvitationBackend{}
	return &InvitationReconciler{
		Requests: requests, Tenants: provider.Tenants, Private: provider.Private,
		Cluster: "organization-cluster", TenantCluster: provider.Cluster, Backend: backend,
		RuntimeNamespace: provider.RuntimeNamespace, ProviderIdentity: provider.ProviderIdentity,
	}, class, invitation, backend
}
