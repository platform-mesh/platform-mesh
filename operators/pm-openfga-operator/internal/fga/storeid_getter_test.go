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

package fga

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.platform-mesh.io/golang-commons/logger/testlogger"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"
)

func TestCachingStoreIDGetter_Get(t *testing.T) {
	t.Run("returns store ID from OpenFGA on cache miss", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		client.EXPECT().ListStores(mock.Anything, mock.Anything).Return(&openfgav1.ListStoresResponse{
			Stores: []*openfgav1.Store{
				{Name: "foo", Id: "DEADBEEF"},
			},
		}, nil).Once()

		log := testlogger.New()
		getter := NewCachingStoreIDGetter(context.Background(), client, 5*time.Minute, log.Logger)

		id, err := getter.Get(context.Background(), "foo")
		require.NoError(t, err)
		assert.Equal(t, "DEADBEEF", id)
	})

	t.Run("returns cached value on subsequent calls without calling OpenFGA", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		client.EXPECT().ListStores(mock.Anything, mock.Anything).Return(&openfgav1.ListStoresResponse{
			Stores: []*openfgav1.Store{
				{Name: "foo", Id: "DEADBEEF"},
			},
		}, nil).Once()

		loadCtx := context.Background()
		log := testlogger.New()
		getter := NewCachingStoreIDGetter(loadCtx, client, 5*time.Minute, log.Logger)

		id1, err := getter.Get(context.Background(), "foo")
		require.NoError(t, err)
		assert.Equal(t, "DEADBEEF", id1)

		id2, err := getter.Get(context.Background(), "foo")
		require.NoError(t, err)
		assert.Equal(t, "DEADBEEF", id2)

		client.AssertExpectations(t)
	})

	t.Run("returns error when store not found in OpenFGA", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		client.EXPECT().ListStores(mock.Anything, mock.Anything).Return(&openfgav1.ListStoresResponse{
			Stores: []*openfgav1.Store{
				{Name: "other-store", Id: "OTHER-ID"},
			},
		}, nil).Once()

		loadCtx := context.Background()
		log := testlogger.New()
		getter := NewCachingStoreIDGetter(loadCtx, client, 5*time.Minute, log.Logger)

		id, err := getter.Get(context.Background(), "missing-store")
		assert.Error(t, err)
		assert.True(t, errors.Is(err, ErrStoreNotFound))
		assert.Contains(t, err.Error(), "store \"missing-store\" not found")
		assert.Empty(t, id)
	})

	t.Run("returns error when ListStores fails", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		client.EXPECT().ListStores(mock.Anything, mock.Anything).Return(nil, errors.New("connection refused")).Once()

		loadCtx := context.Background()
		log := testlogger.New()
		getter := NewCachingStoreIDGetter(loadCtx, client, 5*time.Minute, log.Logger)

		id, err := getter.Get(context.Background(), "foo")
		assert.Error(t, err)
		assert.Empty(t, id)
	})
}

func TestIsStoreNotFound(t *testing.T) {
	t.Run("sentinel error", func(t *testing.T) {
		assert.True(t, IsStoreNotFound(fmt.Errorf("store %q not found: %w", "myorg", ErrStoreNotFound)))
	})

	t.Run("grpc store_id_not_found", func(t *testing.T) {
		err := status.Error(codes.Code(openfgav1.NotFoundErrorCode_store_id_not_found), "not found")
		assert.True(t, IsStoreNotFound(err))
	})

	t.Run("unrelated error", func(t *testing.T) {
		assert.False(t, IsStoreNotFound(errors.New("connection refused")))
	})
}
