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

package store_test

import (
	"context"
	"errors"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"

	fgastore "go.platform-mesh.io/golang-commons/fga/store"
	"go.platform-mesh.io/golang-commons/fga/store/mocks"
)

func listStoresReq(name, continuationToken string) *openfgav1.ListStoresRequest {
	return &openfgav1.ListStoresRequest{
		Name:              name,
		PageSize:          wrapperspb.Int32(fgastore.ListStoresPageSize),
		ContinuationToken: continuationToken,
	}
}

func TestGetModelIDForTenant(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant123"
	storeId := "store123"
	modelId := "model123"

	tests := []struct {
		name            string
		setupMock       func(client *mocks.OpenFGAServiceClient, cachedStore *fgastore.FgaTenantStore)
		expectedModelID string
		expectedError   error
	}{
		{
			name: "FullPath_OK",
			setupMock: func(client *mocks.OpenFGAServiceClient, cachedStore *fgastore.FgaTenantStore) {
				client.EXPECT().
					ListStores(ctx, listStoresReq("tenant-tenant123", "")).
					Return(&openfgav1.ListStoresResponse{
						Stores: []*openfgav1.Store{
							{Id: "store123", Name: "tenant-tenant123"},
						}}, nil).
					Once()

				client.EXPECT().
					ReadAuthorizationModels(ctx, &openfgav1.ReadAuthorizationModelsRequest{StoreId: "store123"}).
					Return(&openfgav1.ReadAuthorizationModelsResponse{
						AuthorizationModels: []*openfgav1.AuthorizationModel{
							{Id: modelId},
						}}, nil).
					Once()
			},
			expectedModelID: modelId,
			expectedError:   nil,
		},
		{
			name: "HitGetModelIDForTenantCache_OK",
			setupMock: func(client *mocks.OpenFGAServiceClient, cachedStore *fgastore.FgaTenantStore) {
				cachedStore.GetCache().Add("model-tenant123", modelId)
			},
			expectedModelID: modelId,
			expectedError:   nil,
		},
		{
			name: "HitGetStoreIDForTenantCache_OK",
			setupMock: func(client *mocks.OpenFGAServiceClient, cachedStore *fgastore.FgaTenantStore) {
				cachedStore.GetCache().Add("tenant-tenant123", storeId)

				client.EXPECT().
					ReadAuthorizationModels(ctx, &openfgav1.ReadAuthorizationModelsRequest{StoreId: "store123"}).
					Return(&openfgav1.ReadAuthorizationModelsResponse{
						AuthorizationModels: []*openfgav1.AuthorizationModel{
							{Id: modelId},
						}}, nil).
					Once()
			},
			expectedModelID: modelId,
			expectedError:   nil,
		},
		{
			name: "ListStores_Error",
			setupMock: func(client *mocks.OpenFGAServiceClient, cachedStore *fgastore.FgaTenantStore) {
				client.EXPECT().
					ListStores(ctx, listStoresReq("tenant-tenant123", "")).
					Return(nil, assert.AnError).
					Once()
			},
			expectedError: assert.AnError,
		},
		{
			name: "MatchingKeyNotFound_Error",
			setupMock: func(client *mocks.OpenFGAServiceClient, cachedStore *fgastore.FgaTenantStore) {
				client.EXPECT().
					ListStores(ctx, listStoresReq("tenant-tenant123", "")).
					Return(&openfgav1.ListStoresResponse{Stores: []*openfgav1.Store{}}, nil).
					Once()
			},
			expectedError: errors.New("could not find store matching key \"tenant-tenant123\""),
		},
		{
			name: "ReadAuthorizationModels_Error",
			setupMock: func(client *mocks.OpenFGAServiceClient, cachedStore *fgastore.FgaTenantStore) {
				cachedStore.GetCache().Add("tenant-tenant123", storeId)

				client.EXPECT().
					ReadAuthorizationModels(ctx, &openfgav1.ReadAuthorizationModelsRequest{StoreId: "store123"}).
					Return(nil, assert.AnError).
					Once()
			},
			expectedError: assert.AnError,
		},
		{
			name: "NoReadAuthorizationModels_Error",
			setupMock: func(client *mocks.OpenFGAServiceClient, cachedStore *fgastore.FgaTenantStore) {
				cachedStore.GetCache().Add("tenant-tenant123", storeId)

				client.EXPECT().
					ReadAuthorizationModels(ctx, &openfgav1.ReadAuthorizationModelsRequest{StoreId: "store123"}).
					Return(&openfgav1.ReadAuthorizationModelsResponse{}, nil).
					Once()
			},
			expectedError: errors.New("no authorization models in response. Cannot determine proper AuthorizationModelId"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &mocks.OpenFGAServiceClient{}
			cachedStore := fgastore.NewWithPrefix("tenant-")
			tt.setupMock(client, cachedStore)

			modelID, err := cachedStore.GetModelIDForTenant(ctx, client, tenantID)

			assert.Equal(t, tt.expectedModelID, modelID)
			assert.Equal(t, tt.expectedError, err)

			client.AssertExpectations(t)
		})
	}
}

