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

func TestOpenFGAClient_Exists(t *testing.T) {
	tenantId := "tenant123"
	storeId := "store123"
	object := "object"
	relation := "relation"
	user := "user"

	tests := []struct {
		name             string
		setupMock        func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient)
		expectedResponse bool
		expectedErr      error
	}{
		{
			name: "Exists_OK",
			setupMock: func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
				client.cache.Set(cacheKeyForStore(tenantId), storeId, ttlcache.DefaultTTL)

				openFGAServiceClientMock.EXPECT().
					Read(ctx, &openfgav1.ReadRequest{
						StoreId: storeId,
						TupleKey: &openfgav1.ReadRequestTupleKey{
							Object:   object,
							Relation: relation,
							User:     user,
						}}).
					Return(&openfgav1.ReadResponse{Tuples: []*openfgav1.Tuple{{Key: nil}}}, nil).
					Once()
			},
			expectedResponse: true,
		},
		{
			name: "Exists_Read_Error",
			setupMock: func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
				client.cache.Set(cacheKeyForStore(tenantId), storeId, ttlcache.DefaultTTL)

				openFGAServiceClientMock.EXPECT().
					Read(ctx, &openfgav1.ReadRequest{
						StoreId: storeId,
						TupleKey: &openfgav1.ReadRequestTupleKey{
							Object:   object,
							Relation: relation,
							User:     user,
						}}).
					Return(nil, assert.AnError).
					Once()
			},
			expectedResponse: false,
			expectedErr:      assert.AnError,
		},
		{
			name: "Exists_No_Tuples_Error",
			setupMock: func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
				client.cache.Set(cacheKeyForStore(tenantId), storeId, ttlcache.DefaultTTL)

				openFGAServiceClientMock.EXPECT().
					Read(ctx, &openfgav1.ReadRequest{
						StoreId: storeId,
						TupleKey: &openfgav1.ReadRequestTupleKey{
							Object:   object,
							Relation: relation,
							User:     user,
						}}).
					Return(&openfgav1.ReadResponse{}, nil).
					Once()
			},
			expectedResponse: false,
			expectedErr:      nil,
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

			res, err := client.Exists(ctx, &openfgav1.TupleKeyWithoutCondition{
				Object:   object,
				Relation: relation,
				User:     user,
			}, tenantId)
			assert.Equal(t, tt.expectedResponse, res)
			assert.Equal(t, tt.expectedErr, err)

			openFGAServiceClientMock.AssertExpectations(t)
		})
	}
}
