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

package search

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"go.platform-mesh.io/golang-commons/logger"

	"go.platform-mesh.io/search-service/internal/observability"
)

type ServiceConfig struct {
	DefaultLimit   int
	MaxLimit       int
	FetchBatchSize int
	MaxScannedHits int
}

type Service struct {
	resolver   SearchIndexResolver
	searcher   OpenSearchSearcher
	authorizer FGAAuthorizer
	metrics    *observability.Metrics
	cfg        ServiceConfig
}

func NewService(
	resolver SearchIndexResolver,
	searcher OpenSearchSearcher,
	authorizer FGAAuthorizer,
	metrics *observability.Metrics,
	cfg ServiceConfig,
) *Service {
	if cfg.DefaultLimit <= 0 {
		cfg.DefaultLimit = 20
	}
	if cfg.MaxLimit <= 0 {
		cfg.MaxLimit = 100
	}
	if cfg.FetchBatchSize <= 0 {
		cfg.FetchBatchSize = 100
	}
	if cfg.MaxScannedHits <= 0 {
		cfg.MaxScannedHits = 1000
	}
	if metrics == nil {
		metrics = observability.NewMetrics()
	}

	return &Service{
		resolver:   resolver,
		searcher:   searcher,
		authorizer: authorizer,
		metrics:    metrics,
		cfg:        cfg,
	}
}

func (s *Service) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	start := time.Now()
	s.metrics.IncSearchRequests()
	defer func() {
		s.metrics.ObserveSearchDuration(time.Since(start))
	}()

	log := logger.LoadLoggerFromContext(ctx)

	query := strings.TrimSpace(req.Query)
	if query == "" {
		query = "*"
	}
	org := strings.TrimSpace(req.Organization)
	if org == "" {
		return SearchResponse{}, fmt.Errorf("%w: organization is required", ErrInvalidRequest)
	}
	user := strings.TrimSpace(req.User)
	if user == "" {
		return SearchResponse{}, fmt.Errorf("%w: user is required", ErrInvalidRequest)
	}
	mode, err := normalizeMode(req.Mode)
	if err != nil {
		return SearchResponse{}, err
	}
	resource := strings.TrimSpace(req.Resource)
	filters := normalizeFilters(req.Filters)
	if resource == "" && len(filters) > 0 {
		return SearchResponse{}, fmt.Errorf("%w: filters require a resource", ErrInvalidRequest)
	}

	if mode == SearchModeSemantic && resource == "" {
		return SearchResponse{}, fmt.Errorf("%w: semantic mode requires a resource", ErrInvalidRequest)
	}

	limit := req.Limit
	if limit <= 0 {
		limit = s.cfg.DefaultLimit
	}
	if limit > s.cfg.MaxLimit {
		limit = s.cfg.MaxLimit
	}

	qHash := queryHash(query)
	fHash := filtersHash(filters)
	var searchAfter []any
	if req.Cursor != "" {
		decoded, err := DecodeCursor(req.Cursor)
		if err != nil {
			return SearchResponse{}, err
		}
		if err := ValidateCursor(decoded, org, qHash, mode, resource, fHash, limit); err != nil {
			log.Error().Err(err).Str("searchmode", mode).Str("cursor", req.Cursor).Msg("invalid cursor")
			return SearchResponse{}, err
		}
		searchAfter = decoded.SearchAfter
	}

	var (
		indices         []string
		resourceByIndex map[string]string
		searchFields    []string
		semanticFields  []string
		filterQuery     map[string][]string
	)

	if resource != "" {
		indexRef, err := s.resolver.ResolveIndex(ctx, org, resource)
		if err != nil {
			log.Error().Err(err).Str("searchmode", mode).Str("org", org).Str("resource", resource).Msg("failed to resolve search index")
			return SearchResponse{}, fmt.Errorf("%w: resolve search index: %v", ErrBackend, err)
		}

		if err := validateFiltersAllowed(filters, indexRef.FilterableFields); err != nil {
			return SearchResponse{}, err
		}
		if mode == SearchModeSemantic && len(indexRef.SemanticFields) == 0 {
			return SearchResponse{}, fmt.Errorf("%w: resource %q has no semantic fields configured", ErrInvalidRequest, resource)
		}

		indices = []string{indexRef.IndexName}
		resourceByIndex = map[string]string{indexRef.IndexName: indexRef.Resource}
		searchFields = searchableFields(indexRef.DefaultFields)
		semanticFields = semanticSearchFields(indexRef.SemanticFields)
		filterQuery = filters
	} else {
		indexRefs, err := s.resolver.ListIndices(ctx, org)
		if err != nil {
			log.Error().Err(err).Str("searchmode", mode).Str("org", org).Msg("failed to list search indices")
			return SearchResponse{}, fmt.Errorf("%w: list search indices: %v", ErrBackend, err)
		}

		indices, resourceByIndex = indexLookup(indexRefs)
		if len(indices) == 0 {
			return SearchResponse{}, fmt.Errorf("%w: no active search indices for org %q", ErrBackend, org)
		}
		searchFields = searchableFieldsForRefs(indexRefs)
	}

	log.Debug().
		Str("searchmode", mode).
		Str("organization", org).
		Str("queryHash", qHash).
		Int("limit", limit).
		Int("indexCount", len(indices)).
		Str("resource", resource).
		Msg("starting search")

	results := make([]SearchHit, 0, limit)
	var nextSearchAfter []any
	var totalScanned int
	var exhausted bool

