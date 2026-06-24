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

package apischema

import (
	"encoding/json"
	"maps"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kube-openapi/pkg/spec3"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

// SchemaEntry holds a schema with its parsed GVK metadata.
// Created once during loading - GVK never needs re-parsing.
type SchemaEntry struct {
	Key    string // OpenAPI schema key (e.g., "io.k8s.api.core.v1.Pod")
	Schema *spec.Schema
	GVK    *schema.GroupVersionKind
}

// SchemaSet is an immutable collection of schemas
type SchemaSet struct {
	entries map[string]*SchemaEntry
	byKind  map[string][]*SchemaEntry
	byGVK   map[schema.GroupVersionKind]*SchemaEntry
}

// NewSchemaSet creates a new SchemaSet from a map of entries.
func NewSchemaSet(entries map[string]*SchemaEntry) *SchemaSet {
	s := &SchemaSet{
		entries: entries,
		byKind:  make(map[string][]*SchemaEntry),
		byGVK:   make(map[schema.GroupVersionKind]*SchemaEntry),
	}

	for _, entry := range entries {
		if entry.GVK == nil {
			continue
		}

		// Index by lowercase kind for O(1) lookup
		kindKey := strings.ToLower(entry.GVK.Kind)
		s.byKind[kindKey] = append(s.byKind[kindKey], entry)

		// Index by exact GVK
		s.byGVK[*entry.GVK] = entry
	}

	return s
}

// Get returns a schema entry by its key.
func (s *SchemaSet) Get(key string) (*SchemaEntry, bool) {
	entry, ok := s.entries[key]
	return entry, ok
}

// GetByGVK returns a schema entry by its exact GVK
func (s *SchemaSet) GetByGVK(gvk schema.GroupVersionKind) (*SchemaEntry, bool) {
	entry, ok := s.byGVK[gvk]
	return entry, ok
}

// FindByKind returns all schema entries matching a kind name
// Kind matching is case-insensitive.
func (s *SchemaSet) FindByKind(kind string) []*SchemaEntry {
	return s.byKind[strings.ToLower(kind)]
}

// All returns all schema entries.
func (s *SchemaSet) All() map[string]*SchemaEntry {
	return maps.Clone(s.entries)
}

// Size returns the number of schema entries.
func (s *SchemaSet) Size() int {
	return len(s.entries)
}

// Marshal serializes the SchemaSet to OpenAPI v3 JSON.
func (s *SchemaSet) Marshal() ([]byte, error) {
	schemas := make(map[string]*spec.Schema, len(s.entries))
	for key, entry := range s.entries {
		schemas[key] = entry.Schema
	}

	return json.Marshal(&spec3.OpenAPI{
		Components: &spec3.Components{
			Schemas: schemas,
		},
	})
}

// NewSchemaSetFromMap creates a SchemaSet from raw schemas.
func NewSchemaSetFromMap(schemas map[string]*spec.Schema) *SchemaSet {
	entries := make(map[string]*SchemaEntry, len(schemas))
	for k, v := range schemas {
		gvk, _ := ExtractGVK(v)
		entries[k] = &SchemaEntry{
			Key:    k,
			Schema: v,
			GVK:    gvk,
		}
	}
	return NewSchemaSet(entries)
}
