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

func TestOpenFGAClient_Write(t *testing.T) {
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
			name: "Write_OK",
			setupMock: func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
				client.cache.Set(cacheKeyForModel(tenantId), modelId, ttlcache.DefaultTTL)
				client.cache.Set(cacheKeyForStore(tenantId), storeId, ttlcache.DefaultTTL)

				openFGAServiceClientMock.EXPECT().
					Write(ctx, &openfgav1.WriteRequest{
						StoreId:              storeId,
						AuthorizationModelId: modelId,
						Writes: &openfgav1.WriteRequestWrites{
							TupleKeys: []*openfgav1.TupleKey{
								{Object: object, Relation: relation, User: user},
							},
						}}).
					Return(nil, nil).
					Once()
			},
		},
		{
			name: "WriteStoreId_Error",
			setupMock: func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
				openFGAServiceClientMock.EXPECT().
					ListStores(ctx, &openfgav1.ListStoresRequest{}).
					Return(nil, assert.AnError).
					Once()
			},
			expectedErr: assert.AnError,
		},
		{
			name: "WriteModelId_Error",
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
			name: "Write_OK",
			setupMock: func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
				client.cache.Set(cacheKeyForModel(tenantId), modelId, ttlcache.DefaultTTL)
				client.cache.Set(cacheKeyForStore(tenantId), storeId, ttlcache.DefaultTTL)

				openFGAServiceClientMock.EXPECT().
					Write(ctx, &openfgav1.WriteRequest{
						StoreId:              storeId,
						AuthorizationModelId: modelId,
						Writes: &openfgav1.WriteRequestWrites{
							TupleKeys: []*openfgav1.TupleKey{
								{Object: object, Relation: relation, User: user},
							},
						}}).
					Return(nil, assert.AnError).
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

			_, err = client.Write(ctx, object, relation, user, tenantId)
			assert.Equal(t, tt.expectedErr, err)

			openFGAServiceClientMock.AssertExpectations(t)
		})
	}
}

func TestOpenFGAClient_Delete(t *testing.T) {
	tenantId := "tenant123"
	storeId := "store123"
	modelId := "model123"
	object := "object"
	relation := "relation"
	user := "user"

	tests := []struct {
		name             string
		clientWritesMock func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient)
		expectedErr      error
	}{
		{
			name: "Write_OK",
			clientWritesMock: func(ctx context.Context, client *OpenFGAClient, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
				client.cache.Set(cacheKeyForModel(tenantId), modelId, ttlcache.DefaultTTL)
				client.cache.Set(cacheKeyForStore(tenantId), storeId, ttlcache.DefaultTTL)

				openFGAServiceClientMock.EXPECT().
					Write(ctx, &openfgav1.WriteRequest{
						StoreId:              storeId,
						AuthorizationModelId: modelId,
						Deletes: &openfgav1.WriteRequestDeletes{
							TupleKeys: []*openfgav1.TupleKeyWithoutCondition{
								{
									Object:   object,
									Relation: relation,
									User:     user,
								},
							},
						}}).
					Return(nil, nil).
					Once()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			openFGAServiceClientMock := &mocks.OpenFGAServiceClient{}

			client, err := NewOpenFGAClient(openFGAServiceClientMock)
			assert.NoError(t, err)

			if tt.clientWritesMock != nil {
				tt.clientWritesMock(ctx, client, openFGAServiceClientMock)
			}

			_, err = client.Delete(ctx, object, relation, user, tenantId)
			assert.Equal(t, tt.expectedErr, err)

			openFGAServiceClientMock.AssertExpectations(t)
		})
	}
}