outer:
	for len(results) < limit {
		page, err := s.searcher.Search(ctx, OpenSearchQuery{
			Indices:        indices,
			Query:          query,
			Mode:           mode,
			Fields:         searchFields,
			SemanticFields: semanticFields,
			Filters:        filterQuery,
			Size:           s.cfg.FetchBatchSize,
			SearchAfter:    searchAfter,
		})
		s.metrics.AddOpenSearchCalls(1)
		if err != nil {
			log.Error().Err(err).Msg("failed to query OpenSearch")
			return SearchResponse{}, fmt.Errorf("%w: query OpenSearch: %v", ErrBackend, err)
		}
		if len(page.Hits) == 0 {
			exhausted = true
			break
		}

		authz, err := s.authorizer.FilterAuthorized(ctx, AuthorizationRequest{
			Organization: org,
			User:         user,
			Relation:     "get",
			Hits:         page.Hits,
		})
		if err != nil {
			log.Error().
				Err(err).
				Str("searchmode", mode).
				Str("organization", org).
				Str("queryHash", qHash).
				Str("resource", resource).
				Int("scannedHits", totalScanned).
				Msg("failed to authorize search hits with OpenFGA")
			return SearchResponse{}, fmt.Errorf("%w: filter authorization: %v", ErrBackend, err)
		}
		s.metrics.AddOpenFGACalls(authz.Calls)
		s.metrics.AddDroppedMissingContext(authz.DroppedMissingContext)
		s.metrics.AddAuthDenied(authz.Denied)

		for i, hit := range page.Hits {
			totalScanned++

			if totalScanned > s.cfg.MaxScannedHits {
				break outer
			}
			nextSearchAfter = hit.Sort

			if i >= len(authz.Allowed) || !authz.Allowed[i] {
				log.Warn().Str("searchmode", mode).Str("organization", org).Str("queryHash", qHash).Str("resource", resource).Int("hitIndex", i).Msg("skipping unauthorized search hit")
				continue
			}

			results = append(results, mapHit(hit, resolveHitResource(hit, resource, resourceByIndex)))
			if len(results) == limit {
				break outer
			}
		}

		if len(page.Hits) < s.cfg.FetchBatchSize {
			exhausted = true
			break
		}
		searchAfter = page.Hits[len(page.Hits)-1].Sort
	}

	var nextCursor *string
	if !exhausted && len(nextSearchAfter) > 0 {
		cursor, err := EncodeCursor(CursorState{
			Version:     cursorVersion,
			Org:         org,
			QueryHash:   qHash,
			Mode:        mode,
			Resource:    resource,
			FiltersHash: fHash,
			Limit:       limit,
			SearchAfter: nextSearchAfter,
		})
		if err != nil {
			log.Error().Err(err).Msg("failed to encode cursor")
			return SearchResponse{}, err
		}
		nextCursor = &cursor
	}

	return SearchResponse{Results: results, NextCursor: nextCursor}, nil
}

