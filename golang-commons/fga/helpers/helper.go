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
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"google.golang.org/grpc/status"
)

var cache = expirable.NewLRU[string, string](10, nil, 10*time.Minute)

func GetStoreIDForTenant(ctx context.Context, client openfgav1.OpenFGAServiceClient, tenantID string) (string, error) {

	cacheKey := "tenant-" + tenantID
	s, ok := cache.Get(cacheKey)
	if ok && s != "" {
		return s, nil
	}

	res, err := client.ListStores(ctx, &openfgav1.ListStoresRequest{})
	if err != nil {
		return "", err
	}

	idx := slices.IndexFunc(res.GetStores(), func(s *openfgav1.Store) bool { return s.Name == cacheKey })
	if idx < 0 {
		return "", fmt.Errorf("could not find store matching key %q", cacheKey)
	}

	store := res.GetStores()[idx]
	cache.Add(cacheKey, store.Id)

	return store.Id, nil
}

func GetModelIDForTenant(ctx context.Context, conn openfgav1.OpenFGAServiceClient, tenantID string) (string, error) {

	cacheKey := "model-" + tenantID
	s, ok := cache.Get(cacheKey)
	if ok && s != "" {
		return s, nil
	}

	storeID, err := GetStoreIDForTenant(ctx, conn, tenantID)
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
	cache.Add(cacheKey, modelID)

	return modelID, nil
}

func IsDuplicateWriteError(err error) bool {
	if err == nil {
		return false
	}

	s, ok := status.FromError(err)
	return ok && int32(s.Code()) == int32(openfgav1.ErrorCode_write_failed_due_to_invalid_input)
}

// Sanitizes the userID by replacing all colons with underscores to also enable the use of kubernetes service accounts
func SanitizeUserID(userID string) string {
	return strings.ReplaceAll(userID, ":", "_")
}
