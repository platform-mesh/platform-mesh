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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/security-operator/internal/subroutine"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestTupleGetName(t *testing.T) {
	subroutine := subroutine.NewTupleSubroutine(nil, nil)
	assert.Equal(t, "TupleSubroutine", subroutine.GetName())
}

func TestTupleFinalizers(t *testing.T) {
	subroutine := subroutine.NewTupleSubroutine(nil, nil)
	assert.Equal(t, []string{"core.platform-mesh.io/fga-tuples"}, subroutine.Finalizers(nil))
}

func TestTupleProcessWithStore(t *testing.T) {
	tests := []struct {
		name        string
		store       *corev1alpha1.Store
		fgaMocks    func(*mocks.MockOpenFGAServiceClient)
		k8sMocks    func(*mocks.MockClient)
		mgrMocks    func(*mocks.MockManager)
		expectError bool
	}{
		{
			name: "should process and add tuples to the store",
			store: &corev1alpha1.Store{
				Spec: corev1alpha1.StoreSpec{
					Tuples: []corev1alpha1.Tuple{
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user1",
						},
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user2",
						},
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user3",
						},
					},
				},
				Status: corev1alpha1.StoreStatus{
					StoreID:              "store-id",
					AuthorizationModelID: "auth-model-id",
					ManagedTuples: []corev1alpha1.Tuple{
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user1",
						},
					},
				},
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, nil)
			},
		},
		{
			name: "should process and add/remove tuples to the store",
			store: &corev1alpha1.Store{
				Spec: corev1alpha1.StoreSpec{
					Tuples: []corev1alpha1.Tuple{
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user1",
						},
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user2",
						},
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user3",
						},
					},
				},
				Status: corev1alpha1.StoreStatus{
					StoreID:              "store-id",
					AuthorizationModelID: "auth-model-id",
					ManagedTuples: []corev1alpha1.Tuple{
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user4",
						},
					},
				},
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				// Apply (batch write) + Delete (batch delete)
				fga.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, nil).Twice()
			},
		},
		{
			name: "should stop processing if an error occurs",
			store: &corev1alpha1.Store{
				Spec: corev1alpha1.StoreSpec{
					Tuples: []corev1alpha1.Tuple{
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user1",
						},
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user2",
						},
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user3",
						},
					},
				},
				Status: corev1alpha1.StoreStatus{
					StoreID:              "store-id",
					AuthorizationModelID: "auth-model-id",
					ManagedTuples: []corev1alpha1.Tuple{
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user1",
						},
					},
				},
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, errors.New("an error"))
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
			if test.mgrMocks != nil {
				test.mgrMocks(manager)
			}

			subroutine := subroutine.NewTupleSubroutine(fga, manager)

			_, err := subroutine.Process(context.Background(), test.store)
			if test.expectError {
				assert.Error(t, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, test.store.Status.ManagedTuples, test.store.Spec.Tuples)
			}

		})
	}
}

func TestTupleProcessWithAuthorizationModel(t *testing.T) {
	tests := []struct {
		name        string
		store       *corev1alpha1.AuthorizationModel
		fgaMocks    func(*mocks.MockOpenFGAServiceClient)
		k8sMocks    func(*mocks.MockClient)
		mgrMocks    func(*mocks.MockManager)
		expectError bool
	}{
		{
			name: "should process and add tuples to the authorization model",
			store: &corev1alpha1.AuthorizationModel{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						corev1alpha1.StoreRefLabelKey: "store",
					},
				},
				Spec: corev1alpha1.AuthorizationModelSpec{
					StoreRef: corev1alpha1.WorkspaceStoreRef{
						Name:    "store",
						Cluster: "store-cluster",
					},
					Tuples: []corev1alpha1.Tuple{
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user1",
						},
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user2",
						},
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user3",
						},
					},
				},
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, nil)
			},
			k8sMocks: func(k8s *mocks.MockClient) {
				// Not used for AuthorizationModel
			},
			mgrMocks: func(mgr *mocks.MockManager) {
				storeCluster := mocks.NewMockCluster(t)
				storeClient := mocks.NewMockClient(t)
				mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("store-cluster")).Return(storeCluster, nil)
				storeCluster.EXPECT().GetClient().Return(storeClient)
				storeClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					store := o.(*corev1alpha1.Store)
					*store = corev1alpha1.Store{
						Status: corev1alpha1.StoreStatus{
							StoreID:              "store-id",
							AuthorizationModelID: "auth-model-id",
						},
					}
					return nil
				})
			},
		},
		{
			name: "should process and add/remove tuples to the authorization model",
			store: &corev1alpha1.AuthorizationModel{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						corev1alpha1.StoreRefLabelKey: "store",
					},
				},
				Spec: corev1alpha1.AuthorizationModelSpec{
					StoreRef: corev1alpha1.WorkspaceStoreRef{
						Name:    "store",
						Cluster: "store-cluster",
					},
					Tuples: []corev1alpha1.Tuple{
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user1",
						},
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user2",
						},
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user3",
						},
					},
				},
				Status: corev1alpha1.AuthorizationModelStatus{
					ManagedTuples: []corev1alpha1.Tuple{
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user4",
						},
					},
				},
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				// Apply (batch write) + Delete (batch delete)
				fga.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, nil).Twice()
			},
			k8sMocks: func(k8s *mocks.MockClient) {
				// Not used for AuthorizationModel
			},
			mgrMocks: func(mgr *mocks.MockManager) {
				storeCluster := mocks.NewMockCluster(t)
				storeClient := mocks.NewMockClient(t)
				mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("store-cluster")).Return(storeCluster, nil)
				storeCluster.EXPECT().GetClient().Return(storeClient)
				storeClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					store := o.(*corev1alpha1.Store)
					*store = corev1alpha1.Store{
						Status: corev1alpha1.StoreStatus{
							StoreID:              "store-id",
							AuthorizationModelID: "auth-model-id",
						},
					}
					return nil
				})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fga := mocks.NewMockOpenFGAServiceClient(t)
			if test.fgaMocks != nil {
				test.fgaMocks(fga)
			}

			manager := mocks.NewMockManager(t)
			if test.mgrMocks != nil {
				test.mgrMocks(manager)
			}
			if test.k8sMocks != nil {
				test.k8sMocks(mocks.NewMockClient(t))
			}

			subroutine := subroutine.NewTupleSubroutine(fga, manager)

			ctx := context.Background()

			_, err := subroutine.Process(ctx, test.store)
			if test.expectError {
				assert.Error(t, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, test.store.Status.ManagedTuples, test.store.Spec.Tuples)
			}

		})
	}
}

