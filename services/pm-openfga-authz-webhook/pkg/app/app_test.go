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

package app

import (
	"context"
	"testing"
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/pm-openfga-authz-webhook/pkg/config"
	"go.platform-mesh.io/pm-openfga-authz-webhook/pkg/handler/mocks"

	"k8s.io/client-go/rest"
)

func orgsStoreResponse() *openfgav1.ListStoresResponse {
	return &openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{{Id: "01ABC", Name: OrgsStoreName}},
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
		// the pm-openfga-operator has created the store.
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

			storeID, err := WaitForStore(t.Context(), fga, OrgsStoreName, time.Minute)

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
			assert.Equal(t, OrgsStoreName, req.GetName(), "the wait must ask OpenFGA for the orgs store by name")
		}).
		Return(&openfgav1.ListStoresResponse{}, nil)

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	t.Cleanup(cancel)

	storeID, err := WaitForStore(ctx, fga, OrgsStoreName, 0)

	require.Error(t, err)
	assert.Empty(t, storeID)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, err.Error(), OrgsStoreName, "the failure must name the store it waited for")
}

func TestWaitForStore_Timeout(t *testing.T) {
	t.Parallel()

	fga := mocks.NewOpenFGAServiceClient(t)
	fga.EXPECT().ListStores(mock.Anything, mock.Anything).Maybe().Return(&openfgav1.ListStoresResponse{}, nil)

	storeID, err := WaitForStore(t.Context(), fga, OrgsStoreName, 100*time.Millisecond)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Empty(t, storeID)
	require.NoError(t, t.Context().Err(), "the timeout must not cancel the caller's context")
}

const workspacePrefix = "/clusters/root:platform-mesh-system"

func TestRun_ReturnsErrors(t *testing.T) {
	t.Run("missing arguments", func(t *testing.T) {
		require.Error(t, Run(t.Context(), nil, config.New(), Deps{}))
		require.Error(t, Run(t.Context(), &rest.Config{}, nil, Deps{}))
	})

	t.Run("kcp not reachable", func(t *testing.T) {
		err := Run(t.Context(), &rest.Config{Host: "http://127.0.0.1:1" + workspacePrefix}, config.New(), Deps{})
		require.ErrorContains(t, err, "unable to construct cluster provider")
	})
}
