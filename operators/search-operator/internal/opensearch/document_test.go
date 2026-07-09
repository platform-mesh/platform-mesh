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
)

func TestDefaultIndexMappingIsValidJSON(t *testing.T) {
	mapping, err := DefaultIndexMapping(FieldMappings{}, "")
	if err != nil {
		t.Fatalf("DefaultIndexMapping() returned error: %v", err)
	}
	var js map[string]any
	if err := json.Unmarshal([]byte(mapping), &js); err != nil {
		t.Fatalf("DefaultIndexMapping() returned invalid JSON: %v\nMapping content:\n%s", err, mapping)
	}
}

func TestDefaultIndexMappingIncludesSemanticFields(t *testing.T) {
	mapping, err := DefaultIndexMapping(FieldMappings{Semantic: []string{"description", "spec.summary"}}, "model-123")
	if err != nil {
		t.Fatalf("DefaultIndexMapping() returned error: %v", err)
	}

	var js map[string]any
	if err := json.Unmarshal([]byte(mapping), &js); err != nil {
		t.Fatalf("DefaultIndexMapping() returned invalid JSON: %v\nMapping content:\n%s", err, mapping)
	}

	properties := js["properties"].(map[string]any)
	semanticFields := properties["semantic_fields"].(map[string]any)
	semanticProperties := semanticFields["properties"].(map[string]any)

	description := semanticProperties["description"].(map[string]any)

	//nolint:goconst
	if got := description["type"]; got != "semantic" {
		t.Fatalf("description type = %v, want semantic", got)
	}
	if got := description["model_id"]; got != "model-123" {
		t.Fatalf("description model_id = %v, want model-123", got)
	}

	spec := semanticProperties["spec"].(map[string]any)
	specProperties := spec["properties"].(map[string]any)
	summary := specProperties["summary"].(map[string]any)

	//nolint:goconst
	if got := summary["type"]; got != "semantic" {
		t.Fatalf("spec.summary type = %v, want semantic", got)
	}
	if got := summary["model_id"]; got != "model-123" {
		t.Fatalf("spec.summary model_id = %v, want model-123", got)
	}
}

func TestDefaultIndexMappingRequiresSemanticModelID(t *testing.T) {
	if _, err := DefaultIndexMapping(FieldMappings{Semantic: []string{"description"}}, ""); err == nil {
		t.Fatal("DefaultIndexMapping() error = nil, want semantic model id validation error")
	}
}

func TestDefaultIndexMappingIncludesFilterableFields(t *testing.T) {
	mapping, err := DefaultIndexMapping(FieldMappings{Filterable: []string{"status.phase"}}, "")
	if err != nil {
		t.Fatalf("DefaultIndexMapping() returned error: %v", err)
	}

	var js map[string]any
	if err := json.Unmarshal([]byte(mapping), &js); err != nil {
		t.Fatalf("DefaultIndexMapping() returned invalid JSON: %v", err)
	}

	properties := js["properties"].(map[string]any)
	status := properties["status"].(map[string]any)
	statusProps := status["properties"].(map[string]any)
	phase := statusProps["phase"].(map[string]any)
	if got := phase["type"]; got != "keyword" {
		t.Fatalf("status.phase type = %v, want keyword", got)
	}
}

func TestDefaultIndexMappingIncludesDefaultFields(t *testing.T) {
	mapping, err := DefaultIndexMapping(FieldMappings{Default: []string{"spec.description"}}, "")
	if err != nil {
		t.Fatalf("DefaultIndexMapping() returned error: %v", err)
	}

	var js map[string]any
	if err := json.Unmarshal([]byte(mapping), &js); err != nil {
		t.Fatalf("DefaultIndexMapping() returned invalid JSON: %v", err)
	}

	properties := js["properties"].(map[string]any)
	spec := properties["spec"].(map[string]any)
	specProps := spec["properties"].(map[string]any)
	desc := specProps["description"].(map[string]any)
	if got := desc["type"]; got != "text" {
		t.Fatalf("spec.description type = %v, want text", got)
	}
	fields, ok := desc["fields"].(map[string]any)
	if !ok {
		t.Fatalf("spec.description missing keyword subfield, got %v", desc["fields"])
	}
	keyword := fields["keyword"].(map[string]any)
	if got := keyword["type"]; got != "keyword" {
		t.Fatalf("spec.description.keyword type = %v, want keyword", got)
	}
}

func TestDefaultIndexMappingSemanticWinsPriority(t *testing.T) {
	// Same path present in all three lists must resolve to the semantic mapping without error.
	mapping, err := DefaultIndexMapping(FieldMappings{
		Default:    []string{"spec.title"},
		Semantic:   []string{"spec.title"},
		Filterable: []string{"spec.title"},
	}, "model-123")
	if err != nil {
		t.Fatalf("DefaultIndexMapping() returned error: %v", err)
	}

	var js map[string]any
	if err := json.Unmarshal([]byte(mapping), &js); err != nil {
		t.Fatalf("DefaultIndexMapping() returned invalid JSON: %v", err)
	}

	properties := js["properties"].(map[string]any)
	spec := properties["spec"].(map[string]any)
	specProps := spec["properties"].(map[string]any)
	title := specProps["title"].(map[string]any)
	if got := title["type"]; got != "semantic" {
		t.Fatalf("spec.title type = %v, want semantic (priority)", got)
	}
}