func TestTupleFinalizationWithAuthorizationModel(t *testing.T) {
	tests := []struct {
		name        string
		store       *corev1alpha1.AuthorizationModel
		fgaMocks    func(*mocks.MockOpenFGAServiceClient)
		k8sMocks    func(*mocks.MockClient)
		mgrMocks    func(*mocks.MockManager)
		expectError bool
	}{
		{
			name: "should finalize the authorization model",
			store: &corev1alpha1.AuthorizationModel{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						corev1alpha1.StoreRefLabelKey: "store",
					},
				},
				Spec: corev1alpha1.AuthorizationModelSpec{
					StoreRef: corev1alpha1.WorkspaceStoreRef{
						Name:    "store",
						Cluster: "store-cluster",
					},
				},
				Status: corev1alpha1.AuthorizationModelStatus{
					ManagedTuples: []corev1alpha1.Tuple{
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user4",
						},
					},
				},
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				// delete call
				fga.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, nil)
			},
			k8sMocks: func(k8s *mocks.MockClient) {
				// Not used for AuthorizationModel
			},
			mgrMocks: func(mgr *mocks.MockManager) {
				storeCluster := mocks.NewMockCluster(t)
				storeClient := mocks.NewMockClient(t)
				mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("store-cluster")).Return(storeCluster, nil)
				storeCluster.EXPECT().GetClient().Return(storeClient)
				storeClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					store := o.(*corev1alpha1.Store)
					*store = corev1alpha1.Store{
						Status: corev1alpha1.StoreStatus{
							StoreID:              "store-id",
							AuthorizationModelID: "auth-model-id",
						},
					}
					return nil
				})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fga := mocks.NewMockOpenFGAServiceClient(t)
			if test.fgaMocks != nil {
				test.fgaMocks(fga)
			}

			manager := mocks.NewMockManager(t)
			if test.mgrMocks != nil {
				test.mgrMocks(manager)
			}
			if test.k8sMocks != nil {
				test.k8sMocks(mocks.NewMockClient(t))
			}

			subroutine := subroutine.NewTupleSubroutine(fga, manager)

			ctx := context.Background()

			_, err := subroutine.Finalize(ctx, test.store)
			if test.expectError {
				assert.Error(t, err)
			} else {
				assert.Nil(t, err)
				assert.Empty(t, test.store.Status.ManagedTuples)
			}

		})
	}
}

func TestTupleFinalizationWithStore(t *testing.T) {
	tests := []struct {
		name        string
		store       *corev1alpha1.Store
		fgaMocks    func(*mocks.MockOpenFGAServiceClient)
		k8sMocks    func(*mocks.MockClient)
		mgrMocks    func(*mocks.MockManager)
		expectError bool
	}{
		{
			name: "should finalize the authorization model",
			store: &corev1alpha1.Store{
				Status: corev1alpha1.StoreStatus{
					ManagedTuples: []corev1alpha1.Tuple{
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user4",
						},
					},
				},
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				// delete call
				fga.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, nil)
			},
		},
		{
			name: "should stop finalizing the authorization model if an error occurs",
			store: &corev1alpha1.Store{
				Status: corev1alpha1.StoreStatus{
					ManagedTuples: []corev1alpha1.Tuple{
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user4",
						},
					},
				},
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				// delete call
				fga.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, errors.New("an error"))
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
			if test.mgrMocks != nil {
				test.mgrMocks(manager)
			}

			subroutine := subroutine.NewTupleSubroutine(fga, manager)

			_, err := subroutine.Finalize(context.Background(), test.store)
			if test.expectError {
				assert.Error(t, err)
			} else {
				assert.Nil(t, err)
				assert.Empty(t, test.store.Status.ManagedTuples)
			}

		})
	}
}
