package subroutines_test

import (
	"context"

	kcpcorev1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
)

func mockGetWorkspaceByName(clientMock *mocks.Client, ready kcpcorev1alpha1.LogicalClusterPhaseType, path string) *mocks.Client_Get_Call {
	return clientMock.EXPECT().
		Get(mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.Workspace")).
		Run(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) {
			wsPath := ""
			if path != "" {
				wsPath = "https://example.com/" + path
			}
			actual, _ := obj.(*kcptenancyv1alpha.Workspace)
			actual.Name = key.Name
			actual.Spec = kcptenancyv1alpha.WorkspaceSpec{
				Cluster: "some-cluster-id-" + key.Name,
				URL:     wsPath,
			}
			actual.Status.Phase = ready
		}).
		Return(nil)
}
