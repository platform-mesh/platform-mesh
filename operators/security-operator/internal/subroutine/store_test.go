package subroutine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kcp-dev/logicalcluster/v3"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/platform-mesh/security-operator/api/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/subroutine"
	"github.com/platform-mesh/security-operator/internal/subroutine/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
)

func TestGetName(t *testing.T) {
	subroutine := subroutine.NewStoreSubroutine(nil, nil)
	assert.Equal(t, "Store", subroutine.GetName())
}

func TestFinalizers(t *testing.T) {
	subroutine := subroutine.NewStoreSubroutine(nil, nil)
	assert.Equal(t, []string{"core.platform-mesh.io/fga-store"}, subroutine.Finalizers(nil))
}

func TestProcess(t *testing.T) {
	tests := []struct {
		name        string
		store       *v1alpha1.Store
		fgaMocks    func(*mocks.MockOpenFGAServiceClient)
		k8sMocks    func(*mocks.MockClient)
		mgrMocks    func(*mocks.MockManager)
		expectError bool
	}{
		{
			name: "should try and create the store if it does not exist",
			store: &v1alpha1.Store{
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
			store: &v1alpha1.Store{
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
			store: &v1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Status: v1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().GetStore(mock.Anything, &openfgav1.GetStoreRequest{StoreId: "id"}).Return(&openfgav1.GetStoreResponse{Name: "store"}, nil)
			},
		},
		{
			name: "should verify and update the store if .status.storeId is set but name differs",
			store: &v1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Status: v1alpha1.StoreStatus{
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
			store: &v1alpha1.Store{
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
			store: &v1alpha1.Store{
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
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			fga := mocks.NewMockOpenFGAServiceClient(t)
			if test.fgaMocks != nil {
				test.fgaMocks(fga)
			}

			manager := mocks.NewMockManager(t)
			subroutine := subroutine.NewStoreSubroutine(fga, manager)

			_, err := subroutine.Process(context.Background(), test.store)
			if test.expectError {
				assert.Error(t, err.Err())
			} else {
				assert.Nil(t, err)
			}

		})
	}
}

func TestFinalize(t *testing.T) {
	tests := []struct {
		name        string
		store       *v1alpha1.Store
		fgaMocks    func(*mocks.MockOpenFGAServiceClient)
		k8sMocks    func(*mocks.MockClient)
		mgrMocks    func(*mocks.MockManager)
		expectError bool
	}{
		{
			name: "should skip reconciliation if .status.storeId is not set",
			store: &v1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
			},
		},
		{
			name: "should deny deletion if at least authorizationModel is referencing the store",
			store: &v1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Status: v1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			k8sMocks: func(k8s *mocks.MockClient) {
				k8s.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					if list, ok := ol.(*v1alpha1.AuthorizationModelList); ok {
						list.Items = []v1alpha1.AuthorizationModel{
							{
								Spec: v1alpha1.AuthorizationModelSpec{
									StoreRef: v1alpha1.WorkspaceStoreRef{
										Name: "store",
										Path: "path",
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
			store: &v1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Status: v1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			k8sMocks: func(k8s *mocks.MockClient) {
				k8s.EXPECT().List(mock.Anything, mock.Anything).Return(errors.New("error"))
			},
			expectError: true,
		},
		{
			name: "should delete the store if no authorizationModel is referencing it",
			store: &v1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Status: v1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			k8sMocks: func(k8s *mocks.MockClient) {
				k8s.EXPECT().List(mock.Anything, mock.Anything).Return(nil)
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().DeleteStore(mock.Anything, &openfgav1.DeleteStoreRequest{StoreId: "id"}).Return(nil, nil)
			},
		},
		{
			name: "should reconcile successfully if store is not found with the .status.storeId",
			store: &v1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Status: v1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			k8sMocks: func(k8s *mocks.MockClient) {
				k8s.EXPECT().List(mock.Anything, mock.Anything).Return(nil)
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().DeleteStore(mock.Anything, &openfgav1.DeleteStoreRequest{StoreId: "id"}).Return(nil, status.Error(codes.Code(openfgav1.NotFoundErrorCode_store_id_not_found), "not found"))
			},
		},
		{
			name: "should not reconcile successfully deletion is errorneous",
			store: &v1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Status: v1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			k8sMocks: func(k8s *mocks.MockClient) {
				k8s.EXPECT().List(mock.Anything, mock.Anything).Return(nil)
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
			subroutine := subroutine.NewStoreSubroutine(fga, manager)

			// Only wire cluster/client expectations when Finalize will actually query k8s (i.e., StoreID is set)
			if test.store.Status.StoreID != "" {
				cluster := mocks.NewMockCluster(t)
				client := mocks.NewMockClient(t)
				if test.k8sMocks != nil {
					test.k8sMocks(client)
				}
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(client)
			}

			ctx := mccontext.WithCluster(context.Background(), string(logicalcluster.Name("path")))

			_, err := subroutine.Finalize(ctx, test.store)
			if test.expectError {
				assert.Error(t, err.Err())
			} else {
				assert.Nil(t, err)
			}

		})
	}
}
