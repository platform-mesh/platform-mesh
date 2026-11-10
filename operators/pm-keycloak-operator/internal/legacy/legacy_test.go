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

package legacy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmkeycloakv1alpha1 "go.platform-mesh.io/apis/keycloak/v1alpha1"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// recorder is a Processor+Finalizer that records what it was handed and mutates status.
type recorder struct {
	seen ctrlruntimeclient.Object
}

func (r *recorder) GetName() string { return "recorder" }

func (r *recorder) Process(_ context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	r.seen = obj
	ipc := obj.(*pmkeycloakv1alpha1.IdentityProviderConfiguration)
	ipc.Status.ManagedClients = map[string]pmkeycloakv1alpha1.ManagedClient{
		"portal": {ClientID: "id-1", RegistrationClientURI: "https://kc/reg/id-1", SecretRef: corev1.SecretReference{Name: "s", Namespace: "default"}},
	}
	ipc.Labels = map[string]string{"touched": "yes"}
	return subroutines.OK(), nil
}

func (r *recorder) Finalize(_ context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	r.seen = obj
	return subroutines.OK(), nil
}

func (r *recorder) Finalizers(_ ctrlruntimeclient.Object) []string {
	return []string{"keycloak.platform-mesh.io/idp-finalizer"}
}

//nolint:staticcheck // legacy kind under test
func legacyIPC() *pmcorev1alpha1.IdentityProviderConfiguration {
	return &pmcorev1alpha1.IdentityProviderConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "acme", Finalizers: []string{"core.platform-mesh.io/idp-finalizer"}},
		Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
			RegistrationAllowed: true,
			Clients: []pmcorev1alpha1.IdentityProviderClientConfig{{
				ClientType:             pmcorev1alpha1.IdentityProviderClientTypeConfidential,
				ClientName:             "portal",
				RedirectURIs:           []string{"https://portal/*"},
				PostLogoutRedirectURIs: []string{"https://portal/logout"},
				SecretRef:              corev1.SecretReference{Name: "s", Namespace: "default"},
			}},
		},
		Status: pmcorev1alpha1.IdentityProviderConfigurationStatus{
			Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse}},
		},
	}
}

func TestSubroutine_Process(t *testing.T) {
	t.Parallel()

	t.Run("converts the legacy IdentityProviderConfiguration and writes status back", func(t *testing.T) {
		t.Parallel()
		inner := &recorder{}
		sub := IdentityProviderConfiguration(inner)
		obj := legacyIPC()

		res, err := sub.Process(t.Context(), obj)

		require.NoError(t, err)
		assert.True(t, res.IsContinue())
		seen, ok := inner.seen.(*pmkeycloakv1alpha1.IdentityProviderConfiguration)
		require.True(t, ok, "inner subroutine must see the keycloak kind")
		assert.Equal(t, "keycloak.platform-mesh.io/v1alpha1", seen.APIVersion)
		assert.Equal(t, "acme", seen.Name)
		assert.True(t, seen.Spec.RegistrationAllowed)
		require.Len(t, seen.Spec.Clients, 1)
		assert.Equal(t, pmkeycloakv1alpha1.IdentityProviderClientTypeConfidential, seen.Spec.Clients[0].ClientType)
		assert.Equal(t, []string{"https://portal/*"}, seen.Spec.Clients[0].RedirectURIs)
		assert.Equal(t, []string{"https://portal/logout"}, seen.Spec.Clients[0].PostLogoutRedirectURIs)
		assert.Equal(t, "id-1", obj.Status.ManagedClients["portal"].ClientID, "status written back to the legacy object")
		assert.Equal(t, "https://kc/reg/id-1", obj.Status.ManagedClients["portal"].RegistrationClientURI)
		assert.Equal(t, "yes", obj.Labels["touched"], "metadata written back to the legacy object")
		assert.Len(t, obj.Status.Conditions, 1, "existing conditions kept")
	})

	t.Run("rejects a non-legacy object", func(t *testing.T) {
		t.Parallel()
		sub := IdentityProviderConfiguration(&recorder{})

		_, err := sub.Process(t.Context(), &pmkeycloakv1alpha1.IdentityProviderConfiguration{})

		require.Error(t, err)
	})

	t.Run("converts the legacy Invite", func(t *testing.T) {
		t.Parallel()
		var seen ctrlruntimeclient.Object
		sub := Invite(processorFunc(func(obj ctrlruntimeclient.Object) {
			seen = obj
			obj.(*pmkeycloakv1alpha1.Invite).Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}
		}))
		//nolint:staticcheck // legacy kind under test
		obj := &pmcorev1alpha1.Invite{ObjectMeta: metav1.ObjectMeta{Name: "inv"}, Spec: pmcorev1alpha1.InviteSpec{Email: "a@b.c"}}

		_, err := sub.Process(t.Context(), obj)

		require.NoError(t, err)
		invite, ok := seen.(*pmkeycloakv1alpha1.Invite)
		require.True(t, ok)
		assert.Equal(t, "a@b.c", invite.Spec.Email)
		assert.Equal(t, metav1.ConditionTrue, obj.Status.Conditions[0].Status)
	})
}

func TestSubroutine_Finalizers(t *testing.T) {
	t.Parallel()

	sub := IdentityProviderConfiguration(&recorder{})

	assert.Equal(t,
		[]string{"keycloak.platform-mesh.io/idp-finalizer", "core.platform-mesh.io/idp-finalizer"},
		sub.Finalizers(legacyIPC()),
		"legacy finalizer released together with the current one")
	assert.Nil(t, Invite(processorFunc(func(ctrlruntimeclient.Object) {})).Finalizers(nil), "inner without Finalizer adds none")
}

// processorFunc is a Processor-only subroutine.
type processorFunc func(obj ctrlruntimeclient.Object)

func (f processorFunc) GetName() string { return "func" }

func (f processorFunc) Process(_ context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	f(obj)
	return subroutines.OK(), nil
}