func TestGetStoreIDForTenant(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant123"
	storeName := "tenant-tenant123"

	tests := []struct {
		name            string
		setupMock       func(client *mocks.OpenFGAServiceClient, cachedStore *fgastore.FgaTenantStore)
		expectedStoreID string
		expectedError   error
	}{
		{
			name: "NameFilterHonored_ResolvesInOneCall",
			setupMock: func(client *mocks.OpenFGAServiceClient, cachedStore *fgastore.FgaTenantStore) {
				client.EXPECT().
					ListStores(ctx, listStoresReq(storeName, "")).
					Return(&openfgav1.ListStoresResponse{
						Stores: []*openfgav1.Store{{Id: "store123", Name: storeName}},
					}, nil).
					Once()
			},
			expectedStoreID: "store123",
		},
		{
			name: "NameFilterIgnored_PaginatedFallbackResolves",
			setupMock: func(client *mocks.OpenFGAServiceClient, cachedStore *fgastore.FgaTenantStore) {
				client.EXPECT().
					ListStores(ctx, listStoresReq(storeName, "")).
					Return(&openfgav1.ListStoresResponse{
						Stores:            []*openfgav1.Store{{Id: "other1", Name: "tenant-other1"}},
						ContinuationToken: "page2",
					}, nil).
					Once()
				client.EXPECT().
					ListStores(ctx, listStoresReq(storeName, "page2")).
					Return(&openfgav1.ListStoresResponse{
						Stores: []*openfgav1.Store{{Id: "store123", Name: storeName}},
					}, nil).
					Once()
			},
			expectedStoreID: "store123",
		},
		{
			name: "NotFoundAcrossAllPages_Error",
			setupMock: func(client *mocks.OpenFGAServiceClient, cachedStore *fgastore.FgaTenantStore) {
				client.EXPECT().
					ListStores(ctx, listStoresReq(storeName, "")).
					Return(&openfgav1.ListStoresResponse{
						Stores:            []*openfgav1.Store{{Id: "other1", Name: "tenant-other1"}},
						ContinuationToken: "page2",
					}, nil).
					Once()
				client.EXPECT().
					ListStores(ctx, listStoresReq(storeName, "page2")).
					Return(&openfgav1.ListStoresResponse{Stores: []*openfgav1.Store{}}, nil).
					Once()
			},
			expectedError: errors.New("could not find store matching key \"tenant-tenant123\""),
		},
		{
			name: "ListStores_Error",
			setupMock: func(client *mocks.OpenFGAServiceClient, cachedStore *fgastore.FgaTenantStore) {
				client.EXPECT().
					ListStores(ctx, listStoresReq(storeName, "")).
					Return(nil, assert.AnError).
					Once()
			},
			expectedError: assert.AnError,
		},
		{
			name: "CacheHit_NoListStores",
			setupMock: func(client *mocks.OpenFGAServiceClient, cachedStore *fgastore.FgaTenantStore) {
				cachedStore.GetCache().Add("tenant-tenant123", "cached-store")
			},
			expectedStoreID: "cached-store",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &mocks.OpenFGAServiceClient{}
			cachedStore := fgastore.NewWithPrefix("tenant-")
			tt.setupMock(client, cachedStore)

			storeID, err := cachedStore.GetStoreIDForTenant(ctx, client, tenantID)

			assert.Equal(t, tt.expectedStoreID, storeID)
			assert.Equal(t, tt.expectedError, err)

			client.AssertExpectations(t)
		})
	}
}

func TestIsDuplicateWriteError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "NoError",
			err:      nil,
			expected: false,
		},
		{
			name:     "NonGRPCError",
			err:      errors.New("some error"),
			expected: false,
		},
		{
			name:     "NonDuplicateWriteGRPCError",
			err:      status.Error(codes.InvalidArgument, "invalid argument"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cachedStore := fgastore.NewWithPrefix("tenant-")
			result := cachedStore.IsDuplicateWriteError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
