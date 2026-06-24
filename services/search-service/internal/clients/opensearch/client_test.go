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

package opensearch

import (
	"encoding/json"
	"testing"

	"go.platform-mesh.io/search-service/internal/service/search"
)

func TestBuildQueryBodyWithoutSearchAfter(t *testing.T) {
	body, err := BuildQueryBody(search.OpenSearchQuery{
		Query:  "hello",
		Fields: []string{"name", "description"},
		Size:   20,
	})
	if err != nil {
		t.Fatalf("BuildQueryBody returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	if payload["size"].(float64) != 20 {
		t.Fatalf("unexpected size: %v", payload["size"])
	}
	if _, ok := payload["search_after"]; ok {
		t.Fatalf("search_after should not be set")
	}

	sort := payload["sort"].([]any)
	if len(sort) != 3 {
		t.Fatalf("expected 3 sort fields")
	}
}

func TestBuildQueryBodyWithSearchAfter(t *testing.T) {
	body, err := BuildQueryBody(search.OpenSearchQuery{
		Query:       "hello",
		Fields:      []string{"name"},
		Size:        10,
		SearchAfter: []any{1.0, "id-1", "idx"},
		Filters: map[string][]string{
			"status": {"Ready"},
		},
	})
	if err != nil {
		t.Fatalf("BuildQueryBody returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	searchAfter := payload["search_after"].([]any)
	if len(searchAfter) != 3 {
		t.Fatalf("expected 3 search_after values")
	}

	query := payload["query"].(map[string]any)
	boolQuery := query["bool"].(map[string]any)
	if _, ok := boolQuery["filter"]; !ok {
		t.Fatalf("expected filter clause")
	}
}

func TestBuildQueryBodyWithoutQueryUsesMatchAll(t *testing.T) {
	body, err := BuildQueryBody(search.OpenSearchQuery{
		Query: "",
		Size:  5,
	})
	if err != nil {
		t.Fatalf("BuildQueryBody returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	query := payload["query"].(map[string]any)
	if _, ok := query["match_all"]; !ok {
		t.Fatalf("expected match_all query")
	}
}
