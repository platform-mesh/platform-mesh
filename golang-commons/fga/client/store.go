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

	"github.com/jellydator/ttlcache/v3"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
)

func (c *OpenFGAClient) ModelId(ctx context.Context, tenantId string) (string, error) {
	if cacheItem := c.cache.Get(cacheKeyForModel(tenantId)); cacheItem != nil {
		val := cacheItem.Value()
		return val, nil
	}

	storeId, err := c.StoreId(ctx, tenantId)
	if err != nil {
		return "", err
	}

	resp, err := c.client.ReadAuthorizationModels(ctx, &openfgav1.ReadAuthorizationModelsRequest{StoreId: storeId})
	if err != nil {
		return "", err
	}

	if len(resp.AuthorizationModels) > 0 {
		c.cache.Set(cacheKeyForModel(tenantId), resp.AuthorizationModels[0].Id, ttlcache.DefaultTTL)
		return resp.AuthorizationModels[0].Id, nil
	}

	return "", errors.New("could not determine model. No models found")
}

func (c *OpenFGAClient) StoreId(ctx context.Context, tenantId string) (string, error) {
	if cacheItem := c.cache.Get(cacheKeyForStore(tenantId)); cacheItem != nil {
		val := cacheItem.Value()
		return val, nil
	}

	expectedStoreName := fmt.Sprintf("tenant-%s", tenantId)
	resp, err := c.client.ListStores(ctx, &openfgav1.ListStoresRequest{})
	if err != nil {
		return "", err
	}

	for _, store := range resp.Stores {
		if store.Name == expectedStoreName {
			c.cache.Set(cacheKeyForStore(tenantId), store.Id, ttlcache.DefaultTTL)
			return store.Id, nil
		}
	}

	return "", errors.New("could not determine store. No stores found")
}
