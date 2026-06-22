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

package client

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jellydator/ttlcache/v3"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"go.platform-mesh.io/golang-commons/directive/mocks"
)

func TestOpenFGAClient_ModelId(t *testing.T) {
	tenantId := "tenant123"
	storeId := "store123"
	modelId := "model123"

	tests := []struct {
		name            string
		setupMock       func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient)
		expectedModelId string
		expectedErr     error
	}{
		{
			name: "ListStores_OK_ReadAuthorizationModels_OK",
			setupMock: func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
				openFGAServiceClientMock.EXPECT().
					ListStores(ctx, &openfgav1.ListStoresRequest{}).
					Return(&openfgav1.ListStoresResponse{
						Stores: []*openfgav1.Store{
							{Id: storeId, Name: fmt.Sprintf("tenant-%s", tenantId)},
						}}, nil).
					Once()

				openFGAServiceClientMock.EXPECT().
					ReadAuthorizationModels(ctx, &openfgav1.ReadAuthorizationModelsRequest{StoreId: storeId}).
					Return(&openfgav1.ReadAuthorizationModelsResponse{
						AuthorizationModels: []*openfgav1.AuthorizationModel{{Id: modelId}}}, nil).
					Once()
			},
			expectedModelId: modelId,
		},
		{
			name: "HitModelIdCache_OK",
			setupMock: func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
				client.cache.Set(cacheKeyForModel(tenantId), modelId, ttlcache.DefaultTTL)
			},
			expectedModelId: modelId,
		},
		{
			name: "HitStoreIdCache_OK",
			setupMock: func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
				client.cache.Set(cacheKeyForStore(tenantId), storeId, ttlcache.DefaultTTL)

				openFGAServiceClientMock.EXPECT().
					ReadAuthorizationModels(ctx, &openfgav1.ReadAuthorizationModelsRequest{StoreId: storeId}).
					Return(&openfgav1.ReadAuthorizationModelsResponse{
						AuthorizationModels: []*openfgav1.AuthorizationModel{{Id: modelId}}}, nil).
					Once()
			},
			expectedModelId: modelId,
		},
		{
			name: "ListStores_Error",
			setupMock: func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
				openFGAServiceClientMock.EXPECT().
					ListStores(ctx, &openfgav1.ListStoresRequest{}).
					Return(nil, assert.AnError).
					Once()
			},
			expectedErr: assert.AnError,
		},
		{
			name: "ReadAuthorizationModels_Error",
			setupMock: func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
				client.cache.Set(cacheKeyForStore(tenantId), storeId, ttlcache.DefaultTTL)

				openFGAServiceClientMock.EXPECT().
					ReadAuthorizationModels(ctx, &openfgav1.ReadAuthorizationModelsRequest{StoreId: storeId}).
					Return(nil, assert.AnError).
					Once()
			},
			expectedErr: assert.AnError,
		},
		{
			name: "modelIdNotFound_Error",
			setupMock: func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
				openFGAServiceClientMock.EXPECT().
					ListStores(ctx, &openfgav1.ListStoresRequest{}).
					Return(&openfgav1.ListStoresResponse{
						Stores: []*openfgav1.Store{{Id: storeId, Name: fmt.Sprintf("tenant-%s", tenantId)}}}, nil).
					Once()

				openFGAServiceClientMock.EXPECT().
					ReadAuthorizationModels(ctx, &openfgav1.ReadAuthorizationModelsRequest{StoreId: storeId}).
					Return(&openfgav1.ReadAuthorizationModelsResponse{}, nil).
					Once()
			},
			expectedErr: errors.New("could not determine model. No models found"),
		},
		{
			name: "NoStoreIdFound_Error",
			setupMock: func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
				openFGAServiceClientMock.EXPECT().
					ListStores(ctx, &openfgav1.ListStoresRequest{}).
					Return(&openfgav1.ListStoresResponse{}, nil).
					Once()
			},
			expectedErr: errors.New("could not determine store. No stores found"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			openFGAServiceClientMock := &mocks.OpenFGAServiceClient{}

			client, err := NewOpenFGAClient(openFGAServiceClientMock)
			assert.NoError(t, err)

			if tt.setupMock != nil {
				tt.setupMock(ctx, client, openFGAServiceClientMock)
			}

			res, err := client.ModelId(ctx, tenantId)
			assert.Equal(t, tt.expectedModelId, res)
			assert.Equal(t, tt.expectedErr, err)

			openFGAServiceClientMock.AssertExpectations(t)
		})
	}
}
