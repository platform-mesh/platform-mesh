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

package admission

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	webhookadmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type testDispatcher struct {
	calls int
	err   error
}

func (d *testDispatcher) Validate(_ context.Context, _ *pmidentityv1alpha1.IdentityProviderClass, _ webhookadmission.Request) (webhookadmission.Response, error) {
	d.calls++
	return webhookadmission.Denied("provider-specific restriction"), d.err
}

func admissionFixture(t *testing.T) (*Handler, *testDispatcher, *pmidentityv1alpha1.IdentityTenant) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, pmidentityv1alpha1.AddToScheme(scheme))
	class := &pmidentityv1alpha1.IdentityProviderClass{
		ObjectMeta: metav1.ObjectMeta{Name: "default", UID: "class-uid", Generation: 1},
		Spec: pmidentityv1alpha1.IdentityProviderClassSpec{
			ControllerName: "provider.example",
			ConfigurationRef: pmidentityv1alpha1.IdentityProviderObjectReference{
				APIGroup: "provider.example", Kind: "ProviderConfiguration", Name: "default",
			},
			AuthenticationPolicy: pmidentityv1alpha1.AuthenticationPolicy{IssuerMode: pmidentityv1alpha1.IssuerModeDedicated},
			Validation:           &pmidentityv1alpha1.IdentityEndpointConfig{URL: "https://validator.example/validate"},
		},
	}
	dispatch := &testDispatcher{}
	return &Handler{
		Classes:     fake.NewClientBuilder().WithScheme(scheme).WithObjects(class).Build(),
		ActiveClass: "default", Dispatcher: dispatch,
	}, dispatch, &pmidentityv1alpha1.IdentityTenant{
		ObjectMeta: metav1.ObjectMeta{Name: "org-a"},
		Spec: pmidentityv1alpha1.IdentityTenantSpec{
			ProviderClassRef: "default", Purpose: pmidentityv1alpha1.IdentityTenantPurposeOrganization, Organization: "org-a",
			DeletionPolicy: pmidentityv1alpha1.DeletionPolicyDelete,
			Clients: []pmidentityv1alpha1.OIDCClient{{
				Name: "cli", Purpose: pmidentityv1alpha1.IdentityClientPurposeCLI,
				RedirectURIs: []string{"https://app.example/callback"},
				GrantTypes:   []string{"authorization_code"}, ResponseTypes: []string{"code"}, Scopes: []string{"openid"},
				TokenEndpointAuthMethod: pmidentityv1alpha1.ClientTokenEndpointAuthMethodNone,
			}},
		},
	}
}

func reviewRequest(t *testing.T, kind string, operation admissionv1.Operation, object, old any) webhookadmission.Request {
	t.Helper()
	raw, err := json.Marshal(object)
	require.NoError(t, err)
	previous, err := json.Marshal(old)
	require.NoError(t, err)
	resources := map[string]string{
		"IdentityTenant": "identitytenants", "IdentityProviderClass": "identityproviderclasses", "Invitation": "invitations",
	}
	return webhookadmission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		UID: "request-uid", Operation: operation,
		Kind:     metav1.GroupVersionKind{Group: pmidentityv1alpha1.GroupName, Version: "v1alpha1", Kind: kind},
		Resource: metav1.GroupVersionResource{Group: pmidentityv1alpha1.GroupName, Version: "v1alpha1", Resource: resources[kind]},
		Object:   runtime.RawExtension{Raw: raw}, OldObject: runtime.RawExtension{Raw: previous},
	}}
}

func TestSharedValidationRunsBeforeProviderDispatch(t *testing.T) {
	h, dispatch, tenant := admissionFixture(t)
	response := h.Handle(t.Context(), reviewRequest(t, "IdentityTenant", admissionv1.Create, tenant, nil))
	require.False(t, response.Allowed)
	require.Equal(t, "provider-specific restriction", response.Result.Message)
	require.Equal(t, 1, dispatch.calls)
	tenant.Spec.Clients[0].RedirectURIs = []string{"https://app.example/*"}
	response = h.Handle(t.Context(), reviewRequest(t, "IdentityTenant", admissionv1.Create, tenant, nil))
	require.False(t, response.Allowed)
	require.Equal(t, 1, dispatch.calls)
}

func TestProviderOutageDoesNotBlockLifecycleOperations(t *testing.T) {
	h, dispatch, tenant := admissionFixture(t)
	h.Classes = nil
	dispatch.err = errors.New("provider unavailable")
	updated := tenant.DeepCopy()
	updated.Finalizers = []string{"identity.example/cleanup"}
	require.True(t, h.Handle(t.Context(), reviewRequest(t, "IdentityTenant", admissionv1.Update, updated, tenant)).Allowed)
	updated.Status.ExternalID = "provider-reference"
	status := reviewRequest(t, "IdentityTenant", admissionv1.Update, updated, tenant)
	status.SubResource = "status"
	require.True(t, h.Handle(t.Context(), status).Allowed)
	require.True(t, h.Handle(t.Context(), reviewRequest(t, "IdentityTenant", admissionv1.Delete, nil, tenant)).Allowed)
	updated.Spec.Clients[0].RedirectURIs = []string{"https://app.example/new-callback"}
	require.False(t, h.Handle(t.Context(), reviewRequest(t, "IdentityTenant", admissionv1.Update, updated, tenant)).Allowed)
	require.Zero(t, dispatch.calls)
}

func TestAdministrativeIdentityOperationsRequireExplicitApproval(t *testing.T) {
	for _, field := range []string{"adoption", "retain"} {
		t.Run(field, func(t *testing.T) {
			h, dispatch, tenant := admissionFixture(t)
			if field == "adoption" {
				tenant.Spec.AdoptionRef = "migration-record"
			} else {
				tenant.Spec.DeletionPolicy = pmidentityv1alpha1.DeletionPolicyRetain
			}
			require.False(t, h.Handle(t.Context(), reviewRequest(t, "IdentityTenant", admissionv1.Create, tenant, nil)).Allowed)
			require.Zero(t, dispatch.calls)
			h.Privileged = func(context.Context, authenticationv1.UserInfo) (bool, error) { return true, nil }
			response := h.Handle(t.Context(), reviewRequest(t, "IdentityTenant", admissionv1.Create, tenant, nil))
			require.False(t, response.Allowed)
			require.Equal(t, 1, dispatch.calls)
		})
	}
}

func TestIdentityBindingAndInvitationCannotBeRetargeted(t *testing.T) {
	h, dispatch, tenant := admissionFixture(t)
	updated := tenant.DeepCopy()
	updated.Spec.ProviderClassRef = "another"
	require.False(t, h.Handle(t.Context(), reviewRequest(t, "IdentityTenant", admissionv1.Update, updated, tenant)).Allowed)
	require.Zero(t, dispatch.calls)
	invitation := &pmidentityv1alpha1.Invitation{
		ObjectMeta: metav1.ObjectMeta{Name: "invite"},
		Spec:       pmidentityv1alpha1.InvitationSpec{Email: "person@example.org", TenantRef: "org-a", ClientName: "portal"},
	}
	require.False(t, h.Handle(t.Context(), reviewRequest(t, "Invitation", admissionv1.Create, invitation, nil)).Allowed)
	changed := invitation.DeepCopy()
	changed.Spec.Email = "another@example.org"
	require.False(t, h.Handle(t.Context(), reviewRequest(t, "Invitation", admissionv1.Update, changed, invitation)).Allowed)
	require.True(t, h.Handle(t.Context(), reviewRequest(t, "Invitation", admissionv1.Update, invitation, invitation)).Allowed)
}
