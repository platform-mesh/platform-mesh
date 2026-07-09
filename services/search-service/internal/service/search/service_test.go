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
	"errors"
	"testing"
)

type fakeResolver struct {
	index   SearchIndexRef
	indices []SearchIndexRef
	err     error
}

func (f fakeResolver) ResolveIndex(ctx context.Context, org, resource string) (SearchIndexRef, error) {
	return f.index, f.err
}

func (f fakeResolver) ListIndices(ctx context.Context, org string) ([]SearchIndexRef, error) {
	if len(f.indices) > 0 {
		return f.indices, f.err
	}
	if f.index.IndexName != "" {
		return []SearchIndexRef{f.index}, f.err
	}
	return nil, f.err
}

type fakeSearcher struct {
	pages []OpenSearchPage
	calls int
	reqs  []OpenSearchQuery
}

func (f *fakeSearcher) Search(ctx context.Context, req OpenSearchQuery) (OpenSearchPage, error) {
	f.reqs = append(f.reqs, req)
	if f.calls >= len(f.pages) {
		return OpenSearchPage{}, nil
	}
	page := f.pages[f.calls]
	f.calls++
	return page, nil
}

type fakeAuthorizer struct {
	results []AuthorizationResult
	calls   int
}

func (f *fakeAuthorizer) FilterAuthorized(ctx context.Context, req AuthorizationRequest) (AuthorizationResult, error) {
	if f.calls >= len(f.results) {
		return AuthorizationResult{Allowed: make([]bool, len(req.Hits))}, nil
	}
	res := f.results[f.calls]
	f.calls++
	return res, nil
}

func TestSearchFillsAuthorizedPageAcrossBatches(t *testing.T) {
	searcher := &fakeSearcher{pages: []OpenSearchPage{
		{Hits: []OpenSearchHit{
			{ID: "1", Score: 1, Sort: []any{1.0, "1"}, Source: map[string]any{"id": "1"}},
			{ID: "2", Score: 1, Sort: []any{0.9, "2"}, Source: map[string]any{"id": "2"}},
		}},
		{Hits: []OpenSearchHit{
			{ID: "3", Score: 1, Sort: []any{0.8, "3"}, Source: map[string]any{"id": "3"}},
			{ID: "4", Score: 1, Sort: []any{0.7, "4"}, Source: map[string]any{"id": "4"}},
		}},
	}}
	authorizer := &fakeAuthorizer{results: []AuthorizationResult{
		{Allowed: []bool{false, true}, Denied: 1, Calls: 1},
		{Allowed: []bool{true, false}, Denied: 1, Calls: 1},
	}}

	svc := NewService(
		fakeResolver{index: SearchIndexRef{IndexName: "idx-acme"}},
		searcher,
		authorizer,
		nil,
		ServiceConfig{DefaultLimit: 20, MaxLimit: 100, FetchBatchSize: 2, MaxScannedHits: 1000},
	)

	resp, err := svc.Search(context.Background(), SearchRequest{Organization: "acme", User: "alice@example.com", Query: "foo", Limit: 2})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}
	if resp.NextCursor == nil {
		t.Fatalf("expected non-nil next cursor")
	}
}

func TestSearchInvalidCursor(t *testing.T) {
	svc := NewService(
		fakeResolver{index: SearchIndexRef{IndexName: "idx"}},
		&fakeSearcher{},
		&fakeAuthorizer{},
		nil,
		ServiceConfig{},
	)

	_, err := svc.Search(context.Background(), SearchRequest{
		Organization: "acme",
		User:         "alice@example.com",
		Query:        "foo",
		Limit:        20,
		Cursor:       "not-a-cursor",
	})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("expected ErrInvalidCursor, got %v", err)
	}
}

