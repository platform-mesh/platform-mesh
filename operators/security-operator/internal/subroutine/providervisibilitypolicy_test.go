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

package subroutine_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/subroutine"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

func TestProviderVisibilityPolicySubroutine_Process(t *testing.T) {
	scheme := providerVisibilityPolicyScheme(t)

	t.Run("policy referencing existing provider resource", func(t *testing.T) {
		kcpClientGetter := mocks.NewMockKCPClientGetter(t)
		sut := subroutine.NewProviderVisibilityPolicySubroutine(kcpClientGetter)

		const providerClusterPath = "root:providers:abcd"
		const providerAPIExport = "widgets.abcd.io"

		policy := pmcorev1alpha1.ProviderVisibilityPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "abcd"},
			Spec: pmcorev1alpha1.ProviderVisibilityPolicySpec{
				ProviderExports: []pmcorev1alpha1.ProviderExport{
					{
						ProviderRef:    pmcorev1alpha1.ProviderRef{ClusterPath: providerClusterPath},
						APIExportNames: []string{providerAPIExport},
					},
				},
			},
		}

		providerClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(&kcpcorev1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
					Annotations: map[string]string{
						"kcp.io/cluster": "abcd-cluster-id",
					},
				},
			}).
			Build()

		policyClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(policy.DeepCopy()).
			WithStatusSubresource(&pmcorev1alpha1.ProviderVisibilityPolicy{}).
			Build()

		kcpClientGetter.EXPECT().
			NewClientForLogicalCluster(mock.Anything,
				string(config.MultiProviderName(config.CoreProviderName, providerClusterPath))).
			Return(providerClient, nil)

		kcpClientGetter.EXPECT().NewClientFromContext(mock.Anything).Return(policyClient, nil)

		_, err := sut.Process(t.Context(), &policy)
		if err != nil {
			t.Fatal(err)
		}
		require.NoError(t, err)

		var persisted pmcorev1alpha1.ProviderVisibilityPolicy
		require.NoError(t, policyClient.Get(t.Context(), ctrlruntimeclient.ObjectKeyFromObject(&policy), &persisted))

		require.Len(t, persisted.Status.ResolvedProviderExports, 1)
		assert.Equal(t, providerClusterPath, persisted.Status.ResolvedProviderExports[0].ClusterPath)
		assert.Equal(t, "abcd-cluster-id", persisted.Status.ResolvedProviderExports[0].ClusterID)
	})

}

func providerVisibilityPolicyScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(pmcorev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpcorev1alpha1.AddToScheme(scheme))

	return scheme
}
