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
	"testing"
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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
		getter := NewCachingStoreIDGetter(client, 5*time.Minute, context.Background(), log.Logger)

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
		getter := NewCachingStoreIDGetter(client, 5*time.Minute, loadCtx, log.Logger)

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
		getter := NewCachingStoreIDGetter(client, 5*time.Minute, loadCtx, log.Logger)

		id, err := getter.Get(context.Background(), "missing-store")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "store \"missing-store\" not found")
		assert.Empty(t, id)
	})

	t.Run("returns error when ListStores fails", func(t *testing.T) {
		client := mocks.NewMockOpenFGAServiceClient(t)
		client.EXPECT().ListStores(mock.Anything, mock.Anything).Return(nil, errors.New("connection refused")).Once()

		loadCtx := context.Background()
		log := testlogger.New()
		getter := NewCachingStoreIDGetter(client, 5*time.Minute, loadCtx, log.Logger)

		id, err := getter.Get(context.Background(), "foo")
		assert.Error(t, err)
		assert.Empty(t, id)
	})
}
