package subroutine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/platform-mesh/security-operator/api/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/subroutine"
	"github.com/platform-mesh/security-operator/internal/subroutine/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/kontext"
)

func TestTupleGetName(t *testing.T) {
	subroutine := subroutine.NewTupleSubroutine(nil, nil, nil)
	assert.Equal(t, "TupleSubroutine", subroutine.GetName())
}

func TestTupleFinalizers(t *testing.T) {
	subroutine := subroutine.NewTupleSubroutine(nil, nil, nil)
	assert.Equal(t, []string{"core.platform-mesh.io/fga-tuples"}, subroutine.Finalizers())
}

func TestTupleProcessWithStore(t *testing.T) {
	tests := []struct {
		name        string
		store       *v1alpha1.Store
		fgaMocks    func(*mocks.MockOpenFGAServiceClient)
		k8sMocks    func(*mocks.MockClient)
		expectError bool
	}{
		{
			name: "should process and add tuples to the store",
			store: &v1alpha1.Store{
				Spec: v1alpha1.StoreSpec{
					Tuples: []v1alpha1.Tuple{
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
				Status: v1alpha1.StoreStatus{
					StoreID:              "store-id",
					AuthorizationModelID: "auth-model-id",
					ManagedTuples: []v1alpha1.Tuple{
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user1",
						},
					},
				},
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, nil).Times(3)
			},
		},
		{
			name: "should process and add/remove tuples to the store",
			store: &v1alpha1.Store{
				Spec: v1alpha1.StoreSpec{
					Tuples: []v1alpha1.Tuple{
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
				Status: v1alpha1.StoreStatus{
					StoreID:              "store-id",
					AuthorizationModelID: "auth-model-id",
					ManagedTuples: []v1alpha1.Tuple{
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user4",
						},
					},
				},
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				// write calls
				fga.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, nil).Times(3)

				// delete call
				fga.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, nil)
			},
		},
		{
			name: "should stop processing if an error occurs",
			store: &v1alpha1.Store{
				Spec: v1alpha1.StoreSpec{
					Tuples: []v1alpha1.Tuple{
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
				Status: v1alpha1.StoreStatus{
					StoreID:              "store-id",
					AuthorizationModelID: "auth-model-id",
					ManagedTuples: []v1alpha1.Tuple{
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

			k8s := mocks.NewMockClient(t)
			if test.k8sMocks != nil {
				test.k8sMocks(k8s)
			}

			subroutine := subroutine.NewTupleSubroutine(fga, k8s, nil)

			_, err := subroutine.Process(context.Background(), test.store)
			if test.expectError {
				assert.Error(t, err.Err())
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
		store       *v1alpha1.AuthorizationModel
		fgaMocks    func(*mocks.MockOpenFGAServiceClient)
		k8sMocks    func(*mocks.MockClient)
		expectError bool
	}{
		{
			name: "should process and add tuples to the authorization model",
			store: &v1alpha1.AuthorizationModel{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						v1alpha1.StoreRefLabelKey: "store",
					},
				},
				Spec: v1alpha1.AuthorizationModelSpec{
					Tuples: []v1alpha1.Tuple{
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
				fga.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, nil).Times(3)
			},
			k8sMocks: func(k8s *mocks.MockClient) {
				mockLogicalClusterGet(k8s)
				k8s.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					store := o.(*v1alpha1.Store)
					*store = v1alpha1.Store{
						Status: v1alpha1.StoreStatus{
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
			store: &v1alpha1.AuthorizationModel{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						v1alpha1.StoreRefLabelKey: "store",
					},
				},
				Spec: v1alpha1.AuthorizationModelSpec{
					Tuples: []v1alpha1.Tuple{
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
				Status: v1alpha1.AuthorizationModelStatus{
					ManagedTuples: []v1alpha1.Tuple{
						{
							Object:   "foo",
							Relation: "bar",
							User:     "user4",
						},
					},
				},
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				// write calls
				fga.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, nil).Times(3)

				// delete call
				fga.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, nil)
			},
			k8sMocks: func(k8s *mocks.MockClient) {
				mockLogicalClusterGet(k8s)
				k8s.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					store := o.(*v1alpha1.Store)
					*store = v1alpha1.Store{
						Status: v1alpha1.StoreStatus{
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

			k8s := mocks.NewMockClient(t)
			if test.k8sMocks != nil {
				test.k8sMocks(k8s)
			}

			subroutine := subroutine.NewTupleSubroutine(fga, k8s, func(clusterKey logicalcluster.Name) (client.Client, error) {
				return k8s, nil
			})

			ctx := kontext.WithCluster(context.Background(), logicalcluster.Name("a"))

			_, err := subroutine.Process(ctx, test.store)
			if test.expectError {
				assert.Error(t, err.Err())
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
		store       *v1alpha1.AuthorizationModel
		fgaMocks    func(*mocks.MockOpenFGAServiceClient)
		k8sMocks    func(*mocks.MockClient)
		expectError bool
	}{
		{
			name: "should finalize the authorization model",
			store: &v1alpha1.AuthorizationModel{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						v1alpha1.StoreRefLabelKey: "store",
					},
				},
				Status: v1alpha1.AuthorizationModelStatus{
					ManagedTuples: []v1alpha1.Tuple{
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
				mockLogicalClusterGet(k8s)
				k8s.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					store := o.(*v1alpha1.Store)
					*store = v1alpha1.Store{
						Status: v1alpha1.StoreStatus{
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

			k8s := mocks.NewMockClient(t)
			if test.k8sMocks != nil {
				test.k8sMocks(k8s)
			}

			subroutine := subroutine.NewTupleSubroutine(fga, k8s, func(clusterKey logicalcluster.Name) (client.Client, error) {
				return k8s, nil
			})

			ctx := kontext.WithCluster(context.Background(), logicalcluster.Name("a"))

			_, err := subroutine.Finalize(ctx, test.store)
			if test.expectError {
				assert.Error(t, err.Err())
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
		store       *v1alpha1.Store
		fgaMocks    func(*mocks.MockOpenFGAServiceClient)
		k8sMocks    func(*mocks.MockClient)
		expectError bool
	}{
		{
			name: "should finalize the authorization model",
			store: &v1alpha1.Store{
				Status: v1alpha1.StoreStatus{
					ManagedTuples: []v1alpha1.Tuple{
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
			store: &v1alpha1.Store{
				Status: v1alpha1.StoreStatus{
					ManagedTuples: []v1alpha1.Tuple{
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

			k8s := mocks.NewMockClient(t)
			if test.k8sMocks != nil {
				test.k8sMocks(k8s)
			}

			subroutine := subroutine.NewTupleSubroutine(fga, k8s, nil)

			_, err := subroutine.Finalize(context.Background(), test.store)
			if test.expectError {
				assert.Error(t, err.Err())
			} else {
				assert.Nil(t, err)
				assert.Empty(t, test.store.Status.ManagedTuples)
			}

		})
	}
}
