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

package migrations

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmkeycloakv1alpha1 "go.platform-mesh.io/apis/keycloak/v1alpha1"
	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/pkg/subroutines/mocks"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

func keycloakMigrationScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(pmcorev1alpha1.AddToScheme(scheme))
	utilruntime.Must(pmkeycloakv1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpapisv1alpha2.AddToScheme(scheme))
	utilruntime.Must(kcpcorev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha1.AddToScheme(scheme))
	return scheme
}

func keycloakMigrationClient(objs ...ctrlruntimeclient.Object) ctrlruntimeclient.Client {
	return fake.NewClientBuilder().
		WithScheme(keycloakMigrationScheme()).
		WithStatusSubresource(&pmkeycloakv1alpha1.IdentityProviderConfiguration{}, &kcpcorev1alpha1.LogicalCluster{}).
		WithObjects(objs...).
		Build()
}

func readyWorkspace(name string) *kcptenancyv1alpha1.Workspace {
	return &kcptenancyv1alpha1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status:     kcptenancyv1alpha1.WorkspaceStatus{Phase: kcpcorev1alpha1.LogicalClusterPhaseReady},
	}
}

//nolint:staticcheck // legacy kind under test
func legacyIdentityProviderConfiguration(name string) *pmcorev1alpha1.IdentityProviderConfiguration {
	return &pmcorev1alpha1.IdentityProviderConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      map[string]string{"keep": "me"},
			Finalizers:  []string{"core.platform-mesh.io/idp-finalizer"},
			Annotations: map[string]string{"note": "x"},
		},
		Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
			RegistrationAllowed: true,
			Clients: []pmcorev1alpha1.IdentityProviderClientConfig{{
				ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
				ClientName:   name,
				RedirectURIs: []string{"https://" + name + ".example.com/*"},
				SecretRef:    corev1.SecretReference{Name: "portal-client-secret-" + name, Namespace: "default"},
			}},
		},
		Status: pmcorev1alpha1.IdentityProviderConfigurationStatus{
			ManagedClients: map[string]pmcorev1alpha1.ManagedClient{
				name: {
					ClientID:              "client-id-" + name,
					RegistrationClientURI: "https://kc/realms/" + name + "/clients-registrations/openid-connect/client-id-" + name,
					SecretRef:             corev1.SecretReference{Name: "portal-client-secret-" + name, Namespace: "default"},
				},
			},
		},
	}
}

func legacyClientSecret(name string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "portal-client-secret-" + name,
			Namespace: "default",
			Labels:    map[string]string{legacyIDPNameLabel: name, legacyClientNameLabel: name},
		},
		Data: map[string][]byte{"client_secret": []byte("s3cret")},
	}
}

func TestKeycloakAPIGroupSplit_relabelClientSecrets(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(t.Context(), keys.LoggerCtxKey, testLogger(t))

	cl := keycloakMigrationClient(legacyIdentityProviderConfiguration("acme"), legacyClientSecret("acme"))

	require.NoError(t, relabelClientSecrets(ctx, cl, cl))

	var secret corev1.Secret
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: "portal-client-secret-acme", Namespace: "default"}, &secret))
	assert.Equal(t, "acme", secret.Labels[idpNameLabel])
	assert.Equal(t, "acme", secret.Labels[clientNameLabel])
	assert.Equal(t, "acme", secret.Labels[legacyIDPNameLabel], "legacy labels stay for the core export claim")

	//nolint:staticcheck // legacy kind under test
	var legacy pmcorev1alpha1.IdentityProviderConfiguration
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: "acme"}, &legacy), "the deprecated object is left to its owner")
	assert.Equal(t, map[string]string{"note": "x"}, legacy.Annotations, "untouched")

	// Idempotent: a second run finds nothing to do.
	require.NoError(t, relabelClientSecrets(ctx, cl, cl))
}

func TestKeycloakAPIGroupSplit_ensureKeycloakTerminator(t *testing.T) {
	t.Parallel()

	cl := keycloakMigrationClient(&kcpcorev1alpha1.LogicalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: kcpcorev1alpha1.LogicalClusterName},
		Spec:       kcpcorev1alpha1.LogicalClusterSpec{Terminators: []kcpcorev1alpha1.LogicalClusterTerminator{"root:security"}},
		Status:     kcpcorev1alpha1.LogicalClusterStatus{Terminators: []kcpcorev1alpha1.LogicalClusterTerminator{"root:security"}},
	})

	require.NoError(t, ensureKeycloakTerminator(t.Context(), cl))
	require.NoError(t, ensureKeycloakTerminator(t.Context(), cl), "idempotent")

	var lc kcpcorev1alpha1.LogicalCluster
	require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: kcpcorev1alpha1.LogicalClusterName}, &lc))
	assert.Equal(t, []kcpcorev1alpha1.LogicalClusterTerminator{"root:security", keycloakTerminator}, lc.Spec.Terminators)
	assert.Equal(t, []kcpcorev1alpha1.LogicalClusterTerminator{"root:security", keycloakTerminator}, lc.Status.Terminators, "kcp terminates by status.terminators")
}

