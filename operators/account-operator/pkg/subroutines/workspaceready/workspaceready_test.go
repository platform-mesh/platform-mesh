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

package workspaceready_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"go.platform-mesh.io/account-operator/pkg/subroutines/mocks"
	"go.platform-mesh.io/account-operator/pkg/subroutines/workspaceready"
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

func TestProcess(t *testing.T) {
	testCases := []struct {
		name            string
		obj             *pmcorev1alpha1.Account
		k8sMocks        func(m *mocks.Client)
		expectRequeue   bool
		expectError     bool
		getClusterError bool
	}{
		{
			name: "success when workspace phase is Ready",
			obj: &pmcorev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			k8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					RunAndReturn(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						ws := obj.(*kcptenancyv1alpha1.Workspace)
						ws.Status.Phase = kcpcorev1alpha1.LogicalClusterPhaseReady
						return nil
					})
			},
		},
		{
			name: "requeue when workspace phase is not Ready",
			obj: &pmcorev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			k8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					RunAndReturn(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						ws := obj.(*kcptenancyv1alpha1.Workspace)
						ws.Status.Phase = kcpcorev1alpha1.LogicalClusterPhaseInitializing
						return nil
					})
			},
			expectRequeue: true,
		},
		{
			name: "error when workspace not found",
			obj: &pmcorev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			k8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(apierrors.NewNotFound(schema.GroupResource{}, "test"))
			},
			expectError: true,
		},
		{
			name: "error on get workspace failure",
			obj: &pmcorev1alpha1.Account{
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
			obj: &pmcorev1alpha1.Account{
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
