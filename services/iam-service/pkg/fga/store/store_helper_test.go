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

package store

import (
	"context"
	"errors"
	"testing"
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"

	fgamocks "go.platform-mesh.io/iam-service/pkg/fga/mocks"
)

func TestNewFGAStoreHelper(t *testing.T) {
	helper := NewFGAStoreHelper(5 * time.Minute)
	assert.NotNil(t, helper)
}

func TestStoreHelper_GetStoreID_Success(t *testing.T) {
	client := fgamocks.NewOpenFGAServiceClient(t)
	helper := NewFGAStoreHelper(5 * time.Minute)

	ctx := context.Background()
	orgID := "test-org"
	expectedStoreID := "store-123"

	listStoresResponse := &openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{
			{
				Id:   "store-456",
				Name: "other-org",
			},
			{
				Id:   expectedStoreID,
				Name: orgID,
			},
		},
	}

	client.EXPECT().ListStores(ctx, &openfgav1.ListStoresRequest{}).Return(listStoresResponse, nil)

	storeID, err := helper.GetStoreID(ctx, client, orgID)

	assert.NoError(t, err)
	assert.Equal(t, expectedStoreID, storeID)
}

func TestStoreHelper_GetStoreID_CachedResult(t *testing.T) {
	client := fgamocks.NewOpenFGAServiceClient(t)
	helper := NewFGAStoreHelper(5 * time.Minute)

	ctx := context.Background()
	orgID := "test-org"
	expectedStoreID := "store-123"

	listStoresResponse := &openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{
			{
				Id:   expectedStoreID,
				Name: orgID,
			},
		},
	}

	// First call should hit ListStores and cache the result
	client.EXPECT().ListStores(ctx, &openfgav1.ListStoresRequest{}).Return(listStoresResponse, nil).Once()

	// First call
	storeID1, err := helper.GetStoreID(ctx, client, orgID)
	assert.NoError(t, err)
	assert.Equal(t, expectedStoreID, storeID1)

	// Second call should use cached result (no additional ListStores call expected)
	storeID2, err := helper.GetStoreID(ctx, client, orgID)
	assert.NoError(t, err)
	assert.Equal(t, expectedStoreID, storeID2)
}

func TestStoreHelper_GetStoreID_StoreNotFound(t *testing.T) {
	client := fgamocks.NewOpenFGAServiceClient(t)
	helper := NewFGAStoreHelper(5 * time.Minute)

	ctx := context.Background()
	orgID := "nonexistent-org"

	listStoresResponse := &openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{
			{
				Id:   "store-123",
				Name: "other-org",
			},
		},
	}

	client.EXPECT().ListStores(ctx, &openfgav1.ListStoresRequest{}).Return(listStoresResponse, nil)

	storeID, err := helper.GetStoreID(ctx, client, orgID)

	assert.Error(t, err)
	assert.Empty(t, storeID)
	assert.Contains(t, err.Error(), "store with name nonexistent-org not found")
}

func TestStoreHelper_GetStoreID_ListStoresError(t *testing.T) {
	client := fgamocks.NewOpenFGAServiceClient(t)
	helper := NewFGAStoreHelper(5 * time.Minute)

	ctx := context.Background()
	orgID := "test-org"

	client.EXPECT().ListStores(ctx, &openfgav1.ListStoresRequest{}).Return(nil, errors.New("connection failed"))

	storeID, err := helper.GetStoreID(ctx, client, orgID)

	assert.Error(t, err)
	assert.Empty(t, storeID)
	assert.Contains(t, err.Error(), "connection failed")
}

func TestStoreHelper_GetModelID_Success(t *testing.T) {
	client := fgamocks.NewOpenFGAServiceClient(t)
	helper := NewFGAStoreHelper(5 * time.Minute)

	ctx := context.Background()
	orgID := "test-org"
	storeID := "store-123"
	expectedModelID := "model-456"

	// Mock ListStores for GetStoreID
	listStoresResponse := &openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{
			{
				Id:   storeID,
				Name: orgID,
			},
		},
	}
	client.EXPECT().ListStores(ctx, &openfgav1.ListStoresRequest{}).Return(listStoresResponse, nil)

	// Mock ReadAuthorizationModels
	readModelsResponse := &openfgav1.ReadAuthorizationModelsResponse{
		AuthorizationModels: []*openfgav1.AuthorizationModel{
			{
				Id: expectedModelID,
			},
			{
				Id: "model-789",
			},
		},
	}
	client.EXPECT().ReadAuthorizationModels(ctx, &openfgav1.ReadAuthorizationModelsRequest{
		StoreId: storeID,
	}).Return(readModelsResponse, nil)

	modelID, err := helper.GetModelID(ctx, client, orgID)

	assert.NoError(t, err)
	assert.Equal(t, expectedModelID, modelID)
}

