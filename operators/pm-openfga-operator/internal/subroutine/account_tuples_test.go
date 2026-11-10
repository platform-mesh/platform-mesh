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
	"fmt"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/security-operator/internal/fga"
	"go.platform-mesh.io/security-operator/internal/subroutine"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

const (
	accountLCPath   = "root:orgs:myorg:myaccount"
	parentClusterID = "parent-cluster-id"
)

func newAccountLogicalCluster() *kcpcorev1alpha1.LogicalCluster {
	return &kcpcorev1alpha1.LogicalCluster{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"kcp.io/path": accountLCPath,
			},
		},
	}
}

func mockParentLogicalCluster(kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient) {
	kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, mock.Anything).Return(parentClient, nil).Once()
	parentClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "cluster"}, mock.Anything).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			if lc, ok := o.(*kcpcorev1alpha1.LogicalCluster); ok {
				lc.Annotations = map[string]string{"kcp.io/cluster": parentClusterID}
				lc.Spec.Owner = &kcpcorev1alpha1.LogicalClusterOwner{Cluster: "grand-cluster-id"}
			}
			return nil
		}).Once()
}

func TestAccountTuplesSubroutine_GetName(t *testing.T) {
	sub := subroutine.NewAccountTuplesSubroutine(nil, nil, nil, "creator", "parent", "type", nil)
	assert.Equal(t, "AccountTuplesSubroutine", sub.GetName())
}

func TestAccountTuplesSubroutine_Process(t *testing.T) {
	storeIDGetter := mocks.NewMockStoreIDGetter(t)
	kcpHelper := mocks.NewMockKCPClientGetter(t)
	parentClient := mocks.NewMockClient(t)
	fgaClient := mocks.NewMockOpenFGAServiceClient(t)

	storeIDGetter.EXPECT().Get(mock.Anything, "myorg").Return("store-id", nil)
	mockParentLogicalCluster(kcpHelper, parentClient)
	kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, mock.Anything).Return(parentClient, nil).Once()
	creator := "user@example.com"
	parentClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "myaccount"}, mock.Anything).
		RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			if acc, ok := o.(*pmcorev1alpha1.Account); ok {
				acc.Spec.Creator = &creator
			}
			return nil
		}).Once()

	expectedTuples, err := fga.InitialTuplesForAccount(fga.InitialTuplesForAccountInput{
		BaseTuplesInput: fga.BaseTuplesInput{
			Creator:                creator,
			AccountOriginClusterID: parentClusterID,
			AccountName:            "myaccount",
			CreatorRelation:        "creator",
			ObjectType:             "account",
		},
		ParentOriginClusterID: "grand-cluster-id",
		ParentName:            "myorg",
		ParentRelation:        "parent",
	})
	require.NoError(t, err)

	fgaClient.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
		if req.StoreId != "store-id" || req.AuthorizationModelId != fga.AuthorizationModelIDLatest {
			return false
		}
		if req.Writes == nil || req.Deletes != nil {
			return false
		}
		if req.Writes.OnDuplicate != "ignore" || len(req.Writes.TupleKeys) != len(expectedTuples) {
			return false
		}
		for i, tk := range req.Writes.TupleKeys {
			if tk.User != expectedTuples[i].User || tk.Relation != expectedTuples[i].Relation || tk.Object != expectedTuples[i].Object {
				return false
			}
		}
		return true
	})).Return(&openfgav1.WriteResponse{}, nil).Once()

	sub := subroutine.NewAccountTuplesSubroutine(nil, fgaClient, storeIDGetter, "creator", "parent", "account", kcpHelper)
	_, err = sub.Process(context.Background(), newAccountLogicalCluster())
	assert.NoError(t, err)
}

