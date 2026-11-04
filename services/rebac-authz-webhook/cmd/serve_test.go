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

package cmd

import (
	"context"
	"testing"
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/rebac-authz-webhook/pkg/handler/mocks"
)

func orgsStoreResponse() *openfgav1.ListStoresResponse {
	return &openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{{Id: "01ABC", Name: orgsStoreName}},
	}
}

func TestWaitForStore_ReturnsStoreID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		expect func(*mocks.OpenFGAServiceClient)
		wantID string
	}{
		{
			name: "store exists",
			expect: func(fga *mocks.OpenFGAServiceClient) {
				fga.EXPECT().ListStores(mock.Anything, mock.Anything).Return(orgsStoreResponse(), nil)
			},
			wantID: "01ABC",
		},
		// The cold start case where the authz webhook may start before
		// the security-operator has created the store.
		{
			name: "store appears after an empty answer and a list error",
			expect: func(fga *mocks.OpenFGAServiceClient) {
				fga.EXPECT().ListStores(mock.Anything, mock.Anything).Return(&openfgav1.ListStoresResponse{}, nil).Once()
				fga.EXPECT().ListStores(mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()
				fga.EXPECT().ListStores(mock.Anything, mock.Anything).Return(orgsStoreResponse(), nil).Once()
			},
			wantID: "01ABC",
		},
		{
			name: "store of another name is ignored",
			expect: func(fga *mocks.OpenFGAServiceClient) {
				fga.EXPECT().ListStores(mock.Anything, mock.Anything).Return(&openfgav1.ListStoresResponse{
					Stores: []*openfgav1.Store{{Id: "01OTHER", Name: "some-other-store"}},
				}, nil).Once()
				fga.EXPECT().ListStores(mock.Anything, mock.Anything).Return(orgsStoreResponse(), nil).Once()
			},
			wantID: "01ABC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fga := mocks.NewOpenFGAServiceClient(t)
			tt.expect(fga)

			storeID, err := waitForStore(t.Context(), fga, orgsStoreName)

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, storeID)
		})
	}
}

func TestWaitForStore_StoreNeverAppears(t *testing.T) {
	t.Parallel()

	fga := mocks.NewOpenFGAServiceClient(t)
	fga.EXPECT().ListStores(mock.Anything, mock.Anything).Maybe().
		Run(func(args mock.Arguments) {
			req, ok := args.Get(1).(*openfgav1.ListStoresRequest)
			require.True(t, ok)
			assert.Equal(t, orgsStoreName, req.GetName(), "the wait must ask OpenFGA for the orgs store by name")
		}).
		Return(&openfgav1.ListStoresResponse{}, nil)

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	t.Cleanup(cancel)

	storeID, err := waitForStore(ctx, fga, orgsStoreName)

	require.Error(t, err)
	assert.Empty(t, storeID)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, err.Error(), orgsStoreName, "the failure must name the store it waited for")
}
