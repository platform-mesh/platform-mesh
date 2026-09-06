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

package webhook

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestValidateIdPRegistrationSpec_requiresSecretSource(t *testing.T) {
	t.Parallel()

	err := validateIdPRegistrationSpec(pmcorev1alpha1.IdPRegistrationSpec{
		Alias: "corp",
		Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
		OIDC: &pmcorev1alpha1.IdPRegistrationOIDCConfig{
			ClientID:     "client",
			DiscoveryURL: "https://idp.example.com/.well-known/openid-configuration",
		},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "oidc.clientSecret")
}

func TestIdPRegistrationDefaulter_Default_persistsSecretAndSetsRef(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	defaulter := &idpRegistrationDefaulter{resolver: &fakeClusterManager{client: cl}}

	reg := &pmcorev1alpha1.IdPRegistration{
		ObjectMeta: metav1.ObjectMeta{Name: "dex"},
		Spec: pmcorev1alpha1.IdPRegistrationSpec{
			Alias: "dex",
			Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
			OIDC: &pmcorev1alpha1.IdPRegistrationOIDCConfig{
				ClientID:     "broker",
				ClientSecret: "super-secret",
			},
		},
	}

	require.NoError(t, defaulter.Default(context.Background(), reg))
	assert.Empty(t, reg.Spec.OIDC.ClientSecret)
	assert.Equal(t, "idpregistration-dex-client-secret", reg.Spec.OIDC.ClientSecretRef.Name)

	secret := &corev1.Secret{}
	require.NoError(t, cl.Get(context.Background(), ctrlruntimeclient.ObjectKey{
		Name:      "idpregistration-dex-client-secret",
		Namespace: idpRegistrationSecretNamespace,
	}, secret))
	assert.Equal(t, "super-secret", string(secret.Data[idpRegistrationSecretDataKey]))
}

func TestIdPRegistrationDefaulter_Default_updatesExistingSecret(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "custom-secret",
			Namespace: idpRegistrationSecretNamespace,
			Labels: map[string]string{
				idpRegistrationSecretLabel: "dex",
			},
		},
		Data: map[string][]byte{
			idpRegistrationSecretDataKey: []byte("old"),
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	defaulter := &idpRegistrationDefaulter{resolver: &fakeClusterManager{client: cl}}

	reg := &pmcorev1alpha1.IdPRegistration{
		ObjectMeta: metav1.ObjectMeta{Name: "dex"},
		Spec: pmcorev1alpha1.IdPRegistrationSpec{
			Alias: "dex",
			Type:  pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC,
			OIDC: &pmcorev1alpha1.IdPRegistrationOIDCConfig{
				ClientID: "broker",
				ClientSecretRef: pmcorev1alpha1.IdPRegistrationSecretRef{
					Name: "custom-secret",
				},
				ClientSecret: "new-secret",
			},
		},
	}

	require.NoError(t, defaulter.Default(context.Background(), reg))

	secret := &corev1.Secret{}
	require.NoError(t, cl.Get(context.Background(), ctrlruntimeclient.ObjectKey{
		Name:      "custom-secret",
		Namespace: idpRegistrationSecretNamespace,
	}, secret))
	assert.Equal(t, "new-secret", string(secret.Data[idpRegistrationSecretDataKey]))
}

type fakeClusterManager struct {
	client ctrlruntimeclient.Client
}

func (f *fakeClusterManager) ClusterClient(context.Context, *pmcorev1alpha1.IdPRegistration) (ctrlruntimeclient.Client, error) {
	return f.client, nil
}
