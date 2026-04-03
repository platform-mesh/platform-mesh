package workspaceready_test

import (
	"context"
	"errors"
	"testing"

	kcpcorev1alpha "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
	"github.com/platform-mesh/account-operator/pkg/subroutines/workspaceready"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
)

func TestProcess(t *testing.T) {
	testCases := []struct {
		name            string
		obj             *corev1alpha1.Account
		k8sMocks        func(m *mocks.Client)
		expectRequeue   bool
		expectError     bool
		getClusterError bool
	}{
		{
			name: "success when workspace phase is Ready",
			obj: &corev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			k8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					RunAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						ws := obj.(*kcptenancyv1alpha.Workspace)
						ws.Status.Phase = kcpcorev1alpha.LogicalClusterPhaseReady
						return nil
					})
			},
		},
		{
			name: "requeue when workspace phase is not Ready",
			obj: &corev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			k8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					RunAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						ws := obj.(*kcptenancyv1alpha.Workspace)
						ws.Status.Phase = kcpcorev1alpha.LogicalClusterPhaseInitializing
						return nil
					})
			},
			expectRequeue: true,
		},
		{
			name: "error when workspace not found",
			obj: &corev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			k8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(kerrors.NewNotFound(schema.GroupResource{}, "test"))
			},
			expectError: true,
		},
		{
			name: "error on get workspace failure",
			obj: &corev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			k8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(errors.New("some-error"))
			},
			expectError: true,
		},
		{
			name: "error when GetCluster fails",
			obj: &corev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			expectError:     true,
			getClusterError: true,
		},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			mgr := mocks.NewManager(t)

			if test.getClusterError {
				mgr.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(nil, errors.New("cluster-error"))
			} else {
				cluster := mocks.NewCluster(t)
				client := mocks.NewClient(t)

				mgr.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(client)

				if test.k8sMocks != nil {
					test.k8sMocks(client)
				}
			}

			s, err := workspaceready.New(mgr)
			assert.NoError(t, err)

			ctx := mccontext.WithCluster(t.Context(), "test")
			result, processErr := s.Process(ctx, test.obj)

			if test.expectError {
				assert.Error(t, processErr)
			} else {
				assert.NoError(t, processErr)
			}
			if test.expectRequeue {
				assert.Greater(t, result.Requeue().Microseconds(), int64(0))
			}
		})
	}
}
