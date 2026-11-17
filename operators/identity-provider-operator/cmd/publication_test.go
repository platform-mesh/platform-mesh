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

package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	"go.platform-mesh.io/golang-commons/idpactivation"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestLoadOrganizationClients(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clients.json")
	require.NoError(t, os.WriteFile(path, []byte(`[{
		"name":"portal",
		"purpose":"Portal",
		"grantTypes":["authorization_code"],
		"tokenEndpointAuthMethod":"none"
	}]`), 0o600))

	clients, digest, err := loadOrganizationClients(path)
	require.NoError(t, err)
	require.True(t, idpactivation.Digest(digest))
	require.Equal(t, []pmidentityv1alpha1.OIDCClient{{
		Name: "portal", Purpose: pmidentityv1alpha1.IdentityClientPurposePortal,
		GrantTypes:              []string{"authorization_code"},
		TokenEndpointAuthMethod: pmidentityv1alpha1.ClientTokenEndpointAuthMethodNone,
	}}, clients)
	firstDigest := digest
	require.NoError(t, os.WriteFile(path, []byte(`[{"name":"portal","purpose":"Portal","grantTypes":["authorization_code"],"tokenEndpointAuthMethod":"none"}]`), 0o600))
	_, compactDigest, err := loadOrganizationClients(path)
	require.NoError(t, err)
	require.Equal(t, firstDigest, compactDigest)
	require.NoError(t, os.WriteFile(path, []byte(`[{"name":"portal","purpose":"Portal","redirectUris":["https://portal.example/callback"],"grantTypes":["authorization_code"],"tokenEndpointAuthMethod":"none"}]`), 0o600))
	_, changedDigest, err := loadOrganizationClients(path)
	require.NoError(t, err)
	require.NotEqual(t, firstDigest, changedDigest)

	for _, invalid := range []string{
		`[]`,
		`[{"name":"portal","unknown":"field"}]`,
		`[] []`,
		`null`,
	} {
		require.NoError(t, os.WriteFile(path, []byte(invalid), 0o600))
		_, _, err := loadOrganizationClients(path)
		require.Error(t, err, "input: %s", invalid)
	}
}

func TestOrganizationPublicationControllerIgnoresNonOrganizationAccounts(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, pmcorev1alpha1.AddToScheme(scheme))
	organizations := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&pmcorev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "account-a"},
		Spec:       pmcorev1alpha1.AccountSpec{Type: pmcorev1alpha1.AccountTypeAccount},
	}).Build()
	controller := &organizationPublicationController{
		Organizations: organizations, RequestTimeout: time.Second, ResyncInterval: time.Second,
	}

	result, err := controller.Reconcile(t.Context(), reconcile.Request{
		NamespacedName: ctrlruntimeclient.ObjectKey{Name: "account-a"},
	})
	require.NoError(t, err)
	require.Empty(t, result)
}
