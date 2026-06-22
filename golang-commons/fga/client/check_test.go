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
	"testing"

	"github.com/jellydator/ttlcache/v3"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"go.platform-mesh.io/golang-commons/directive/mocks"
)

func TestOpenFGAClient_Check(t *testing.T) {
	tenantId := "tenant123"
	storeId := "store123"
	modelId := "model123"
	object := "object"
	relation := "relation"
	user := "user"

	tests := []struct {
		name        string
		setupMock   func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient)
		expectedErr error
	}{
		{
			name: "Check_OK",
			setupMock: func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
				client.cache.Set(cacheKeyForStore(tenantId), storeId, ttlcache.DefaultTTL)
				client.cache.Set(cacheKeyForModel(tenantId), modelId, ttlcache.DefaultTTL)

				openFGAServiceClientMock.EXPECT().
					Check(ctx, &openfgav1.CheckRequest{
						StoreId:              storeId,
						AuthorizationModelId: modelId,
						TupleKey: &openfgav1.CheckRequestTupleKey{
							Object:   object,
							Relation: relation,
							User:     user,
						},
					}).
					Return(&openfgav1.CheckResponse{}, nil).
					Once()
			},
		},
		{
			name: "Check_StoreIdError",
			setupMock: func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {

				openFGAServiceClientMock.EXPECT().
					ListStores(ctx, &openfgav1.ListStoresRequest{}).
					Return(&openfgav1.ListStoresResponse{}, assert.AnError).
					Once()
			},
			expectedErr: assert.AnError,
		},
		{
			name: "Check_ModelIdError",
			setupMock: func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
				client.cache.Set(cacheKeyForStore(tenantId), storeId, ttlcache.DefaultTTL)

				openFGAServiceClientMock.EXPECT().
					ReadAuthorizationModels(ctx, &openfgav1.ReadAuthorizationModelsRequest{StoreId: storeId}).
					Return(&openfgav1.ReadAuthorizationModelsResponse{}, assert.AnError).
					Once()
			},
			expectedErr: assert.AnError,
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

			_, err = client.Check(ctx, object, relation, user, tenantId)
			assert.Equal(t, tt.expectedErr, err)

			openFGAServiceClientMock.AssertExpectations(t)
		})
	}
}