func TestStoreHelper_GetModelID_CachedResult(t *testing.T) {
	client := fgamocks.NewOpenFGAServiceClient(t)
	helper := NewFGAStoreHelper(5 * time.Minute)

	ctx := context.Background()
	orgID := "test-org"
	storeID := "store-123"
	expectedModelID := "model-456"

	// Mock ListStores for GetStoreID
	listStoresResponse := &openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{
			{
				Id:   storeID,
				Name: orgID,
			},
		},
	}
	client.EXPECT().ListStores(ctx, &openfgav1.ListStoresRequest{}).Return(listStoresResponse, nil).Once()

	// Mock ReadAuthorizationModels
	readModelsResponse := &openfgav1.ReadAuthorizationModelsResponse{
		AuthorizationModels: []*openfgav1.AuthorizationModel{
			{
				Id: expectedModelID,
			},
		},
	}
	client.EXPECT().ReadAuthorizationModels(ctx, &openfgav1.ReadAuthorizationModelsRequest{
		StoreId: storeID,
	}).Return(readModelsResponse, nil).Once()

	// First call should hit the actual methods and cache the result
	modelID1, err := helper.GetModelID(ctx, client, orgID)
	assert.NoError(t, err)
	assert.Equal(t, expectedModelID, modelID1)

	// Second call should use cached result (no additional API calls expected)
	modelID2, err := helper.GetModelID(ctx, client, orgID)
	assert.NoError(t, err)
	assert.Equal(t, expectedModelID, modelID2)
}

func TestStoreHelper_GetModelID_GetStoreIDError(t *testing.T) {
	client := fgamocks.NewOpenFGAServiceClient(t)
	helper := NewFGAStoreHelper(5 * time.Minute)

	ctx := context.Background()
	orgID := "test-org"

	// Mock ListStores to fail
	client.EXPECT().ListStores(ctx, &openfgav1.ListStoresRequest{}).Return(nil, errors.New("store error"))

	modelID, err := helper.GetModelID(ctx, client, orgID)

	assert.Error(t, err)
	assert.Empty(t, modelID)
	assert.Contains(t, err.Error(), "failed to get store ID")
}

func TestStoreHelper_GetModelID_NoModels(t *testing.T) {
	client := fgamocks.NewOpenFGAServiceClient(t)
	helper := NewFGAStoreHelper(5 * time.Minute)

	ctx := context.Background()
	orgID := "test-org"
	storeID := "store-123"

	// Mock ListStores for GetStoreID
	listStoresResponse := &openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{
			{
				Id:   storeID,
				Name: orgID,
			},
		},
	}
	client.EXPECT().ListStores(ctx, &openfgav1.ListStoresRequest{}).Return(listStoresResponse, nil)

	// Mock ReadAuthorizationModels with empty response
	readModelsResponse := &openfgav1.ReadAuthorizationModelsResponse{
		AuthorizationModels: []*openfgav1.AuthorizationModel{},
	}
	client.EXPECT().ReadAuthorizationModels(ctx, &openfgav1.ReadAuthorizationModelsRequest{
		StoreId: storeID,
	}).Return(readModelsResponse, nil)

	modelID, err := helper.GetModelID(ctx, client, orgID)

	assert.Error(t, err)
	assert.Empty(t, modelID)
	assert.Contains(t, err.Error(), "no authorization models in response")
}

func TestStoreHelper_GetModelID_ReadModelsError(t *testing.T) {
	client := fgamocks.NewOpenFGAServiceClient(t)
	helper := NewFGAStoreHelper(5 * time.Minute)

	ctx := context.Background()
	orgID := "test-org"
	storeID := "store-123"

	// Mock ListStores for GetStoreID
	listStoresResponse := &openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{
			{
				Id:   storeID,
				Name: orgID,
			},
		},
	}
	client.EXPECT().ListStores(ctx, &openfgav1.ListStoresRequest{}).Return(listStoresResponse, nil)

	// Mock ReadAuthorizationModels to fail
	client.EXPECT().ReadAuthorizationModels(ctx, &openfgav1.ReadAuthorizationModelsRequest{
		StoreId: storeID,
	}).Return(nil, errors.New("read models failed"))

	modelID, err := helper.GetModelID(ctx, client, orgID)

	assert.Error(t, err)
	assert.Empty(t, modelID)
	assert.Contains(t, err.Error(), "read models failed")
}
