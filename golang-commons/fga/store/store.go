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
	"fmt"
	"slices"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"google.golang.org/grpc/status"
)

type FGAStoreHelper interface {
	GetStoreIDForTenant(ctx context.Context, conn openfgav1.OpenFGAServiceClient, tenantID string) (string, error)
	GetModelIDForTenant(ctx context.Context, conn openfgav1.OpenFGAServiceClient, tenantID string) (string, error)
	IsDuplicateWriteError(err error) bool
}

type FgaTenantStore struct {
	cache       *expirable.LRU[string, string]
	storePrefix string
}

var _ FGAStoreHelper = (*FgaTenantStore)(nil)

// Deprecated: Use NewWithPrefix instead.
func New() *FgaTenantStore {
	return &FgaTenantStore{
		cache:       expirable.NewLRU[string, string](10, nil, 10*time.Minute),
		storePrefix: "tenant-",
	}
}

func NewWithPrefix(prefix string) *FgaTenantStore {
	return &FgaTenantStore{
		cache:       expirable.NewLRU[string, string](10, nil, 10*time.Minute),
		storePrefix: prefix,
	}
}

func (c *FgaTenantStore) GetStoreIDForTenant(ctx context.Context, conn openfgav1.OpenFGAServiceClient, tenantID string) (string, error) {

	cacheKey := "tenant-" + tenantID
	s, ok := c.cache.Get(cacheKey)
	if ok && s != "" {
		return s, nil
	}

	res, err := conn.ListStores(ctx, &openfgav1.ListStoresRequest{})
	if err != nil {
		return "", err
	}

	storeName := c.storePrefix + tenantID
	idx := slices.IndexFunc(res.GetStores(), func(s *openfgav1.Store) bool { return s.Name == storeName })
	if idx < 0 {
		return "", fmt.Errorf("could not find store matching key %q", storeName)
	}

	store := res.GetStores()[idx]
	c.cache.Add(cacheKey, store.Id)

	return store.Id, nil
}

func (c *FgaTenantStore) GetModelIDForTenant(ctx context.Context, conn openfgav1.OpenFGAServiceClient, tenantID string) (string, error) {

	cacheKey := "model-" + tenantID
	s, ok := c.cache.Get(cacheKey)
	if ok && s != "" {
		return s, nil
	}

	storeID, err := c.GetStoreIDForTenant(ctx, conn, tenantID)
	if err != nil {
		return "", err
	}

	res, err := conn.ReadAuthorizationModels(ctx, &openfgav1.ReadAuthorizationModelsRequest{StoreId: storeID})
	if err != nil {
		return "", err
	}

	if len(res.AuthorizationModels) < 1 {
		return "", errors.New("no authorization models in response. Cannot determine proper AuthorizationModelId")
	}

	modelID := res.AuthorizationModels[0].Id
	c.cache.Add(cacheKey, modelID)

	return modelID, nil
}

func (c *FgaTenantStore) IsDuplicateWriteError(err error) bool {
	if err == nil {
		return false
	}

	s, ok := status.FromError(err)
	return ok && int32(s.Code()) == int32(openfgav1.ErrorCode_write_failed_due_to_invalid_input)
}

func (c *FgaTenantStore) GetCache() *expirable.LRU[string, string] {
	return c.cache
}