func (s *Service) ListResources(ctx context.Context, req SearchResourcesRequest) (SearchResourcesResponse, error) {
	org := strings.TrimSpace(req.Organization)
	if org == "" {
		return SearchResourcesResponse{}, fmt.Errorf("%w: organization is required", ErrInvalidRequest)
	}

	refs, err := s.resolver.ListIndices(ctx, org)
	if err != nil {
		return SearchResourcesResponse{}, fmt.Errorf("%w: list search indices: %v", ErrBackend, err)
	}

	resources := make([]SearchResource, 0, len(refs))
	byResource := make(map[string]SearchResource, len(refs))
	for _, ref := range refs {
		resource := strings.TrimSpace(ref.Resource)
		if resource == "" {
			continue
		}
		byResource[resource] = SearchResource{
			Resource:         resource,
			DefaultFields:    dedupeNonEmpty(ref.DefaultFields),
			FilterableFields: dedupeNonEmpty(ref.FilterableFields),
			SemanticFields:   dedupeNonEmpty(ref.SemanticFields),
		}
	}

	keys := slices.Sorted(maps.Keys(byResource))
	for _, resource := range keys {
		resources = append(resources, byResource[resource])
	}

	return SearchResourcesResponse{Resources: resources}, nil
}

func (s *Service) FilterValues(ctx context.Context, req FilterValuesRequest) (FilterValuesResponse, error) {
	start := time.Now()
	s.metrics.IncSearchRequests()
	defer func() { s.metrics.ObserveSearchDuration(time.Since(start)) }()

	org := strings.TrimSpace(req.Organization)
	if org == "" {
		return FilterValuesResponse{}, fmt.Errorf("%w: organization is required", ErrInvalidRequest)
	}
	user := strings.TrimSpace(req.User)
	if user == "" {
		return FilterValuesResponse{}, fmt.Errorf("%w: user is required", ErrInvalidRequest)
	}
	resource := strings.TrimSpace(req.Resource)
	if resource == "" {
		return FilterValuesResponse{}, fmt.Errorf("%w: resource is required", ErrInvalidRequest)
	}
	field := strings.TrimSpace(req.Field)
	if field == "" {
		return FilterValuesResponse{}, fmt.Errorf("%w: field is required", ErrInvalidRequest)
	}

	limit := req.Limit
	if limit <= 0 {
		limit = s.cfg.DefaultLimit
	}
	if limit > s.cfg.MaxLimit {
		limit = s.cfg.MaxLimit
	}

	indexRef, err := s.resolver.ResolveIndex(ctx, org, resource)
	if err != nil {
		return FilterValuesResponse{}, fmt.Errorf("%w: resolve search index: %v", ErrBackend, err)
	}

	allowed := fieldSet(indexRef.FilterableFields)
	if _, ok := allowed[field]; !ok {
		return FilterValuesResponse{}, fmt.Errorf("%w: field %q is not filterable for resource %q", ErrInvalidRequest, field, resource)
	}

	filters := normalizeFilters(req.Filters)
	if err := validateFiltersAllowed(filters, indexRef.FilterableFields); err != nil {
		return FilterValuesResponse{}, err
	}

	query := strings.TrimSpace(req.Query)
	searchFields := searchableFields(indexRef.DefaultFields)

	searchAfter := []any(nil)
	totalScanned := 0
	seen := make(map[string]struct{}, limit)
	values := make([]string, 0, limit)

outer:
	for len(values) < limit {
		page, err := s.searcher.Search(ctx, OpenSearchQuery{
			Indices:     []string{indexRef.IndexName},
			Query:       query,
			Fields:      searchFields,
			Filters:     filters,
			Size:        s.cfg.FetchBatchSize,
			SearchAfter: searchAfter,
		})
		s.metrics.AddOpenSearchCalls(1)
		if err != nil {
			return FilterValuesResponse{}, fmt.Errorf("%w: query OpenSearch: %v", ErrBackend, err)
		}
		if len(page.Hits) == 0 {
			break
		}

		authz, err := s.authorizer.FilterAuthorized(ctx, AuthorizationRequest{
			Organization: org,
			User:         user,
			Relation:     "get",
			Hits:         page.Hits,
		})
		if err != nil {
			return FilterValuesResponse{}, fmt.Errorf("%w: filter authorization: %v", ErrBackend, err)
		}
		s.metrics.AddOpenFGACalls(authz.Calls)
		s.metrics.AddDroppedMissingContext(authz.DroppedMissingContext)
		s.metrics.AddAuthDenied(authz.Denied)

		for i, hit := range page.Hits {
			totalScanned++
			if totalScanned > s.cfg.MaxScannedHits {
				break outer
			}
			if i >= len(authz.Allowed) || !authz.Allowed[i] {
				continue
			}
			for _, value := range extractFieldValues(hit.Source, prefixedDocumentField("filterable_fields", field)) {
				if _, exists := seen[value]; exists {
					continue
				}
				seen[value] = struct{}{}
				values = append(values, value)
				if len(values) >= limit {
					break outer
				}
			}
		}

		if len(page.Hits) < s.cfg.FetchBatchSize {
			break
		}

		searchAfter = page.Hits[len(page.Hits)-1].Sort
	}

	slices.Sort(values)
	if len(values) > limit {
		values = values[:limit]
	}

	return FilterValuesResponse{Values: values}, nil
}

