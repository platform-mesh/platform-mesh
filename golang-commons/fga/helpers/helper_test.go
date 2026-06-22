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

package helpers

import (
	"context"
	"errors"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.platform-mesh.io/golang-commons/directive/mocks"
)

func TestGetModelIDForTenant(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant123"
	storeId := "store123"
	modelId := "model123"

	tests := []struct {
		name            string
		setupMock       func(client *mocks.OpenFGAServiceClient)
		expectedModelID string
		expectedError   error
	}{
		{
			name: "FullPath_OK",
			setupMock: func(client *mocks.OpenFGAServiceClient) {
				client.EXPECT().
					ListStores(ctx, &openfgav1.ListStoresRequest{}).
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
			setupMock: func(client *mocks.OpenFGAServiceClient) {
				cache.Add("model-tenant123", modelId)
			},
			expectedModelID: modelId,
			expectedError:   nil,
		},
		{
			name: "HitGetStoreIDForTenantCache_OK",
			setupMock: func(client *mocks.OpenFGAServiceClient) {
				cache.Add("tenant-tenant123", storeId)

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
			setupMock: func(client *mocks.OpenFGAServiceClient) {
				client.EXPECT().
					ListStores(ctx, &openfgav1.ListStoresRequest{}).
					Return(nil, assert.AnError).
					Once()
			},
			expectedError: assert.AnError,
		},
		{
			name: "MatchingKeyNotFound_Error",
			setupMock: func(client *mocks.OpenFGAServiceClient) {
				client.EXPECT().
					ListStores(ctx, &openfgav1.ListStoresRequest{}).
					Return(&openfgav1.ListStoresResponse{Stores: []*openfgav1.Store{}}, nil).
					Once()
			},
			expectedError: errors.New("could not find store matching key \"tenant-tenant123\""),
		},
		{
			name: "ReadAuthorizationModels_Error",
			setupMock: func(client *mocks.OpenFGAServiceClient) {
				cache.Add("tenant-tenant123", storeId)

				client.EXPECT().
					ReadAuthorizationModels(ctx, &openfgav1.ReadAuthorizationModelsRequest{StoreId: "store123"}).
					Return(nil, assert.AnError).
					Once()
			},
			expectedError: assert.AnError,
		},
		{
			name: "NoReadAuthorizationModels_Error",
			setupMock: func(client *mocks.OpenFGAServiceClient) {
				cache.Add("tenant-tenant123", storeId)

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
			cache.Purge() // Clear cache before each test

			client := &mocks.OpenFGAServiceClient{}
			tt.setupMock(client)

			modelID, err := GetModelIDForTenant(ctx, client, tenantID)

			assert.Equal(t, tt.expectedModelID, modelID)
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
		//{
		//	name:     "DuplicateWriteGRPCError",
		//	err:      status.Error(codes.InvalidArgument, openfgav1.ErrorCode_write_failed_due_to_invalid_input.String()),
		//	expected: true,
		//},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsDuplicateWriteError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeUserID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		out  string
	}{
		{
			name: "should remove all colons",
			in:   "system:serviceaccount:default:default",
			out:  "system_serviceaccount_default_default",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			out := SanitizeUserID(test.in)
			assert.Equal(t, test.out, out)
		})
	}
}