func TestSearchDefaultsMissingQueryToWildcard(t *testing.T) {
	searcher := &fakeSearcher{pages: []OpenSearchPage{
		{Hits: []OpenSearchHit{
			{ID: "1", Score: 1, Sort: []any{1.0, "1"}, Source: map[string]any{"id": "1"}},
		}},
	}}
	authorizer := &fakeAuthorizer{results: []AuthorizationResult{
		{Allowed: []bool{true}, Calls: 1},
	}}

	svc := NewService(
		fakeResolver{index: SearchIndexRef{IndexName: "idx-acme"}},
		searcher,
		authorizer,
		nil,
		ServiceConfig{FetchBatchSize: 10, MaxScannedHits: 100},
	)

	_, err := svc.Search(context.Background(), SearchRequest{Organization: "acme", User: "alice@example.com", Query: "  "})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(searcher.reqs) != 1 {
		t.Fatalf("expected 1 OpenSearch request, got %d", len(searcher.reqs))
	}
	if searcher.reqs[0].Query != "*" {
		t.Fatalf("expected wildcard query, got %q", searcher.reqs[0].Query)
	}
}

func TestSearchSemanticModeUsesSemanticFields(t *testing.T) {
	searcher := &fakeSearcher{pages: []OpenSearchPage{
		{Hits: []OpenSearchHit{
			{ID: "1", Score: 1, Sort: []any{1.0, "1"}, Source: map[string]any{"id": "1"}},
		}},
	}}
	authorizer := &fakeAuthorizer{results: []AuthorizationResult{
		{Allowed: []bool{true}, Calls: 1},
	}}

	svc := NewService(
		fakeResolver{index: SearchIndexRef{
			IndexName:      "idx-acme",
			Resource:       "components",
			SemanticFields: []string{"description", "spec.summary"},
		}},
		searcher,
		authorizer,
		nil,
		ServiceConfig{FetchBatchSize: 10, MaxScannedHits: 100},
	)

	resp, err := svc.Search(context.Background(), SearchRequest{
		Organization: "acme",
		User:         "alice@example.com",
		Query:        "foo",
		Mode:         SearchModeSemantic,
		Resource:     "components",
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if len(searcher.reqs) != 1 {
		t.Fatalf("expected 1 OpenSearch request, got %d", len(searcher.reqs))
	}
	if searcher.reqs[0].Mode != SearchModeSemantic {
		t.Fatalf("expected semantic mode, got %q", searcher.reqs[0].Mode)
	}
	if len(searcher.reqs[0].SemanticFields) != 2 {
		t.Fatalf("expected semantic fields to be forwarded, got %+v", searcher.reqs[0].SemanticFields)
	}
}

func TestSearchSemanticModeRequiresResource(t *testing.T) {
	svc := NewService(fakeResolver{}, &fakeSearcher{}, &fakeAuthorizer{}, nil, ServiceConfig{})
	_, err := svc.Search(context.Background(), SearchRequest{
		Organization: "acme",
		User:         "alice@example.com",
		Query:        "foo",
		Mode:         SearchModeSemantic,
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestSearchSemanticModeRequiresConfiguredFields(t *testing.T) {
	svc := NewService(
		fakeResolver{index: SearchIndexRef{IndexName: "idx-acme", Resource: "components"}},
		&fakeSearcher{},
		&fakeAuthorizer{},
		nil,
		ServiceConfig{},
	)

	_, err := svc.Search(context.Background(), SearchRequest{
		Organization: "acme",
		User:         "alice@example.com",
		Query:        "foo",
		Mode:         SearchModeSemantic,
		Resource:     "components",
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestSearchClampsLimitToConfiguredMax(t *testing.T) {
	searcher := &fakeSearcher{pages: []OpenSearchPage{
		{Hits: []OpenSearchHit{
			{ID: "1", Score: 1, Sort: []any{1.0, "1"}, Source: map[string]any{"id": "1"}},
		}},
		{Hits: []OpenSearchHit{
			{ID: "2", Score: 1, Sort: []any{0.9, "2"}, Source: map[string]any{"id": "2"}},
		}},
	}}
	authorizer := &fakeAuthorizer{results: []AuthorizationResult{
		{Allowed: []bool{true}, Calls: 1},
		{Allowed: []bool{true}, Calls: 1},
	}}

	svc := NewService(
		fakeResolver{index: SearchIndexRef{IndexName: "idx-acme"}},
		searcher,
		authorizer,
		nil,
		ServiceConfig{DefaultLimit: 20, MaxLimit: 100, FetchBatchSize: 1, MaxScannedHits: 1},
	)

	resp, err := svc.Search(context.Background(), SearchRequest{
		Organization: "acme",
		User:         "alice@example.com",
		Query:        "foo",
		Limit:        500,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if resp.NextCursor == nil {
		t.Fatalf("expected next cursor when scan cap is reached")
	}

	decoded, err := DecodeCursor(*resp.NextCursor)
	if err != nil {
		t.Fatalf("decode next cursor: %v", err)
	}
	if decoded.Limit != 100 {
		t.Fatalf("expected clamped limit 100, got %d", decoded.Limit)
	}
}

func TestFilterValuesPostFiltersAndEnforcesLimit(t *testing.T) {
	searcher := &fakeSearcher{pages: []OpenSearchPage{
		{Hits: []OpenSearchHit{
			{ID: "1", Source: map[string]any{"filterable_fields": map[string]any{"status": "Terminated"}}},
			{ID: "2", Source: map[string]any{"filterable_fields": map[string]any{"status": "Active"}}},
			{ID: "3", Source: map[string]any{"filterable_fields": map[string]any{"status": "Pending"}}},
		}},
	}}

	svc := NewService(
		fakeResolver{index: SearchIndexRef{
			IndexName:        "idx",
			FilterableFields: []string{"status"},
		}},
		searcher,
		&fakeAuthorizer{results: []AuthorizationResult{
			{Allowed: []bool{false, true, true}, Calls: 1, Denied: 1},
		}},
		nil,
		ServiceConfig{FetchBatchSize: 10, MaxScannedHits: 100},
	)

	resp, err := svc.FilterValues(context.Background(), FilterValuesRequest{
		Organization: "acme",
		User:         "alice@example.com",
		Resource:     "pods",
		Field:        "status",
		Limit:        1,
	})
	if err != nil {
		t.Fatalf("FilterValues returned error: %v", err)
	}

	if len(resp.Values) != 1 {
		t.Fatalf("expected 1 value, got %d", len(resp.Values))
	}
	if resp.Values[0] != "Active" {
		t.Fatalf("unexpected value: %s", resp.Values[0])
	}
}

func TestSearchForwardsConfiguredFieldsWithoutCoreFallback(t *testing.T) {
	searcher := &fakeSearcher{pages: []OpenSearchPage{
		{Hits: []OpenSearchHit{
			{ID: "1", Score: 1, Sort: []any{1.0, "1"}, Source: map[string]any{"id": "1"}},
		}},
	}}
	authorizer := &fakeAuthorizer{results: []AuthorizationResult{
		{Allowed: []bool{true}, Calls: 1},
	}}

	svc := NewService(
		fakeResolver{index: SearchIndexRef{
			IndexName:     "idx-acme",
			Resource:      "components",
			DefaultFields: []string{"spec.summary", "metadata.labels.team"},
		}},
		searcher,
		authorizer,
		nil,
		ServiceConfig{FetchBatchSize: 10, MaxScannedHits: 100},
	)

	_, err := svc.Search(context.Background(), SearchRequest{
		Organization: "acme",
		User:         "alice@example.com",
		Query:        "foo",
		Resource:     "components",
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(searcher.reqs) != 1 {
		t.Fatalf("expected 1 OpenSearch request, got %d", len(searcher.reqs))
	}
	fields := searcher.reqs[0].Fields
	if len(fields) != 2 || fields[0] != "metadata.labels.team" || fields[1] != "spec.summary" {
		t.Fatalf("unexpected forwarded fields: %v", fields)
	}
}

func TestFilterValuesReadsNestedFilterableFields(t *testing.T) {
	searcher := &fakeSearcher{pages: []OpenSearchPage{
		{Hits: []OpenSearchHit{
			{ID: "1", Source: map[string]any{"filterable_fields": map[string]any{"spec": map[string]any{"replicas": float64(3)}}}},
			{ID: "2", Source: map[string]any{"filterable_fields": map[string]any{"spec": map[string]any{"replicas": float64(5)}}}},
		}},
	}}

	svc := NewService(
		fakeResolver{index: SearchIndexRef{IndexName: "idx", FilterableFields: []string{"spec.replicas"}}},
		searcher,
		&fakeAuthorizer{results: []AuthorizationResult{{Allowed: []bool{true, true}, Calls: 1}}},
		nil,
		ServiceConfig{FetchBatchSize: 10, MaxScannedHits: 100},
	)

	resp, err := svc.FilterValues(context.Background(), FilterValuesRequest{Organization: "acme", User: "alice@example.com", Resource: "pods", Field: "spec.replicas", Limit: 10})
	if err != nil {
		t.Fatalf("FilterValues returned error: %v", err)
	}
	if len(resp.Values) != 2 || resp.Values[0] != "3" || resp.Values[1] != "5" {
		t.Fatalf("unexpected values: %v", resp.Values)
	}
}

func TestFilterValuesRejectsMissingUser(t *testing.T) {
	svc := NewService(
		fakeResolver{index: SearchIndexRef{IndexName: "idx", FilterableFields: []string{"status"}}},
		&fakeSearcher{},
		&fakeAuthorizer{},
		nil,
		ServiceConfig{},
	)

	_, err := svc.FilterValues(context.Background(), FilterValuesRequest{
		Organization: "acme",
		Resource:     "pods",
		Field:        "status",
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestSearchFlattensCustomFieldsInResponse(t *testing.T) {
	searcher := &fakeSearcher{pages: []OpenSearchPage{
		{Hits: []OpenSearchHit{
			{
				ID:    "1",
				Score: 1,
				Sort:  []any{1.0, "1"},
				Source: map[string]any{
					"id": "1",
					"custom_fields": map[string]any{
						"apiVersion": "metadata.dxp.sap.com/v1alpha1",
						"kind":       "Component",
						"metadata": map[string]any{
							"annotations": map[string]any{
								"kcp.io/cluster":                         "ecvrp5ijg9ufrmnl",
								"migration.platform-mesh.io/source-name": "compo-docs3",
							},
							"finalizers": []any{"search.platform-mesh.io/indexable-resource"},
						},
					},
				},
			},
		}},
	}}
	authorizer := &fakeAuthorizer{results: []AuthorizationResult{
		{Allowed: []bool{true}, Calls: 1},
	}}

	svc := NewService(
		fakeResolver{index: SearchIndexRef{IndexName: "idx-acme"}},
		searcher,
		authorizer,
		nil,
		ServiceConfig{FetchBatchSize: 10, MaxScannedHits: 100},
	)

	resp, err := svc.Search(context.Background(), SearchRequest{Organization: "acme", User: "alice@example.com", Query: "component"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}

	customFields, ok := resp.Results[0].Source["custom_fields"].(map[string]any)
	if !ok {
		t.Fatalf("expected flattened custom_fields map, got %T", resp.Results[0].Source["custom_fields"])
	}
	if got := customFields["apiVersion"]; got != "metadata.dxp.sap.com/v1alpha1" {
		t.Fatalf("apiVersion = %v", got)
	}
	if got := customFields["metadata.annotations.kcp.io/cluster"]; got != "ecvrp5ijg9ufrmnl" {
		t.Fatalf("metadata.annotations.kcp.io/cluster = %v", got)
	}
	if got := customFields["metadata.annotations.migration.platform-mesh.io/source-name"]; got != "compo-docs3" {
		t.Fatalf("metadata.annotations.migration.platform-mesh.io/source-name = %v", got)
	}
	if _, exists := customFields["metadata"]; exists {
		t.Fatalf("nested metadata object should not be present in flattened custom_fields: %v", customFields["metadata"])
	}
}
