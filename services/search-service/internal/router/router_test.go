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

package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appcontext "go.platform-mesh.io/search-service/internal/context"
	"go.platform-mesh.io/search-service/internal/service/search"
)

type fakeSearchService struct {
	response search.SearchResponse
	err      error
	lastReq  search.SearchRequest

	resourcesResp search.SearchResourcesResponse
	resourcesErr  error
	lastResReq    search.SearchResourcesRequest

	filterValuesResp search.FilterValuesResponse
	filterValuesErr  error
	lastFilterReq    search.FilterValuesRequest
}

func (f *fakeSearchService) Search(ctx context.Context, req search.SearchRequest) (search.SearchResponse, error) {
	f.lastReq = req
	return f.response, f.err
}

func (f *fakeSearchService) ListResources(ctx context.Context, req search.SearchResourcesRequest) (search.SearchResourcesResponse, error) {
	f.lastResReq = req
	return f.resourcesResp, f.resourcesErr
}

func (f *fakeSearchService) FilterValues(ctx context.Context, req search.FilterValuesRequest) (search.FilterValuesResponse, error) {
	f.lastFilterReq = req
	return f.filterValuesResp, f.filterValuesErr
}

func withRequestContext(rc appcontext.RequestContext) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(appcontext.WithRequestContext(r.Context(), rc)))
		})
	}
}

func TestCreateRouterSearchSuccess(t *testing.T) {
	svc := &fakeSearchService{response: search.SearchResponse{Results: []search.SearchHit{{ID: "1", Score: 1, Source: map[string]any{"id": "1"}}}}}
	r := CreateRouter(svc, []func(http.Handler) http.Handler{withRequestContext(appcontext.RequestContext{Organization: "acme", User: "alice@example.com"})})

	req := httptest.NewRequest(http.MethodGet, "/rest/v1/search?q=hello&mode=semantic&limit=15&cursor=abc&resource=accounts&filter.status=Ready", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if svc.lastReq.Organization != "acme" || svc.lastReq.User != "alice@example.com" {
		t.Fatalf("unexpected request context: %+v", svc.lastReq)
	}
	if svc.lastReq.Query != "hello" || svc.lastReq.Mode != search.SearchModeSemantic || svc.lastReq.Limit != 15 || svc.lastReq.Cursor != "abc" || svc.lastReq.Resource != "accounts" {
		t.Fatalf("unexpected request payload: %+v", svc.lastReq)
	}
	if len(svc.lastReq.Filters["status"]) != 1 || svc.lastReq.Filters["status"][0] != "Ready" {
		t.Fatalf("unexpected filters: %+v", svc.lastReq.Filters)
	}

	var payload search.SearchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response is not valid json: %v", err)
	}
	if len(payload.Results) != 1 {
		t.Fatalf("expected one result")
	}
}

func TestCreateRouterSearchResponseContract(t *testing.T) {
	next := "opaque-cursor"
	svc := &fakeSearchService{
		response: search.SearchResponse{
			Results: []search.SearchHit{{
				ID:     "res-1",
				Score:  12.34,
				Kind:   "Component",
				Name:   "my-component",
				Source: map[string]any{"id": "res-1", "kind": "Component"},
			}},
			NextCursor: &next,
		},
	}
	r := CreateRouter(svc, []func(http.Handler) http.Handler{withRequestContext(appcontext.RequestContext{Organization: "acme", User: "alice@example.com"})})
	req := httptest.NewRequest(http.MethodGet, "/rest/v1/search?q=hello", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response is not valid json: %v", err)
	}

	if _, ok := payload["results"]; !ok {
		t.Fatalf("missing results field")
	}
	if _, ok := payload["nextCursor"]; !ok {
		t.Fatalf("missing nextCursor field")
	}

	results, ok := payload["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("expected results array with one element")
	}
	first, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("expected object result")
	}
	if _, ok := first["id"]; !ok {
		t.Fatalf("missing result id field")
	}
	if _, ok := first["score"]; !ok {
		t.Fatalf("missing result score field")
	}
	if _, ok := first["source"]; !ok {
		t.Fatalf("missing result source field")
	}
}

func TestCreateRouterMissingContextUnauthorized(t *testing.T) {
	svc := &fakeSearchService{}
	r := CreateRouter(svc, nil)
	req := httptest.NewRequest(http.MethodGet, "/rest/v1/search?q=hello", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestCreateRouterInvalidLimit(t *testing.T) {
	svc := &fakeSearchService{}
	r := CreateRouter(svc, []func(http.Handler) http.Handler{withRequestContext(appcontext.RequestContext{Organization: "acme", User: "alice@example.com"})})
	req := httptest.NewRequest(http.MethodGet, "/rest/v1/search?q=hello&limit=bad", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestCreateRouterResourcesEndpoint(t *testing.T) {
	svc := &fakeSearchService{
		resourcesResp: search.SearchResourcesResponse{
			Resources: []search.SearchResource{
				{Resource: "accounts", DefaultFields: []string{"name"}},
			},
		},
	}
	r := CreateRouter(svc, []func(http.Handler) http.Handler{withRequestContext(appcontext.RequestContext{Organization: "acme", User: "alice@example.com"})})
	req := httptest.NewRequest(http.MethodGet, "/rest/v1/search/resources", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if svc.lastResReq.Organization != "acme" {
		t.Fatalf("unexpected request: %+v", svc.lastResReq)
	}
}

func TestCreateRouterFilterValuesEndpoint(t *testing.T) {
	svc := &fakeSearchService{
		filterValuesResp: search.FilterValuesResponse{Values: []string{"Ready", "Pending"}},
	}
	r := CreateRouter(svc, []func(http.Handler) http.Handler{withRequestContext(appcontext.RequestContext{Organization: "acme", User: "alice@example.com"})})
	req := httptest.NewRequest(http.MethodGet, "/rest/v1/search/filter-values?resource=accounts&field=status&q=foo&filter.type=premium", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if svc.lastFilterReq.Organization != "acme" || svc.lastFilterReq.User != "alice@example.com" {
		t.Fatalf("unexpected request context: %+v", svc.lastFilterReq)
	}
	if svc.lastFilterReq.Resource != "accounts" || svc.lastFilterReq.Field != "status" {
		t.Fatalf("unexpected request payload: %+v", svc.lastFilterReq)
	}
	if len(svc.lastFilterReq.Filters["type"]) != 1 || svc.lastFilterReq.Filters["type"][0] != "premium" {
		t.Fatalf("unexpected filters: %+v", svc.lastFilterReq.Filters)
	}
}

func TestCreateRouterErrorMapping(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "invalid", err: search.ErrInvalidCursor, status: http.StatusBadRequest},
		{name: "unauthorized", err: search.ErrUnauthorized, status: http.StatusUnauthorized},
		{name: "forbidden", err: search.ErrForbidden, status: http.StatusForbidden},
		{name: "backend wrapped", err: fmt.Errorf("%w: opensearch", search.ErrBackend), status: http.StatusInternalServerError},
		{name: "backend", err: errors.New("boom"), status: http.StatusInternalServerError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeSearchService{err: tc.err}
			r := CreateRouter(svc, []func(http.Handler) http.Handler{withRequestContext(appcontext.RequestContext{Organization: "acme", User: "alice@example.com"})})
			req := httptest.NewRequest(http.MethodGet, "/rest/v1/search?q=hello", nil)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
			if rr.Code != tc.status {
				t.Fatalf("expected %d, got %d body=%s", tc.status, rr.Code, strings.TrimSpace(rr.Body.String()))
			}
		})
	}
}
