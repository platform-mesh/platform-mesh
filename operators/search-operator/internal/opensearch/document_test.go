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
	mapping, err := DefaultIndexMapping(nil, "")
	if err != nil {
		t.Fatalf("DefaultIndexMapping() returned error: %v", err)
	}
	var js map[string]interface{}
	if err := json.Unmarshal([]byte(mapping), &js); err != nil {
		t.Fatalf("DefaultIndexMapping() returned invalid JSON: %v\nMapping content:\n%s", err, mapping)
	}
}

func TestDefaultIndexMappingIncludesSemanticFields(t *testing.T) {
	mapping, err := DefaultIndexMapping([]string{"description", "spec.summary"}, "model-123")
	if err != nil {
		t.Fatalf("DefaultIndexMapping() returned error: %v", err)
	}

	var js map[string]any
	if err := json.Unmarshal([]byte(mapping), &js); err != nil {
		t.Fatalf("DefaultIndexMapping() returned invalid JSON: %v\nMapping content:\n%s", err, mapping)
	}

	properties := js["properties"].(map[string]any)

	description := properties["description"].(map[string]any)

	//nolint:goconst
	if got := description["type"]; got != "semantic" {
		t.Fatalf("description type = %v, want semantic", got)
	}
	if got := description["model_id"]; got != "model-123" {
		t.Fatalf("description model_id = %v, want model-123", got)
	}

	spec := properties["spec"].(map[string]any)
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
	if _, err := DefaultIndexMapping([]string{"description"}, ""); err == nil {
		t.Fatal("DefaultIndexMapping() error = nil, want semantic model id validation error")
	}
}