func mapHit(hit OpenSearchHit, resource string) SearchHit {
	src := responseSource(hit.Source)

	return SearchHit{
		ID:               firstString(hit.ID, stringFromMap(src, "id")),
		Score:            hit.Score,
		Resource:         resource,
		Kind:             stringFromMap(src, "kind"),
		Name:             stringFromMap(src, "name"),
		Namespace:        stringFromMap(src, "namespace"),
		APIGroup:         stringFromMap(src, "api_group"),
		APIVersion:       stringFromMap(src, "api_version"),
		WorkspacePath:    stringFromMap(src, "workspace_path"),
		ClusterName:      stringFromMap(src, "cluster_name"),
		OrganizationID:   stringFromMap(src, "organization_id"),
		OrganizationName: stringFromMap(src, "organization_name"),
		AccountID:        stringFromMap(src, "account_id"),
		AccountName:      stringFromMap(src, "account_name"),
		Source:           src,
	}
}

func responseSource(source map[string]any) map[string]any {
	if len(source) == 0 {
		return map[string]any{}
	}

	out := make(map[string]any, len(source))
	maps.Copy(out, source)

	customFields, ok := out["custom_fields"].(map[string]any)
	if !ok {
		return out
	}

	out["custom_fields"] = flattenMap(customFields)
	return out
}

func flattenMap(values map[string]any) map[string]any {
	out := make(map[string]any)
	flattenValue(out, "", values)
	return out
}

func flattenValue(out map[string]any, prefix string, value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			flattenValue(out, joinFieldPath(prefix, key), nested)
		}
	case map[string]string:
		for key, nested := range typed {
			flattenValue(out, joinFieldPath(prefix, key), nested)
		}
	default:
		if prefix != "" {
			out[prefix] = value
		}
	}
}

func joinFieldPath(prefix, field string) string {
	field = strings.TrimSpace(field)
	if field == "" {
		return prefix
	}
	if prefix == "" {
		return field
	}
	return prefix + "." + field
}

