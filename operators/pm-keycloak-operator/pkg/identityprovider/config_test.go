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
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	pmkeycloakv1alpha1 "go.platform-mesh.io/apis/keycloak/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func configurationFixture(t *testing.T) (ctrlruntimeclient.Client, ctrlruntimeclient.Client, *pmidentityv1alpha1.IdentityProviderClass, *pmkeycloakv1alpha1.KeycloakProviderConfiguration) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, pmkeycloakv1alpha1.AddToScheme(scheme))
	configuration := &pmkeycloakv1alpha1.KeycloakProviderConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "default", UID: "config-uid"},
		Spec: pmkeycloakv1alpha1.KeycloakProviderConfigurationSpec{
			AdminURL: "https://internal.example/keycloak", PublicURL: "https://public.example/keycloak",
			CredentialsSecretRef: corev1.LocalObjectReference{Name: "credentials"},
		},
	}
	classes := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configuration).Build()
	private := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "credentials", Namespace: "private", UID: "credentials-uid"},
		Data:       map[string][]byte{"client_id": []byte("service"), "client_secret": []byte("private-credential")},
	}).Build()
	return classes, private, &pmidentityv1alpha1.IdentityProviderClass{
		ObjectMeta: metav1.ObjectMeta{Name: "default", UID: "class-uid"},
		Spec: pmidentityv1alpha1.IdentityProviderClassSpec{
			ConfigurationRef: pmidentityv1alpha1.IdentityProviderObjectReference{
				APIGroup: pmkeycloakv1alpha1.GroupName, Kind: "KeycloakProviderConfiguration", Name: "default",
			},
		},
	}, configuration
}

func TestConfigurationRotationDoesNotChangeRemoteOwnership(t *testing.T) {
	classes, private, class, configuration := configurationFixture(t)
	first, err := LoadConfiguration(t.Context(), classes, private, class, "private", time.Second)
	require.NoError(t, err)
	defer first.Close()
	require.Equal(t, "default", first.OutputNamespace)
	require.Equal(t, 28800, first.Backend.config.AccessTokenLifespan)
	credentials := &corev1.Secret{}
	require.NoError(t, private.Get(t.Context(), ctrlruntimeclient.ObjectKey{Namespace: "private", Name: "credentials"}, credentials))
	credentials.Data["client_secret"] = []byte("rotated-private-credential")
	require.NoError(t, private.Update(t.Context(), credentials))
	rotated, err := LoadConfiguration(t.Context(), classes, private, class, "private", time.Second)
	require.NoError(t, err)
	defer rotated.Close()
	require.NotEqual(t, first.Revision, rotated.Revision)
	require.Equal(t, first.Identity, rotated.Identity)
	require.NoError(t, classes.Get(t.Context(), ctrlruntimeclient.ObjectKeyFromObject(configuration), configuration))
	configuration.Spec.PublicURL = "https://different.example/keycloak"
	require.NoError(t, classes.Update(t.Context(), configuration))
	changed, err := LoadConfiguration(t.Context(), classes, private, class, "private", time.Second)
	require.NoError(t, err)
	defer changed.Close()
	require.NotEqual(t, first.Identity, changed.Identity)
}

func TestConfigurationTrustAndPrivateSecrets(t *testing.T) {
	t.Run("references cannot select another runtime namespace", func(t *testing.T) {
		classes, private, class, _ := configurationFixture(t)
		_, err := LoadConfiguration(t.Context(), classes, private, class, "tenant-selected", time.Second)
		require.ErrorContains(t, err, "runtime Secret")
	})
	for _, tt := range []struct {
		name string
		edit func(*pmkeycloakv1alpha1.KeycloakProviderConfiguration)
	}{
		{"public SMTP password", func(c *pmkeycloakv1alpha1.KeycloakProviderConfiguration) {
			c.Spec.SMTP = map[string]string{"password": "forbidden"}
		}},
		{"untrusted HTTP", func(c *pmkeycloakv1alpha1.KeycloakProviderConfiguration) { c.Spec.AdminURL = "http://internal.example" }},
		{"bad CA", func(c *pmkeycloakv1alpha1.KeycloakProviderConfiguration) {
			c.Spec.CASecretRef = &corev1.LocalObjectReference{Name: "credentials"}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			classes, private, class, configuration := configurationFixture(t)
			require.NoError(t, classes.Get(t.Context(), ctrlruntimeclient.ObjectKeyFromObject(configuration), configuration))
			tt.edit(configuration)
			require.NoError(t, classes.Update(t.Context(), configuration))
			_, err := LoadConfiguration(t.Context(), classes, private, class, "private", time.Second)
			require.Error(t, err)
		})
	}
	t.Run("SMTP password comes only from the fixed runtime namespace", func(t *testing.T) {
		classes, private, class, configuration := configurationFixture(t)
		require.NoError(t, private.Create(t.Context(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "smtp", Namespace: "private"},
			Data:       map[string][]byte{"password": []byte("private-smtp")},
		}))
		require.NoError(t, classes.Get(t.Context(), ctrlruntimeclient.ObjectKeyFromObject(configuration), configuration))
		configuration.Spec.SMTPSecretRef = &corev1.LocalObjectReference{Name: "smtp"}
		require.NoError(t, classes.Update(t.Context(), configuration))
		loaded, err := LoadConfiguration(t.Context(), classes, private, class, "private", time.Second)
		require.NoError(t, err)
		defer loaded.Close()
		require.Equal(t, "private-smtp", loaded.Backend.config.SMTP["password"])
	})
}
