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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmkeycloakv1alpha1 "go.platform-mesh.io/apis/keycloak/v1alpha1"
	"go.platform-mesh.io/pm-keycloak-operator/internal/subroutine/mocks"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func legacyClient(objs ...ctrlruntimeclient.Object) ctrlruntimeclient.Client {
	scheme := runtime.NewScheme()
	utilruntime.Must(pmcorev1alpha1.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).
		//nolint:staticcheck // legacy kind under test
		WithStatusSubresource(&pmcorev1alpha1.IdentityProviderConfiguration{}).
		WithObjects(objs...).Build()
}

func TestIdentityProviderConfigurationAdopter(t *testing.T) {
	t.Parallel()

	t.Run("marks the legacy object and returns its managed clients", func(t *testing.T) {
		t.Parallel()
		legacy := legacyIPC()
		legacy.Status.ManagedClients = map[string]pmcorev1alpha1.ManagedClient{"portal": {ClientID: "id-1"}}
		cl := legacyClient(legacy)
		getter := mocks.NewMockKCPClientGetter(t)
		getter.EXPECT().NewClientForLogicalCluster(mock.Anything, "system#abc").Return(cl, nil)

		adopted, err := IdentityProviderConfigurationAdopter(getter)(t.Context(), "keycloak#abc", "acme")

		require.NoError(t, err)
		assert.Equal(t, "id-1", adopted["portal"].ClientID)
		//nolint:staticcheck // legacy kind under test
		var marked pmcorev1alpha1.IdentityProviderConfiguration
		require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: "acme"}, &marked))
		assert.True(t, Migrated(&marked), "legacy object handed over, its reconciler stops managing the realm")
	})

	t.Run("nothing to adopt", func(t *testing.T) {
		t.Parallel()
		getter := mocks.NewMockKCPClientGetter(t)
		getter.EXPECT().NewClientForLogicalCluster(mock.Anything, "system#abc").Return(legacyClient(), nil)

		adopted, err := IdentityProviderConfigurationAdopter(getter)(t.Context(), "keycloak#abc", "acme")

		require.NoError(t, err)
		assert.Nil(t, adopted)
	})
}

func TestIdentityProviderConfigurationExists(t *testing.T) {
	t.Parallel()

	getter := mocks.NewMockKCPClientGetter(t)
	getter.EXPECT().NewClientForLogicalCluster(mock.Anything, "abc").Return(legacyClient(legacyIPC()), nil)
	exists := IdentityProviderConfigurationExists(getter)

	found, err := exists(t.Context(), "abc", "acme")
	require.NoError(t, err)
	assert.True(t, found)

	found, err = exists(t.Context(), "abc", "other")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestIdentityProviderConfigurationMirror(t *testing.T) {
	t.Parallel()
	status := pmkeycloakv1alpha1.IdentityProviderConfigurationStatus{
		ManagedClients: map[string]pmkeycloakv1alpha1.ManagedClient{"portal": {ClientID: "id-1", RegistrationClientURI: "https://kc/reg/id-1"}},
	}

	t.Run("marks the legacy object migrated and copies the status", func(t *testing.T) {
		t.Parallel()
		cl := legacyClient(legacyIPC())
		getter := mocks.NewMockKCPClientGetter(t)
		getter.EXPECT().NewClientForLogicalCluster(mock.Anything, "system#abc").Return(cl, nil)

		mirror := IdentityProviderConfigurationMirror(getter)
		require.NoError(t, mirror(t.Context(), "keycloak#abc", "acme", status))
		require.NoError(t, mirror(t.Context(), "keycloak#abc", "acme", status), "idempotent")

		//nolint:staticcheck // legacy kind under test
		var got pmcorev1alpha1.IdentityProviderConfiguration
		require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: "acme"}, &got))
		assert.Equal(t, "true", got.Annotations[MigratedAnnotation])
		assert.Equal(t, IdentityProviderConfigurationStatusFromCurrent(status), got.Status)
		assert.Equal(t, "id-1", got.Status.ManagedClients["portal"].ClientID)
	})

	t.Run("nothing without a legacy object", func(t *testing.T) {
		t.Parallel()
		getter := mocks.NewMockKCPClientGetter(t)
		getter.EXPECT().NewClientForLogicalCluster(mock.Anything, "system#abc").Return(legacyClient(), nil)

		require.NoError(t, IdentityProviderConfigurationMirror(getter)(t.Context(), "keycloak#abc", "acme", status))
	})
}