func indexLookup(refs []SearchIndexRef) ([]string, map[string]string) {
	indices := make([]string, 0, len(refs))
	resourceByIndex := make(map[string]string, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		indexName := strings.TrimSpace(ref.IndexName)
		if indexName == "" {
			continue
		}
		if _, ok := seen[indexName]; !ok {
			indices = append(indices, indexName)
			seen[indexName] = struct{}{}
		}
		if resource := strings.TrimSpace(ref.Resource); resource != "" {
			resourceByIndex[indexName] = resource
		}
	}
	slices.Sort(indices)
	return indices, resourceByIndex
}

func resolveHitResource(hit OpenSearchHit, requestedResource string, byIndex map[string]string) string {
	if resource := strings.TrimSpace(requestedResource); resource != "" {
		return resource
	}
	if resource := strings.TrimSpace(byIndex[hit.Index]); resource != "" {
		return resource
	}

	return strings.TrimSpace(stringFromMap(hit.Source, "resource"))
}

func searchableFields(defaultFields []string) []string {
	return dedupeNonEmpty(defaultFields)
}

func searchableFieldsForRefs(refs []SearchIndexRef) []string {
	fields := make([]string, 0, len(refs)*2)
	for _, ref := range refs {
		fields = append(fields, ref.DefaultFields...)
	}

	return dedupeNonEmpty(fields)
}

func semanticSearchFields(fields []string) []string {
	return dedupeNonEmpty(fields)
}

func normalizeFilters(filters map[string][]string) map[string][]string {
	if len(filters) == 0 {
		return nil
	}

	normalized := make(map[string][]string, len(filters))
	for field, values := range filters {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}

		clean := make([]string, 0, len(values))
		seen := make(map[string]struct{}, len(values))
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			clean = append(clean, value)
		}
		if len(clean) == 0 {
			continue
		}
		normalized[field] = clean
	}

	if len(normalized) == 0 {
		return nil
	}

	return normalized
}

func validateFiltersAllowed(filters map[string][]string, allowedFields []string) error {
	if len(filters) == 0 {
		return nil
	}
	allowed := fieldSet(allowedFields)
	for field := range filters {
		if _, ok := allowed[field]; !ok {
			return fmt.Errorf("%w: field %q is not filterable", ErrInvalidRequest, field)
		}
	}

	return nil
}

func fieldSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			out[trimmed] = struct{}{}
		}
	}

	return out
}

func dedupeNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	slices.Sort(out)

	return out
}

func prefixedDocumentField(prefix, field string) string {
	field = strings.TrimSpace(field)
	if field == "" {
		return ""
	}
	for _, existingPrefix := range []string{"custom_fields.", "default_fields.", "semantic_fields.", "filterable_fields."} {
		if strings.HasPrefix(field, existingPrefix) {
			return field
		}
	}
	return prefix + "." + field
}

func extractFieldValues(source map[string]any, fieldPath string) []string {
	if len(source) == 0 {
		return nil
	}
	parts := strings.Split(strings.TrimSpace(fieldPath), ".")
	if len(parts) == 0 {
		return nil
	}

	results := make(map[string]struct{})
	collectFieldValues(source, parts, results)
	if len(results) == 0 {
		return nil
	}

	values := slices.Sorted(maps.Keys(results))
	return values
}

func collectFieldValues(current any, parts []string, out map[string]struct{}) {
	if len(parts) == 0 {
		collectScalarValues(current, out)
		return
	}

	switch typed := current.(type) {
	case map[string]any:
		next, ok := typed[parts[0]]
		if !ok {
			return
		}
		collectFieldValues(next, parts[1:], out)
	case []any:
		for _, item := range typed {
			collectFieldValues(item, parts, out)
		}
	}
}

func collectScalarValues(value any, out map[string]struct{}) {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed != "" {
			out[trimmed] = struct{}{}
		}
	case []any:
		for _, item := range typed {
			collectScalarValues(item, out)
		}
	case fmt.Stringer:
		trimmed := strings.TrimSpace(typed.String())
		if trimmed != "" {
			out[trimmed] = struct{}{}
		}
	case bool:
		out[fmt.Sprintf("%t", typed)] = struct{}{}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		out[fmt.Sprintf("%v", typed)] = struct{}{}
	}
}

func stringFromMap(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func firstString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}