func TestOpenFGAClient_WriteIfNeeded(t *testing.T) {
	tenantId := "tenant123"
	storeId := "store123"
	modelId := "model123"
	object := "object"
	relation := "relation"
	user := "user"

	tests := []struct {
		name             string
		prepareCache     func(client *OpenFGAClient)
		clientReadMock   func(ctx context.Context, openFGAServiceClientMock *mocks.OpenFGAServiceClient)
		clientWritesMock func(ctx context.Context, openFGAServiceClientMock *mocks.OpenFGAServiceClient)
		expectedErr      error
	}{
		{
			name: "WriteIfNeeded_OK",
			prepareCache: func(client *OpenFGAClient) {
				client.cache.Set(cacheKeyForModel(tenantId), modelId, ttlcache.DefaultTTL)
				client.cache.Set(cacheKeyForStore(tenantId), storeId, ttlcache.DefaultTTL)
			},
			clientReadMock: func(ctx context.Context, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
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
			clientWritesMock: func(ctx context.Context, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
				openFGAServiceClientMock.EXPECT().
					Write(ctx, &openfgav1.WriteRequest{
						StoreId:              storeId,
						AuthorizationModelId: modelId,
						Writes: &openfgav1.WriteRequestWrites{
							TupleKeys: []*openfgav1.TupleKey{
								{Object: object, Relation: relation, User: user},
							},
						}}).
					Return(nil, nil).
					Once()
			},
		},
		{
			name: "WriteIfNeededClientRead_Error",
			prepareCache: func(client *OpenFGAClient) {
				client.cache.Set(cacheKeyForModel(tenantId), modelId, ttlcache.DefaultTTL)
				client.cache.Set(cacheKeyForStore(tenantId), storeId, ttlcache.DefaultTTL)
			},
			clientReadMock: func(ctx context.Context, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
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
			expectedErr: assert.AnError,
		},

		{
			name: "WriteIfNeededWrites_Error",
			prepareCache: func(client *OpenFGAClient) {
				client.cache.Set(cacheKeyForModel(tenantId), modelId, ttlcache.DefaultTTL)
				client.cache.Set(cacheKeyForStore(tenantId), storeId, ttlcache.DefaultTTL)
			},
			clientReadMock: func(ctx context.Context, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
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
			clientWritesMock: func(ctx context.Context, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
				openFGAServiceClientMock.EXPECT().
					Write(ctx, &openfgav1.WriteRequest{
						StoreId:              storeId,
						AuthorizationModelId: modelId,
						Writes: &openfgav1.WriteRequestWrites{
							TupleKeys: []*openfgav1.TupleKey{
								{
									Object:   object,
									Relation: relation,
									User:     user,
								},
							},
						}}).
					Return(nil, assert.AnError).
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

			if tt.prepareCache != nil {
				tt.prepareCache(client)
			}

			if tt.clientReadMock != nil {
				tt.clientReadMock(ctx, openFGAServiceClientMock)
			}

			if tt.clientWritesMock != nil {
				tt.clientWritesMock(ctx, openFGAServiceClientMock)
			}

			err = client.WriteIfNeeded(ctx, []*openfgav1.TupleKeyWithoutCondition{
				{
					Object:   object,
					Relation: relation,
					User:     user,
				},
			}, tenantId)
			assert.Equal(t, tt.expectedErr, err)

			openFGAServiceClientMock.AssertExpectations(t)
		})
	}
}

func TestOpenFGAClient_DeleteIfNeeded(t *testing.T) {
	tenantId := "tenant123"
	storeId := "store123"
	modelId := "model123"
	object := "object"
	relation := "relation"
	user := "user"

	tests := []struct {
		name             string
		prepareCache     func(client *OpenFGAClient)
		clientReadMock   func(ctx context.Context, openFGAServiceClientMock *mocks.OpenFGAServiceClient)
		clientWritesMock func(ctx context.Context, openFGAServiceClientMock *mocks.OpenFGAServiceClient)
		expectedErr      error
	}{
		{
			name: "DeleteIfNeeded_OK",
			prepareCache: func(client *OpenFGAClient) {
				client.cache.Set(cacheKeyForModel(tenantId), modelId, ttlcache.DefaultTTL)
				client.cache.Set(cacheKeyForStore(tenantId), storeId, ttlcache.DefaultTTL)
			},
			clientReadMock: func(ctx context.Context, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
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
			clientWritesMock: func(ctx context.Context, openFGAServiceClientMock *mocks.OpenFGAServiceClient) {
				openFGAServiceClientMock.EXPECT().
					Write(ctx, &openfgav1.WriteRequest{
						StoreId:              storeId,
						AuthorizationModelId: modelId,
						Deletes: &openfgav1.WriteRequestDeletes{
							TupleKeys: []*openfgav1.TupleKeyWithoutCondition{
								{
									Object:   object,
									Relation: relation,
									User:     user,
								},
							},
						}}).
					Return(nil, nil).
					Once()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			openFGAServiceClientMock := &mocks.OpenFGAServiceClient{}

			client, err := NewOpenFGAClient(openFGAServiceClientMock)
			assert.NoError(t, err)

			if tt.prepareCache != nil {
				tt.prepareCache(client)
			}

			if tt.clientReadMock != nil {
				tt.clientReadMock(ctx, openFGAServiceClientMock)
			}

			if tt.clientWritesMock != nil {
				tt.clientWritesMock(ctx, openFGAServiceClientMock)
			}

			err = client.DeleteIfNeeded(ctx, []*openfgav1.TupleKeyWithoutCondition{
				{
					Object:   object,
					Relation: relation,
					User:     user,
				},
			}, tenantId)
			assert.Equal(t, tt.expectedErr, err)

			openFGAServiceClientMock.AssertExpectations(t)
		})
	}
}
