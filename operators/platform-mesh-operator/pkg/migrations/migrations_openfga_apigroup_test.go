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
	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/platform-mesh-operator/pkg/subroutines/mocks"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

func logicalClusterWithTerminators(terminators ...kcpcorev1alpha1.LogicalClusterTerminator) *kcpcorev1alpha1.LogicalCluster {
	return &kcpcorev1alpha1.LogicalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: kcpcorev1alpha1.LogicalClusterName},
		Spec:       kcpcorev1alpha1.LogicalClusterSpec{Terminators: terminators},
	}
}

func terminatorsOf(t *testing.T, cl ctrlruntimeclient.Client) []kcpcorev1alpha1.LogicalClusterTerminator {
	t.Helper()
	var lc kcpcorev1alpha1.LogicalCluster
	require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: kcpcorev1alpha1.LogicalClusterName}, &lc))
	return lc.Spec.Terminators
}

func TestOpenfgaAPIGroupSplit_replaceSecurityTerminator(t *testing.T) {
	t.Parallel()

	t.Run("swaps security for openfga", func(t *testing.T) {
		t.Parallel()
		cl := keycloakMigrationClient(logicalClusterWithTerminators(securityTerminator, keycloakTerminator))

		require.NoError(t, replaceSecurityTerminator(t.Context(), cl))
		require.NoError(t, replaceSecurityTerminator(t.Context(), cl), "idempotent")

		assert.Equal(t, []kcpcorev1alpha1.LogicalClusterTerminator{keycloakTerminator, openfgaTerminator}, terminatorsOf(t, cl))
	})

	t.Run("leaves a workspace without the security terminator alone", func(t *testing.T) {
		t.Parallel()
		cl := keycloakMigrationClient(logicalClusterWithTerminators(keycloakTerminator))

		require.NoError(t, replaceSecurityTerminator(t.Context(), cl))

		assert.Equal(t, []kcpcorev1alpha1.LogicalClusterTerminator{keycloakTerminator}, terminatorsOf(t, cl))
	})

	t.Run("does not duplicate openfga", func(t *testing.T) {
		t.Parallel()
		cl := keycloakMigrationClient(logicalClusterWithTerminators(openfgaTerminator, securityTerminator))

		require.NoError(t, replaceSecurityTerminator(t.Context(), cl))

		assert.Equal(t, []kcpcorev1alpha1.LogicalClusterTerminator{openfgaTerminator}, terminatorsOf(t, cl))
	})
}

func TestOpenfgaAPIGroupSplit_Run(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(t.Context(), keys.LoggerCtxKey, testLogger(t))

	t.Run("skipped when OpenFGA is disabled", func(t *testing.T) {
		t.Parallel()
		helper := new(mocks.KcpHelper)
		inst := &pmcorev1alpha1.PlatformMesh{Spec: pmcorev1alpha1.PlatformMeshSpec{
			FeatureToggles: []pmcorev1alpha1.FeatureToggle{{Name: featureDisableOpenFGA}},
		}}

		require.NoError(t, openfgaAPIGroupSplit{}.Run(ctx, Deps{KcpHelper: helper, Config: &rest.Config{}, Instance: inst}))
		helper.AssertNotCalled(t, "NewKcpClient", mock.Anything, mock.Anything)
	})

	t.Run("walks organizations, accounts and providers", func(t *testing.T) {
		t.Parallel()
		orgs := keycloakMigrationClient(readyWorkspace("acme"))
		org := keycloakMigrationClient(readyWorkspace("team"), logicalClusterWithTerminators(securityTerminator))
		account := keycloakMigrationClient(logicalClusterWithTerminators(securityTerminator))
		providers := keycloakMigrationClient(readyWorkspace("httpbin"), readyWorkspace(providersSystemName))
		provider := keycloakMigrationClient()
		helper := new(mocks.KcpHelper)
		helper.EXPECT().NewKcpClient(mock.Anything, orgsPath).Return(orgs, nil)
		helper.EXPECT().NewKcpClient(mock.Anything, "root:orgs:acme").Return(org, nil)
		helper.EXPECT().NewKcpClient(mock.Anything, "root:orgs:acme:team").Return(account, nil)
		helper.EXPECT().NewKcpClient(mock.Anything, providersPath).Return(providers, nil)
		helper.EXPECT().NewKcpClient(mock.Anything, "root:providers:httpbin").Return(provider, nil)

		require.NoError(t, openfgaAPIGroupSplit{}.Run(ctx, Deps{KcpHelper: helper, Config: &rest.Config{}, Instance: &pmcorev1alpha1.PlatformMesh{}}))

		assert.Equal(t, []kcpcorev1alpha1.LogicalClusterTerminator{openfgaTerminator}, terminatorsOf(t, org))
		assert.Equal(t, []kcpcorev1alpha1.LogicalClusterTerminator{openfgaTerminator}, terminatorsOf(t, account))
		binding := &unstructured.Unstructured{}
		binding.SetGroupVersionKind(schema.GroupVersionKind{Group: "apis.kcp.io", Version: "v1alpha2", Kind: "APIBinding"})
		require.NoError(t, provider.Get(ctx, types.NamespacedName{Name: providerOpenfgaPlatformMeshIOExport}, binding))
		path, _, _ := unstructured.NestedString(binding.Object, "spec", "reference", "export", "path")
		assert.Equal(t, platformMeshSystemPath, path)
		helper.AssertNotCalled(t, "NewKcpClient", mock.Anything, "root:providers:system")
	})
}
