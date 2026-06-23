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

// Package store provides functionality for managing OpenFGA store and authorization
// model operations with caching capabilities.
//
// This package contains the StoreHelper interface and its implementation (PMStoreHelper)
// for efficiently managing OpenFGA store and authorization model lookups with LRU
// caching to reduce API calls and improve performance.
package store

import (
	"context"
	"slices"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/rs/zerolog/log"
	"go.platform-mesh.io/golang-commons/errors"
)

// StoreHelper provides methods for managing OpenFGA store and model operations
// with built-in caching to improve performance by reducing redundant API calls.
type StoreHelper interface {
	// GetStoreID retrieves the OpenFGA store ID for a given organization.
	// It first checks the internal cache and returns the cached value if available.
	// If not cached, it queries OpenFGA's ListStores API to find the store with
	// the specified organization name, caches the result, and returns it.
	//
	// Parameters:
	//   - ctx: Context for the operation
	//   - conn: OpenFGA service client connection
	//   - orgID: Organization identifier used as the store name
	//
	// Returns:
	//   - string: The OpenFGA store ID
	//   - error: Error if store is not found or API call fails
	GetStoreID(ctx context.Context, conn openfgav1.OpenFGAServiceClient, orgID string) (string, error)

	// GetModelID retrieves the most recent authorization model ID for a given organization.
	// It first attempts to get the store ID using GetStoreID, then queries the
	// ReadAuthorizationModels API to get the list of models, returning the first
	// (most recent) model ID. Results are cached to improve performance.
	//
	// Parameters:
	//   - ctx: Context for the operation
	//   - conn: OpenFGA service client connection
	//   - orgID: Organization identifier
	//
	// Returns:
	//   - string: The most recent authorization model ID
	//   - error: Error if store/model is not found or API call fails
	GetModelID(ctx context.Context, conn openfgav1.OpenFGAServiceClient, orgID string) (string, error)
}

// PMStoreHelper is the concrete implementation of StoreHelper that provides
// caching functionality for OpenFGA store and model operations. It uses an
// LRU (Least Recently Used) cache with time-based expiration to store
// frequently accessed store and model IDs.
type PMStoreHelper struct {
	// cache is an expirable LRU cache that stores string key-value pairs
	// with automatic expiration based on the configured TTL
	cache *expirable.LRU[string, string]
}

// NewFGAStoreHelper creates a new instance of PMStoreHelper with the specified
// time-to-live (TTL) for cached entries. The cache has a maximum capacity of 10
// entries and uses LRU eviction policy when the capacity is exceeded.
//
// Parameters:
//   - ttl: Time-to-live duration for cached entries. After this duration,
//     cached entries will be automatically expired and removed.
//
// Returns:
//   - StoreHelper: A new PMStoreHelper instance implementing the StoreHelper interface
func NewFGAStoreHelper(ttl time.Duration) StoreHelper {
	return &PMStoreHelper{cache: expirable.NewLRU[string, string](10, nil, ttl)}
}

// GetStoreID implements the StoreHelper interface method to retrieve an OpenFGA store ID
// for the specified organization. It uses a cache-first approach to minimize API calls.
//
// The method follows this workflow:
// 1. Check the cache using key "store-{orgID}"
// 2. If cached and non-empty, return the cached value immediately
// 3. If not cached, call OpenFGA's ListStores API
// 4. Search through the stores to find one with matching name (orgID)
// 5. Cache the result and return the store ID
//
// Cache key format: "store-{orgID}"
// Example: For orgID "my-org", cache key would be "store-my-org"
func (d PMStoreHelper) GetStoreID(ctx context.Context, conn openfgav1.OpenFGAServiceClient, orgID string) (string, error) {
	cacheKey := "store-" + orgID
	s, ok := d.cache.Get(cacheKey)
	if ok && s != "" {
		return s, nil
	}

	stores, err := conn.ListStores(ctx, &openfgav1.ListStoresRequest{})
	if err != nil {
		log.Error().Err(err).Msg("Failed to list stores")
		return "", errors.Wrap(err, "failed to list stores from OpenFGA")
	}

	idx := slices.IndexFunc(stores.Stores, func(store *openfgav1.Store) bool {
		return store.Name == orgID
	})
	if idx == -1 {
		return "", errors.New("store with name %s not found", orgID)
	}

	storeID := stores.Stores[idx].Id
	d.cache.Add(cacheKey, storeID)
	return storeID, nil
}

// GetModelID implements the StoreHelper interface method to retrieve the most recent
// authorization model ID for the specified organization. It uses caching and depends
// on GetStoreID to first obtain the store ID.
//
// The method follows this workflow:
// 1. Check the cache using key "model-{orgID}"
// 2. If cached and non-empty, return the cached value immediately
// 3. If not cached, call GetStoreID to obtain the store ID
// 4. Call OpenFGA's ReadAuthorizationModels API with the store ID
// 5. Return the first (most recent) model from the response
// 6. Cache the result for future use
//
// Cache key format: "model-{orgID}"
// Example: For orgID "my-org", cache key would be "model-my-org"
//
// Note: OpenFGA returns authorization models in descending order by creation time,
// so the first model in the response is the most recent one.
func (d PMStoreHelper) GetModelID(ctx context.Context, conn openfgav1.OpenFGAServiceClient, orgID string) (string, error) {
	cacheKey := "model-" + orgID
	s, ok := d.cache.Get(cacheKey)
	if ok && s != "" {
		return s, nil
	}

	storeID, err := d.GetStoreID(ctx, conn, orgID)
	if err != nil {
		return "", errors.Wrap(err, "failed to get store ID for tenant %s", orgID)
	}
	res, err := conn.ReadAuthorizationModels(ctx, &openfgav1.ReadAuthorizationModelsRequest{StoreId: storeID})
	if err != nil {
		return "", errors.Wrap(err, "failed to read authorization models for store %s", storeID)
	}

	if len(res.AuthorizationModels) < 1 {
		return "", errors.New("no authorization models in response. Cannot determine proper AuthorizationModelId")
	}

	modelID := res.AuthorizationModels[0].Id
	d.cache.Add(cacheKey, modelID)

	return modelID, nil
}