func TestAccountTuplesSubroutine_Initialize(t *testing.T) {
	tests := []struct {
		name        string
		obj         *kcpcorev1alpha1.LogicalCluster
		mockSetup   func(*mocks.MockStoreIDGetter, *mocks.MockKCPClientGetter, *mocks.MockClient, *mocks.MockOpenFGAServiceClient)
		expectError bool
	}{
		{
			name: "error: missing path annotation",
			obj:  &kcpcorev1alpha1.LogicalCluster{},
			mockSetup: func(storeIDGetter *mocks.MockStoreIDGetter, kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient, fgaClient *mocks.MockOpenFGAServiceClient) {
			},
			expectError: true,
		},
		{
			name: "error: storeIDGetter fails",
			obj:  newAccountLogicalCluster(),
			mockSetup: func(storeIDGetter *mocks.MockStoreIDGetter, kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient, fgaClient *mocks.MockOpenFGAServiceClient) {
				storeIDGetter.EXPECT().Get(mock.Anything, "myorg").Return("", assert.AnError)
			},
			expectError: true,
		},
		{
			name: "error: NewForLogicalCluster fails for parent cluster",
			obj:  newAccountLogicalCluster(),
			mockSetup: func(storeIDGetter *mocks.MockStoreIDGetter, kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient, fgaClient *mocks.MockOpenFGAServiceClient) {
				storeIDGetter.EXPECT().Get(mock.Anything, "myorg").Return("store-id", nil)
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
		{
			name: "error: Get LogicalCluster fails",
			obj:  newAccountLogicalCluster(),
			mockSetup: func(storeIDGetter *mocks.MockStoreIDGetter, kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient, fgaClient *mocks.MockOpenFGAServiceClient) {
				storeIDGetter.EXPECT().Get(mock.Anything, "myorg").Return("store-id", nil)
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, mock.Anything).Return(parentClient, nil).Once()
				parentClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "cluster"}, mock.Anything).Return(assert.AnError).Once()
			},
			expectError: true,
		},
		{
			name: "error: kcp.io/cluster annotation missing on parent LogicalCluster",
			obj:  newAccountLogicalCluster(),
			mockSetup: func(storeIDGetter *mocks.MockStoreIDGetter, kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient, fgaClient *mocks.MockOpenFGAServiceClient) {
				storeIDGetter.EXPECT().Get(mock.Anything, "myorg").Return("store-id", nil)
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, mock.Anything).Return(parentClient, nil).Once()
				parentClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "cluster"}, mock.Anything).Return(nil).Once()
			},
			expectError: true,
		},
		{
			name: "error: NewForLogicalCluster fails for parent account client",
			obj:  newAccountLogicalCluster(),
			mockSetup: func(storeIDGetter *mocks.MockStoreIDGetter, kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient, fgaClient *mocks.MockOpenFGAServiceClient) {
				storeIDGetter.EXPECT().Get(mock.Anything, "myorg").Return("store-id", nil)
				mockParentLogicalCluster(kcpHelper, parentClient)
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
		{
			name: "error: Get Account fails",
			obj:  newAccountLogicalCluster(),
			mockSetup: func(storeIDGetter *mocks.MockStoreIDGetter, kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient, fgaClient *mocks.MockOpenFGAServiceClient) {
				storeIDGetter.EXPECT().Get(mock.Anything, "myorg").Return("store-id", nil)
				mockParentLogicalCluster(kcpHelper, parentClient)
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, mock.Anything).Return(parentClient, nil).Once()
				parentClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "myaccount"}, mock.Anything).Return(assert.AnError).Once()
			},
			expectError: true,
		},
		{
			name: "error: account creator is empty",
			obj:  newAccountLogicalCluster(),
			mockSetup: func(storeIDGetter *mocks.MockStoreIDGetter, kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient, fgaClient *mocks.MockOpenFGAServiceClient) {
				storeIDGetter.EXPECT().Get(mock.Anything, "myorg").Return("store-id", nil)
				mockParentLogicalCluster(kcpHelper, parentClient)
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, mock.Anything).Return(parentClient, nil).Once()
				parentClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "myaccount"}, mock.Anything).Return(nil).Once()
			},
			expectError: true,
		},
		{
			name: "error: fga.Write fails",
			obj:  newAccountLogicalCluster(),
			mockSetup: func(storeIDGetter *mocks.MockStoreIDGetter, kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient, fgaClient *mocks.MockOpenFGAServiceClient) {
				storeIDGetter.EXPECT().Get(mock.Anything, "myorg").Return("store-id", nil)
				mockParentLogicalCluster(kcpHelper, parentClient)
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, mock.Anything).Return(parentClient, nil).Once()
				creator := "user@example.com"
				parentClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "myaccount"}, mock.Anything).
					RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						if acc, ok := o.(*pmcorev1alpha1.Account); ok {
							acc.Spec.Creator = &creator
						}
						return nil
					}).Once()
				fgaClient.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, assert.AnError)
			},
			expectError: true,
		},
		{
			name: "success",
			obj:  newAccountLogicalCluster(),
			mockSetup: func(storeIDGetter *mocks.MockStoreIDGetter, kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient, fgaClient *mocks.MockOpenFGAServiceClient) {
				storeIDGetter.EXPECT().Get(mock.Anything, "myorg").Return("store-id", nil)
				mockParentLogicalCluster(kcpHelper, parentClient)
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, mock.Anything).Return(parentClient, nil).Once()
				creator := "user@example.com"
				parentClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "myaccount"}, mock.Anything).
					RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						if acc, ok := o.(*pmcorev1alpha1.Account); ok {
							acc.Spec.Creator = &creator
						}
						return nil
					}).Once()
				fgaClient.EXPECT().Write(mock.Anything, mock.Anything).Return(&openfgav1.WriteResponse{}, nil)
			},
			expectError: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			storeIDGetter := mocks.NewMockStoreIDGetter(t)
			kcpHelper := mocks.NewMockKCPClientGetter(t)
			parentClient := mocks.NewMockClient(t)
			fgaClient := mocks.NewMockOpenFGAServiceClient(t)

			test.mockSetup(storeIDGetter, kcpHelper, parentClient, fgaClient)

			sub := subroutine.NewAccountTuplesSubroutine(nil, fgaClient, storeIDGetter, "creator", "parent", "account", kcpHelper)
			_, err := sub.Initialize(context.Background(), test.obj)
			if test.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAccountTuplesSubroutine_Terminate(t *testing.T) {
	tests := []struct {
		name        string
		obj         *kcpcorev1alpha1.LogicalCluster
		mockSetup   func(*mocks.MockStoreIDGetter, *mocks.MockKCPClientGetter, *mocks.MockClient, *mocks.MockOpenFGAServiceClient)
		expectError bool
	}{
		{
			name: "error: missing path annotation",
			obj:  &kcpcorev1alpha1.LogicalCluster{},
			mockSetup: func(storeIDGetter *mocks.MockStoreIDGetter, kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient, fgaClient *mocks.MockOpenFGAServiceClient) {
			},
			expectError: true,
		},
		{
			name: "error: NewForLogicalCluster fails",
			obj:  newAccountLogicalCluster(),
			mockSetup: func(storeIDGetter *mocks.MockStoreIDGetter, kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient, fgaClient *mocks.MockOpenFGAServiceClient) {
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
		{
			name: "error: storeIDGetter fails",
			obj:  newAccountLogicalCluster(),
			mockSetup: func(storeIDGetter *mocks.MockStoreIDGetter, kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient, fgaClient *mocks.MockOpenFGAServiceClient) {
				mockParentLogicalCluster(kcpHelper, parentClient)
				storeIDGetter.EXPECT().Get(mock.Anything, "myorg").Return("", assert.AnError)
			},
			expectError: true,
		},
		{
			name: "success: org store already deleted",
			obj:  newAccountLogicalCluster(),
			mockSetup: func(storeIDGetter *mocks.MockStoreIDGetter, kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient, fgaClient *mocks.MockOpenFGAServiceClient) {
				mockParentLogicalCluster(kcpHelper, parentClient)
				storeIDGetter.EXPECT().Get(mock.Anything, "myorg").Return("", fmt.Errorf("store %q not found: %w", "myorg", fga.ErrStoreNotFound))
			},
			expectError: false,
		},
		{
			name: "success: stale store ID returns store_id_not_found on Read",
			obj:  newAccountLogicalCluster(),
			mockSetup: func(storeIDGetter *mocks.MockStoreIDGetter, kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient, fgaClient *mocks.MockOpenFGAServiceClient) {
				mockParentLogicalCluster(kcpHelper, parentClient)
				storeIDGetter.EXPECT().Get(mock.Anything, "myorg").Return("store-id", nil)
				fgaClient.EXPECT().Read(mock.Anything, mock.Anything).Return(nil, status.Error(codes.Code(openfgav1.NotFoundErrorCode_store_id_not_found), "not found")).Once()
			},
			expectError: false,
		},
		{
			name: "error: ListWithKey fails",
			obj:  newAccountLogicalCluster(),
			mockSetup: func(storeIDGetter *mocks.MockStoreIDGetter, kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient, fgaClient *mocks.MockOpenFGAServiceClient) {
				mockParentLogicalCluster(kcpHelper, parentClient)
				storeIDGetter.EXPECT().Get(mock.Anything, "myorg").Return("store-id", nil)
				fgaClient.EXPECT().Read(mock.Anything, mock.Anything).Return(nil, assert.AnError)
			},
			expectError: true,
		},
		{
			name: "error: ListWithKey for role fails",
			obj:  newAccountLogicalCluster(),
			mockSetup: func(storeIDGetter *mocks.MockStoreIDGetter, kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient, fgaClient *mocks.MockOpenFGAServiceClient) {
				mockParentLogicalCluster(kcpHelper, parentClient)
				storeIDGetter.EXPECT().Get(mock.Anything, "myorg").Return("store-id", nil)
				// First Read returns a tuple whose User has the role prefix for this account.
				roleUser := "role:account/" + parentClusterID + "/myaccount/owner#assignee"
				fgaClient.EXPECT().Read(mock.Anything, mock.Anything).
					Return(&openfgav1.ReadResponse{
						Tuples: []*openfgav1.Tuple{
							{Key: &openfgav1.TupleKey{Object: "account:" + parentClusterID + "/myaccount", Relation: "member", User: roleUser}},
						},
					}, nil).Once()
				// Second Read (for role references) fails.
				fgaClient.EXPECT().Read(mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()
			},
			expectError: true,
		},
		{
			name: "error: Delete fails",
			obj:  newAccountLogicalCluster(),
			mockSetup: func(storeIDGetter *mocks.MockStoreIDGetter, kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient, fgaClient *mocks.MockOpenFGAServiceClient) {
				mockParentLogicalCluster(kcpHelper, parentClient)
				storeIDGetter.EXPECT().Get(mock.Anything, "myorg").Return("store-id", nil)
				fgaClient.EXPECT().Read(mock.Anything, mock.Anything).
					Return(&openfgav1.ReadResponse{
						Tuples: []*openfgav1.Tuple{
							{Key: &openfgav1.TupleKey{Object: "account:" + parentClusterID + "/myaccount", Relation: "member", User: "user:someone"}},
						},
					}, nil).Once()
				fgaClient.EXPECT().Write(mock.Anything, mock.Anything).Return(nil, assert.AnError)
			},
			expectError: true,
		},
		{
			name: "success: no tuples to delete",
			obj:  newAccountLogicalCluster(),
			mockSetup: func(storeIDGetter *mocks.MockStoreIDGetter, kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient, fgaClient *mocks.MockOpenFGAServiceClient) {
				mockParentLogicalCluster(kcpHelper, parentClient)
				storeIDGetter.EXPECT().Get(mock.Anything, "myorg").Return("store-id", nil)
				fgaClient.EXPECT().Read(mock.Anything, mock.Anything).
					Return(&openfgav1.ReadResponse{Tuples: []*openfgav1.Tuple{}}, nil).Once()
			},
			expectError: false,
		},
		{
			name: "success: tuples with role prefix deleted",
			obj:  newAccountLogicalCluster(),
			mockSetup: func(storeIDGetter *mocks.MockStoreIDGetter, kcpHelper *mocks.MockKCPClientGetter, parentClient *mocks.MockClient, fgaClient *mocks.MockOpenFGAServiceClient) {
				mockParentLogicalCluster(kcpHelper, parentClient)
				storeIDGetter.EXPECT().Get(mock.Anything, "myorg").Return("store-id", nil)
				roleUser := "role:account/" + parentClusterID + "/myaccount/owner#assignee"
				fgaClient.EXPECT().Read(mock.Anything, mock.Anything).
					Return(&openfgav1.ReadResponse{
						Tuples: []*openfgav1.Tuple{
							{Key: &openfgav1.TupleKey{Object: "account:" + parentClusterID + "/myaccount", Relation: "member", User: roleUser}},
						},
					}, nil).Once()
				// Role references lookup returns no results.
				fgaClient.EXPECT().Read(mock.Anything, mock.Anything).
					Return(&openfgav1.ReadResponse{Tuples: []*openfgav1.Tuple{}}, nil).Once()
				fgaClient.EXPECT().Write(mock.Anything, mock.Anything).Return(&openfgav1.WriteResponse{}, nil)
			},
			expectError: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			storeIDGetter := mocks.NewMockStoreIDGetter(t)
			kcpHelper := mocks.NewMockKCPClientGetter(t)
			parentClient := mocks.NewMockClient(t)
			fgaClient := mocks.NewMockOpenFGAServiceClient(t)

			test.mockSetup(storeIDGetter, kcpHelper, parentClient, fgaClient)

			sub := subroutine.NewAccountTuplesSubroutine(nil, fgaClient, storeIDGetter, "creator", "parent", "account", kcpHelper)
			_, err := sub.Terminate(context.Background(), test.obj)
			if test.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
