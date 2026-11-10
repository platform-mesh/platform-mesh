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

package subroutine

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

func TestAuthorizationModelFinalizer(t *testing.T) {
	assert.True(t, strings.HasPrefix(authorizationModelFinalizer("cluster1", "model1"), authorizationModelFinalizerPrefix))
	assert.Equal(t, authorizationModelFinalizer("cluster1", "model1"), authorizationModelFinalizer("cluster1", "model1"), "must be deterministic")
	assert.NotEqual(t, authorizationModelFinalizer("cluster1", "model1"), authorizationModelFinalizer("cluster1", "model2"), "must differ for different models")
	assert.NotEqual(t, authorizationModelFinalizer("cluster1", "model1"), authorizationModelFinalizer("cluster2", "model1"), "must differ for different clusters")

	// verify that the finalizer name isn't too long
	longCluster := strings.Repeat("c", 100)
	longModel := "process-test-example-com-testresources-generator-test-process"
	segment := strings.TrimPrefix(authorizationModelFinalizer(longCluster, longModel), authorizationModelFinalizerPrefix)
	assert.LessOrEqual(t, len(segment), 63)
}

func TestRegisterAuthorizationModelWithStore(t *testing.T) {
	tests := []struct {
		name        string
		mockSetup   func(*mocks.MockManager, *mocks.MockCluster, *mocks.MockClient)
		expectError bool
	}{
		{
			name: "error getting store cluster",
			mockSetup: func(mgr *mocks.MockManager, cl *mocks.MockCluster, kc *mocks.MockClient) {
				mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("store-cluster")).Return(nil, assert.AnError)
			},
			expectError: true,
		},
		{
			name: "error getting store (e.g. NotFound because the Store or its org is terminating)",
			mockSetup: func(mgr *mocks.MockManager, cl *mocks.MockCluster, kc *mocks.MockClient) {
				mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("store-cluster")).Return(cl, nil)
				cl.EXPECT().GetClient().Return(kc)
				kc.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(
					apierrors.NewNotFound(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "stores"}, "store"))
			},
			expectError: true,
		},
		{
			name: "adds the finalizer when not already present",
			mockSetup: func(mgr *mocks.MockManager, cl *mocks.MockCluster, kc *mocks.MockClient) {
				mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("store-cluster")).Return(cl, nil)
				cl.EXPECT().GetClient().Return(kc)
				kc.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "store"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					*o.(*pmcorev1alpha1.Store) = pmcorev1alpha1.Store{}
					return nil
				})
				kc.EXPECT().Update(mock.Anything, mock.MatchedBy(func(o *pmcorev1alpha1.Store) bool {
					return len(o.Finalizers) == 1 && o.Finalizers[0] == authorizationModelFinalizer("model-cluster", "model")
				})).Return(nil)
			},
		},
		{
			name: "no-op when the finalizer is already present",
			mockSetup: func(mgr *mocks.MockManager, cl *mocks.MockCluster, kc *mocks.MockClient) {
				mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("store-cluster")).Return(cl, nil)
				cl.EXPECT().GetClient().Return(kc)
				kc.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "store"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					*o.(*pmcorev1alpha1.Store) = pmcorev1alpha1.Store{
						ObjectMeta: metav1.ObjectMeta{Finalizers: []string{authorizationModelFinalizer("model-cluster", "model")}},
					}
					return nil
				})
				// No Update expected.
			},
		},
		{
			name: "propagates the rejection when the Store is already being deleted",
			mockSetup: func(mgr *mocks.MockManager, cl *mocks.MockCluster, kc *mocks.MockClient) {
				mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("store-cluster")).Return(cl, nil)
				cl.EXPECT().GetClient().Return(kc)
				kc.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "store"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					*o.(*pmcorev1alpha1.Store) = pmcorev1alpha1.Store{}
					return nil
				})
				// The API server rejects adding a finalizer once deletionTimestamp is
				// set; simulate that rejection (not a conflict, so no retry).
				kc.EXPECT().Update(mock.Anything, mock.Anything).Return(
					apierrors.NewInvalid(schema.GroupKind{Group: "core.platform-mesh.io", Kind: "Store"}, "store", nil))
			},
			expectError: true,
		},
		{
			name: "retries once on a resourceVersion conflict then succeeds",
			mockSetup: func(mgr *mocks.MockManager, cl *mocks.MockCluster, kc *mocks.MockClient) {
				mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("store-cluster")).Return(cl, nil)
				cl.EXPECT().GetClient().Return(kc)
				kc.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "store"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					*o.(*pmcorev1alpha1.Store) = pmcorev1alpha1.Store{}
					return nil
				}).Twice()
				kc.EXPECT().Update(mock.Anything, mock.Anything).Return(
					apierrors.NewConflict(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "stores"}, "store", assert.AnError)).Once()
				kc.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Once()
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mgr := mocks.NewMockManager(t)
			cl := mocks.NewMockCluster(t)
			kc := mocks.NewMockClient(t)
			test.mockSetup(mgr, cl, kc)

			err := registerAuthorizationModelWithStore(context.Background(), mgr, "store-cluster", "store", "model-cluster", "model")
			if test.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDeregisterAuthorizationModelFromStore(t *testing.T) {
	tests := []struct {
		name        string
		mockSetup   func(*mocks.MockManager, *mocks.MockCluster, *mocks.MockClient)
		expectError bool
	}{
		{
			name: "error getting store cluster",
			mockSetup: func(mgr *mocks.MockManager, cl *mocks.MockCluster, kc *mocks.MockClient) {
				mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("store-cluster")).Return(nil, assert.AnError)
			},
			expectError: true,
		},
		{
			name: "store already gone: nothing to deregister",
			mockSetup: func(mgr *mocks.MockManager, cl *mocks.MockCluster, kc *mocks.MockClient) {
				mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("store-cluster")).Return(cl, nil)
				cl.EXPECT().GetClient().Return(kc)
				kc.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(
					apierrors.NewNotFound(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "stores"}, "store"))
			},
		},
		{
			name: "error getting store (not NotFound)",
			mockSetup: func(mgr *mocks.MockManager, cl *mocks.MockCluster, kc *mocks.MockClient) {
				mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("store-cluster")).Return(cl, nil)
				cl.EXPECT().GetClient().Return(kc)
				kc.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError)
			},
			expectError: true,
		},
		{
			name: "removes the finalizer when present",
			mockSetup: func(mgr *mocks.MockManager, cl *mocks.MockCluster, kc *mocks.MockClient) {
				mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("store-cluster")).Return(cl, nil)
				cl.EXPECT().GetClient().Return(kc)
				kc.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "store"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					*o.(*pmcorev1alpha1.Store) = pmcorev1alpha1.Store{
						ObjectMeta: metav1.ObjectMeta{Finalizers: []string{authorizationModelFinalizer("model-cluster", "model"), "core.platform-mesh.io/fga-store"}},
					}
					return nil
				})
				kc.EXPECT().Update(mock.Anything, mock.MatchedBy(func(o *pmcorev1alpha1.Store) bool {
					return len(o.Finalizers) == 1 && o.Finalizers[0] == "core.platform-mesh.io/fga-store"
				})).Return(nil)
			},
		},
		{
			name: "no-op when the finalizer is not present",
			mockSetup: func(mgr *mocks.MockManager, cl *mocks.MockCluster, kc *mocks.MockClient) {
				mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("store-cluster")).Return(cl, nil)
				cl.EXPECT().GetClient().Return(kc)
				kc.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "store"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					*o.(*pmcorev1alpha1.Store) = pmcorev1alpha1.Store{}
					return nil
				})
				// No Update expected.
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mgr := mocks.NewMockManager(t)
			cl := mocks.NewMockCluster(t)
			kc := mocks.NewMockClient(t)
			test.mockSetup(mgr, cl, kc)

			err := deregisterAuthorizationModelFromStore(context.Background(), mgr, "store-cluster", "store", "model-cluster", "model")
			if test.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