func TestKeycloakAPIGroupSplit_ensureKeycloakBinding(t *testing.T) {
	t.Parallel()
	cl := keycloakMigrationClient()

	require.NoError(t, ensureKeycloakBinding(t.Context(), cl))

	binding := &unstructured.Unstructured{}
	binding.SetGroupVersionKind(schema.GroupVersionKind{Group: "apis.kcp.io", Version: "v1alpha2", Kind: "APIBinding"})
	require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: keycloakPlatformMeshIOExport}, binding))
	path, _, _ := unstructured.NestedString(binding.Object, "spec", "reference", "export", "path")
	assert.Equal(t, platformMeshSystemPath, path)
}

func TestKeycloakAPIGroupSplit_Run(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(t.Context(), keys.LoggerCtxKey, testLogger(t))

	t.Run("skipped when Keycloak is disabled", func(t *testing.T) {
		t.Parallel()
		helper := new(mocks.KcpHelper)
		inst := &pmcorev1alpha1.PlatformMesh{Spec: pmcorev1alpha1.PlatformMeshSpec{
			FeatureToggles: []pmcorev1alpha1.FeatureToggle{{Name: featureDisableKeycloak}},
		}}

		require.NoError(t, keycloakAPIGroupSplit{}.Run(ctx, Deps{KcpHelper: helper, Config: &rest.Config{}, Instance: inst}))
		helper.AssertNotCalled(t, "NewKcpClient", mock.Anything, mock.Anything)
	})

	t.Run("walks organizations and accounts", func(t *testing.T) {
		t.Parallel()
		system := keycloakMigrationClient(legacyIdentityProviderConfiguration("welcome"))
		orgs := keycloakMigrationClient(readyWorkspace("acme"), legacyIdentityProviderConfiguration("acme"), legacyClientSecret("acme"))
		org := keycloakMigrationClient(
			readyWorkspace("team"),
			&kcpcorev1alpha1.LogicalCluster{ObjectMeta: metav1.ObjectMeta{Name: kcpcorev1alpha1.LogicalClusterName}},
		)
		account := keycloakMigrationClient()
		helper := new(mocks.KcpHelper)
		helper.EXPECT().NewKcpClient(mock.Anything, platformMeshSystemPath).Return(system, nil)
		helper.EXPECT().NewKcpClient(mock.Anything, orgsPath).Return(orgs, nil)
		helper.EXPECT().NewKcpClient(mock.Anything, "root:orgs:acme").Return(org, nil)
		helper.EXPECT().NewKcpClient(mock.Anything, "root:orgs:acme:team").Return(account, nil)

		require.NoError(t, keycloakAPIGroupSplit{}.Run(ctx, Deps{KcpHelper: helper, Config: &rest.Config{}, Instance: &pmcorev1alpha1.PlatformMesh{}}))

		var secret corev1.Secret
		require.NoError(t, orgs.Get(ctx, types.NamespacedName{Name: "portal-client-secret-acme", Namespace: "default"}, &secret))
		assert.Equal(t, "acme", secret.Labels[idpNameLabel])
		var lc kcpcorev1alpha1.LogicalCluster
		require.NoError(t, org.Get(ctx, types.NamespacedName{Name: kcpcorev1alpha1.LogicalClusterName}, &lc))
		assert.Contains(t, lc.Spec.Terminators, kcpcorev1alpha1.LogicalClusterTerminator(keycloakTerminator))
		assert.Contains(t, lc.Status.Terminators, kcpcorev1alpha1.LogicalClusterTerminator(keycloakTerminator))
		for _, cl := range []ctrlruntimeclient.Client{org, account} {
			binding := &unstructured.Unstructured{}
			binding.SetGroupVersionKind(schema.GroupVersionKind{Group: "apis.kcp.io", Version: "v1alpha2", Kind: "APIBinding"})
			assert.NoError(t, cl.Get(ctx, types.NamespacedName{Name: keycloakPlatformMeshIOExport}, binding))
		}
	})
}

func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	cfg := logger.DefaultConfig()
	cfg.NoJSON = true
	log, err := logger.New(cfg)
	require.NoError(t, err)
	return log
}
