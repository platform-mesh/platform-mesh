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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/security-operator/internal/subroutine"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"
	"go.platform-mesh.io/subroutines"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

func TestAccountInfoFinalizerSubroutine_GetName(t *testing.T) {
	sub := subroutine.NewAccountInfoFinalizerSubroutine(nil)
	assert.Equal(t, "AccountInfoFinalizer", sub.GetName())
}

func TestAccountInfoFinalizerSubroutine_Finalizers(t *testing.T) {
	sub := subroutine.NewAccountInfoFinalizerSubroutine(nil)
	finalizers := sub.Finalizers(nil)
	assert.Equal(t, []string{"security.platform-mesh.io/accountinfo-finalizer"}, finalizers)
}

func TestAccountInfoFinalizerSubroutine_Finalize(t *testing.T) {
	tests := []struct {
		name           string
		mockSetup      func(*mocks.MockManager, *mocks.MockCluster, *mocks.MockClient)
		expectError    bool
		expectedResult subroutines.Result
	}{
		{
			name: "error on ClusterFromContext",
			mockSetup: func(manager *mocks.MockManager, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(nil, assert.AnError)
			},
			expectError: true,
		},
		{
			name: "error on List APIBindings",
			mockSetup: func(manager *mocks.MockManager, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				kcpClient.EXPECT().List(mock.Anything, mock.Anything).Return(assert.AnError)
			},
			expectError: true,
		},
		{
			name: "no APIBindings exist - allow deletion",
			mockSetup: func(manager *mocks.MockManager, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				kcpClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
					list := ol.(*kcpapisv1alpha2.APIBindingList)
					list.Items = []kcpapisv1alpha2.APIBinding{}
					return nil
				})
			},
			expectError:    false,
			expectedResult: subroutines.OK(),
		},
		{
			name: "APIBindings exist without finalizer - allow deletion",
			mockSetup: func(manager *mocks.MockManager, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				kcpClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
					list := ol.(*kcpapisv1alpha2.APIBindingList)
					list.Items = []kcpapisv1alpha2.APIBinding{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:       "binding1",
								Finalizers: []string{"other-finalizer"},
							},
						},
					}
					return nil
				})
			},
			expectError:    false,
			expectedResult: subroutines.OK(),
		},
		{
			name: "APIBinding exists with apibinding-finalizer - requeue",
			mockSetup: func(manager *mocks.MockManager, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				kcpClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
					list := ol.(*kcpapisv1alpha2.APIBindingList)
					list.Items = []kcpapisv1alpha2.APIBinding{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:       "binding1",
								Finalizers: []string{"core.platform-mesh.io/apibinding-finalizer"},
							},
						},
					}
					return nil
				})
			},
			expectError: false,
			expectedResult: subroutines.StopWithRequeue(5*time.Second,
				"APIBinding still has finalizer, requeuing AccountInfo deletion"),
		},
		{
			name: "multiple APIBindings - one with finalizer - requeue",
			mockSetup: func(manager *mocks.MockManager, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				kcpClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
					list := ol.(*kcpapisv1alpha2.APIBindingList)
					list.Items = []kcpapisv1alpha2.APIBinding{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:       "binding1",
								Finalizers: []string{"other-finalizer"},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:       "binding2",
								Finalizers: []string{"core.platform-mesh.io/apibinding-finalizer"},
							},
						},
					}
					return nil
				})
			},
			expectError: false,
			expectedResult: subroutines.StopWithRequeue(5*time.Second,
				"APIBinding still has finalizer, requeuing AccountInfo deletion"),
		},
		{
			name: "multiple APIBindings - none with target finalizer - allow deletion",
			mockSetup: func(manager *mocks.MockManager, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				kcpClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
					list := ol.(*kcpapisv1alpha2.APIBindingList)
					list.Items = []kcpapisv1alpha2.APIBinding{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:       "binding1",
								Finalizers: []string{"other-finalizer-1"},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:       "binding2",
								Finalizers: []string{"other-finalizer-2"},
							},
						},
					}
					return nil
				})
			},
			expectError:    false,
			expectedResult: subroutines.OK(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := mocks.NewMockManager(t)
			cluster := mocks.NewMockCluster(t)
			kcpClient := mocks.NewMockClient(t)

			if test.mockSetup != nil {
				test.mockSetup(manager, cluster, kcpClient)
			}

			sub := subroutine.NewAccountInfoFinalizerSubroutine(manager)
			result, err := sub.Finalize(context.Background(), &pmcorev1alpha1.AccountInfo{})

			if test.expectError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, test.expectedResult, result)
			}
		})
	}
}
