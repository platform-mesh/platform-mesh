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
	"errors"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/security-operator/internal/subroutine"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	"github.com/kcp-dev/logicalcluster/v3"
)

func TestGetName(t *testing.T) {
	subroutine := subroutine.NewStoreSubroutine(nil, nil, nil)
	assert.Equal(t, "Store", subroutine.GetName())
}

func TestFinalizers(t *testing.T) {
	subroutine := subroutine.NewStoreSubroutine(nil, nil, nil)
	assert.Equal(t, []string{"core.platform-mesh.io/fga-store"}, subroutine.Finalizers(nil))
}

func TestProcess(t *testing.T) {
	tests := []struct {
		name        string
		store       *pmcorev1alpha1.Store
		fgaMocks    func(*mocks.MockOpenFGAServiceClient)
		expectError bool
	}{
		{
			name: "should try and create the store if it does not exist",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().ListStores(mock.Anything, mock.Anything).Return(&openfgav1.ListStoresResponse{}, nil)
				fga.EXPECT().CreateStore(mock.Anything, &openfgav1.CreateStoreRequest{Name: "store"}).Return(&openfgav1.CreateStoreResponse{Id: "id"}, nil)
			},
		},
		{
			name: "should skip creation if the store already exists",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().ListStores(mock.Anything, mock.Anything).Return(&openfgav1.ListStoresResponse{
					Stores: []*openfgav1.Store{
						{
							Name: "store",
						},
					},
				}, nil)
			},
		},
		{
			name: "should verify the store if .status.storeId is set",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Status: pmcorev1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().GetStore(mock.Anything, &openfgav1.GetStoreRequest{StoreId: "id"}).Return(&openfgav1.GetStoreResponse{Name: "store"}, nil)
			},
		},
		{
			name: "should verify and update the store if .status.storeId is set but name differs",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Status: pmcorev1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().GetStore(mock.Anything, &openfgav1.GetStoreRequest{StoreId: "id"}).Return(&openfgav1.GetStoreResponse{Name: "store2"}, nil)
				fga.EXPECT().UpdateStore(mock.Anything, &openfgav1.UpdateStoreRequest{StoreId: "id", Name: "store"}).Return(&openfgav1.UpdateStoreResponse{}, nil)
			},
		},
		{
			name: "should fail if store listing fails",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().ListStores(mock.Anything, mock.Anything).Return(nil, errors.New("error"))
			},
			expectError: true,
		},
		{
			name: "should fail if store creation fails",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().ListStores(mock.Anything, mock.Anything).Return(&openfgav1.ListStoresResponse{}, nil)
				fga.EXPECT().CreateStore(mock.Anything, mock.Anything).Return(nil, errors.New("error"))
			},
			expectError: true,
		},
		{
			name: "should fail if get store fails when verifying existing store",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Status: pmcorev1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().GetStore(mock.Anything, &openfgav1.GetStoreRequest{StoreId: "id"}).Return(nil, errors.New("get store failed"))
			},
			expectError: true,
		},
		{
			name: "should fail if update store fails when names differ",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Status: pmcorev1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().GetStore(mock.Anything, &openfgav1.GetStoreRequest{StoreId: "id"}).Return(&openfgav1.GetStoreResponse{Name: "different-name"}, nil)
				fga.EXPECT().UpdateStore(mock.Anything, &openfgav1.UpdateStoreRequest{StoreId: "id", Name: "store"}).Return(nil, errors.New("update failed"))
			},
			expectError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fga := mocks.NewMockOpenFGAServiceClient(t)
			if test.fgaMocks != nil {
				test.fgaMocks(fga)
			}

			manager := mocks.NewMockManager(t)
			kcpHelper := mocks.NewMockLister(t)
			subroutine := subroutine.NewStoreSubroutine(fga, manager, kcpHelper)

			_, err := subroutine.Process(context.Background(), test.store)
			if test.expectError {
				assert.Error(t, err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestFinalize(t *testing.T) {
	tests := []struct {
		name           string
		store          *pmcorev1alpha1.Store
		fgaMocks       func(*mocks.MockOpenFGAServiceClient)
		kcpHelperMocks func(*mocks.MockLister)
		expectError    bool
	}{
		{
			name: "should skip reconciliation if .status.storeId is not set",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
			},
		},
		{
			name: "should deny deletion if at least authorizationModel is referencing the store",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Status: pmcorev1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			kcpHelperMocks: func(kcpHelper *mocks.MockLister) {
				kcpHelper.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
					if list, ok := ol.(*pmcorev1alpha1.AuthorizationModelList); ok {
						list.Items = []pmcorev1alpha1.AuthorizationModel{
							{
								Spec: pmcorev1alpha1.AuthorizationModelSpec{
									StoreRef: pmcorev1alpha1.WorkspaceStoreRef{
										Name:    "store",
										Cluster: "path",
									},
								},
							},
						}
					}
					return nil
				}).Once()
			},
			expectError: true,
		},
		{
			name: "should deny deletion the list call is failing",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Status: pmcorev1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			kcpHelperMocks: func(kcpHelper *mocks.MockLister) {
				kcpHelper.EXPECT().List(mock.Anything, mock.Anything).Return(errors.New("error"))
			},
			expectError: true,
		},
		{
			name: "should delete the store if no authorizationModel is referencing it",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Status: pmcorev1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			kcpHelperMocks: func(kcpHelper *mocks.MockLister) {
				kcpHelper.EXPECT().List(mock.Anything, mock.Anything).Return(nil)
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().DeleteStore(mock.Anything, &openfgav1.DeleteStoreRequest{StoreId: "id"}).Return(nil, nil)
			},
		},
		{
			name: "should reconcile successfully if store is not found with the .status.storeId",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Status: pmcorev1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			kcpHelperMocks: func(kcpHelper *mocks.MockLister) {
				kcpHelper.EXPECT().List(mock.Anything, mock.Anything).Return(nil)
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().DeleteStore(mock.Anything, &openfgav1.DeleteStoreRequest{StoreId: "id"}).Return(nil, status.Error(codes.Code(openfgav1.NotFoundErrorCode_store_id_not_found), "not found"))
			},
		},
		{
			name: "should not reconcile successfully deletion is errorneous",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Status: pmcorev1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			kcpHelperMocks: func(kcpHelper *mocks.MockLister) {
				kcpHelper.EXPECT().List(mock.Anything, mock.Anything).Return(nil)
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().DeleteStore(mock.Anything, &openfgav1.DeleteStoreRequest{StoreId: "id"}).Return(nil, errors.New("error"))
			},
			expectError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fga := mocks.NewMockOpenFGAServiceClient(t)
			if test.fgaMocks != nil {
				test.fgaMocks(fga)
			}

			manager := mocks.NewMockManager(t)
			kcpHelper := mocks.NewMockLister(t)

			// Only wire kcpHelper expectations when Finalize will actually query k8s (i.e., StoreID is set)
			if test.store.Status.StoreID != "" && test.kcpHelperMocks != nil {
				test.kcpHelperMocks(kcpHelper)
			}

			subroutine := subroutine.NewStoreSubroutine(fga, manager, kcpHelper)

			ctx := mccontext.WithCluster(context.Background(), multicluster.ClusterName(logicalcluster.Name("path").String()))

			_, err := subroutine.Finalize(ctx, test.store)
			if test.expectError {
				assert.Error(t, err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}
