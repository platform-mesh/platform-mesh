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
	"fmt"
	"strconv"
	"strings"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"

	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/search-service/internal/service/search"
)

const batchCheckChunkSize = 100

type Authorizer struct {
	client openfgav1.OpenFGAServiceClient
}

func NewAuthorizer(client openfgav1.OpenFGAServiceClient) *Authorizer {
	return &Authorizer{client: client}
}

func (a *Authorizer) FilterAuthorized(ctx context.Context, req search.AuthorizationRequest) (search.AuthorizationResult, error) {
	log := logger.LoadLoggerFromContext(ctx)
	allowed := make([]bool, len(req.Hits))
	if len(req.Hits) == 0 {
		return search.AuthorizationResult{Allowed: allowed}, nil
	}

	storeID, err := a.resolveStoreID(ctx, req.Organization)
	if err != nil {
		log.Error().
			Err(err).
			Str("organization", req.Organization).
			Str("user", req.User).
			Msg("failed to resolve OpenFGA store ID")
		return search.AuthorizationResult{}, fmt.Errorf("resolve store ID: %w", err)
	}

	log.Debug().
		Str("organization", req.Organization).
		Str("storeID", storeID).
		Str("user", req.User).
		Int("hits", len(req.Hits)).
		Msg("resolved OpenFGA store for authorization")

	result := search.AuthorizationResult{Allowed: allowed}

	for _, chunk := range chunkRanges(len(req.Hits), batchCheckChunkSize) {
		start := chunk[0]
		end := chunk[1]
		items := make([]*openfgav1.BatchCheckItem, 0, end-start)
		indicesByCorrelation := make(map[string]int, end-start)

		for idx := start; idx < end; idx++ {
			item, missingContext := buildBatchCheckItem(log, req.User, req.Relation, idx, req.Hits[idx])
			if missingContext {
				result.DroppedMissingContext++
				continue
			}
			items = append(items, item)
			indicesByCorrelation[item.CorrelationId] = idx
		}

		if len(items) == 0 {
			continue
		}

		batchResponse, err := a.client.BatchCheck(ctx, &openfgav1.BatchCheckRequest{
			StoreId: storeID,
			Checks:  items,
		})
		if err != nil {
			log.Error().
				Err(err).
				Str("organization", req.Organization).
				Str("storeID", storeID).
				Str("user", req.User).
				Int("checks", len(items)).
				Msg("OpenFGA BatchCheck failed")
			return search.AuthorizationResult{}, fmt.Errorf("openfga batch check: %w", err)
		}
		result.Calls++

		for correlationID, index := range indicesByCorrelation {
			entry, ok := batchResponse.GetResult()[correlationID]
			if !ok {
				result.Denied++
				continue
			}
			if entry.GetAllowed() {
				result.Allowed[index] = true
				continue
			}
			result.Denied++
		}
	}

	return result, nil
}

func (a *Authorizer) resolveStoreID(ctx context.Context, org string) (string, error) {
	res, err := a.client.ListStores(ctx, &openfgav1.ListStoresRequest{})
	if err != nil {
		return "", fmt.Errorf("list stores: %w", err)
	}

	for _, store := range res.GetStores() {
		if strings.TrimSpace(store.GetName()) == org {
			return store.GetId(), nil
		}
	}

	return "", fmt.Errorf("no OpenFGA store found for organization %q", org)
}

func chunkRanges(total, chunkSize int) [][2]int {
	if total <= 0 || chunkSize <= 0 {
		return nil
	}

	ranges := make([][2]int, 0, (total+chunkSize-1)/chunkSize)
	for start := 0; start < total; start += chunkSize {
		end := min(start+chunkSize, total)
		ranges = append(ranges, [2]int{start, end})
	}

	return ranges
}

func buildBatchCheckItem(log *logger.Logger, user, relation string, index int, hit search.OpenSearchHit) (*openfgav1.BatchCheckItem, bool) {
	authContext, ok := buildAuthorizationContext(log, hit.Source)
	if !ok {
		return nil, true
	}

	tupleKey := &openfgav1.CheckRequestTupleKey{
		User:     fmt.Sprintf("user:%s", formatUser(user)),
		Relation: relation,
		Object:   authContext.object,
	}

	log.Debug().
		Int("hitIndex", index).
		Str("user", tupleKey.User).
		Str("relation", tupleKey.Relation).
		Str("object", tupleKey.Object).
		Interface("contextualTuples", authContext.contextualTuples).
		Msg("building FGA BatchCheck item")

	return &openfgav1.BatchCheckItem{
		TupleKey:         tupleKey,
		ContextualTuples: &openfgav1.ContextualTupleKeys{TupleKeys: authContext.contextualTuples},
		CorrelationId:    strconv.Itoa(index),
	}, false
}

type authContext struct {
	object           string
	contextualTuples []*openfgav1.TupleKey
}

func buildAuthorizationContext(log *logger.Logger, source map[string]any) (authContext, bool) {
	if source == nil {
		log.Debug().Msg("auth context build failed: source is nil")
		return authContext{}, false
	}

	fgaObject := readString(source, "fga_object")
	permissionsRaw, hasPermissions := source["permissions"].([]any)

	if fgaObject != "" {
		tuples := make([]*openfgav1.TupleKey, 0)
		if hasPermissions {
			for _, p := range permissionsRaw {
				m, ok := p.(map[string]any)
				if !ok {
					continue
				}
				tuples = append(tuples, &openfgav1.TupleKey{
					User:     readString(m, "user"),
					Relation: readString(m, "relation"),
					Object:   readString(m, "object"),
				})
			}
		}
		return authContext{object: fgaObject, contextualTuples: tuples}, true
	}

	return authContext{}, false
}

func formatUser(user string) string {
	return strings.ReplaceAll(user, ":", ".")
}

func readString(source map[string]any, key string) string {
	v, ok := source[key]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return strings.TrimSpace(s)
}
