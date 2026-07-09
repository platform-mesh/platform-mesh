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

	query := payload["query"].(map[string]any)
	simple := query["simple_query_string"].(map[string]any)
	fields := simple["fields"].([]any)
	if fields[0] != "account_name" || fields[1] != "api_group" {
		t.Fatalf("expected default lexical fields first, got %v", fields)
	}
	if !containsField(fields, "custom_fields.description") || !containsField(fields, "custom_fields.name") {
		t.Fatalf("unexpected search fields: %v", fields)
	}
	if !containsField(fields, "payload_text") {
		t.Fatalf("expected payload_text search field, got %v", fields)
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
	filter := boolQuery["filter"].([]any)
	terms := filter[0].(map[string]any)["terms"].(map[string]any)
	if _, ok := terms["filterable_fields.status"]; !ok {
		t.Fatalf("expected filterable_fields.status filter, got %v", terms)
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

func TestBuildQueryBodySemanticSingleField(t *testing.T) {
	body, err := BuildQueryBody(search.OpenSearchQuery{
		Query:          "hello",
		Mode:           search.SearchModeSemantic,
		SemanticFields: []string{"description"},
		Size:           20,
	})
	if err != nil {
		t.Fatalf("BuildQueryBody returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	query := payload["query"].(map[string]any)
	neural := query["neural"].(map[string]any)
	description := neural["semantic_fields.description"].(map[string]any)
	if got := description["query_text"]; got != "hello" {
		t.Fatalf("unexpected query_text: %v", got)
	}
}

func TestBuildQueryBodySemanticMultipleFieldsWithFilters(t *testing.T) {
	body, err := BuildQueryBody(search.OpenSearchQuery{
		Query:          "hello",
		Mode:           search.SearchModeSemantic,
		SemanticFields: []string{"description", "spec.summary"},
		Filters: map[string][]string{
			"status": {"Ready"},
		},
		Size: 10,
	})
	if err != nil {
		t.Fatalf("BuildQueryBody returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	query := payload["query"].(map[string]any)
	boolQuery := query["bool"].(map[string]any)
	must := boolQuery["must"].([]any)
	innerBool := must[0].(map[string]any)["bool"].(map[string]any)
	should := innerBool["should"].([]any)
	if len(should) != 2 {
		t.Fatalf("expected 2 semantic should clauses, got %d", len(should))
	}
	if _, ok := boolQuery["filter"]; !ok {
		t.Fatalf("expected filter clause")
	}
}

func TestBuildQueryBodyAggregationUsesFilterableFieldPrefix(t *testing.T) {
	body, err := BuildQueryBody(search.OpenSearchQuery{
		AggregationField: "spec.replicas",
		Size:             0,
	})
	if err != nil {
		t.Fatalf("BuildQueryBody returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	aggs := payload["aggs"].(map[string]any)
	values := aggs["values"].(map[string]any)
	terms := values["terms"].(map[string]any)
	if got := terms["field"]; got != "filterable_fields.spec.replicas" {
		t.Fatalf("aggregation field = %v, want filterable_fields.spec.replicas", got)
	}
	if got := payload["size"]; got != float64(0) {
		t.Fatalf("size = %v, want 0", got)
	}
}

func containsField(fields []any, want string) bool {
	for _, field := range fields {
		if field == want {
			return true
		}
	}
	return false
}
