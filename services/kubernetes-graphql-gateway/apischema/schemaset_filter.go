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
	"errors"
	"fmt"
	"maps"

	pmgateway "go.platform-mesh.io/apis/gateway"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kube-openapi/pkg/schemamutation"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

// ErrSchemaReferenceNotFound indicates that a selected schema refers to a
// definition that was not loaded.
var ErrSchemaReferenceNotFound = errors.New("schema reference not found")

// ResourceSelector matches a Kubernetes resource by exact group and optional
// exact version and resource name.
type ResourceSelector struct {
	Group    string
	Version  string
	Resource string
}

// Matches reports whether the selector matches the supplied GVR.
func (s ResourceSelector) Matches(gvr schema.GroupVersionResource) bool {
	return s.Group == gvr.Group &&
		(s.Version == "" || s.Version == gvr.Version) &&
		(s.Resource == "" || s.Resource == gvr.Resource)
}

// SelectResources returns a new SchemaSet containing matched resource roots
// and the transitive closure of definitions they reference.
func (s *SchemaSet) SelectResources(selectors []ResourceSelector, mapper meta.RESTMapper) (*SchemaSet, error) {
	selected := make(map[string]*SchemaEntry)
	queue := make([]string, 0)

	for key, entry := range s.entries {
		if entry.GVK == nil {
			continue
		}

		matches, err := matchesAny(selectors, mapper, *entry.GVK)
		if err != nil {
			return nil, err
		}
		if !matches {
			continue
		}

		selected[key] = entry
		queue = append(queue, key)
	}

	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]

		for refKey := range schemaReferences(selected[key].Schema) {
			if _, ok := selected[refKey]; ok {
				continue
			}

			referenced, ok := s.entries[refKey]
			if !ok {
				return nil, fmt.Errorf("%w: %q referenced by %q", ErrSchemaReferenceNotFound, refKey, key)
			}

			if referenced.GVK != nil {
				referenced = asSupportDefinition(referenced)
			}

			selected[refKey] = referenced
			queue = append(queue, refKey)
		}
	}

	return NewSchemaSet(selected), nil
}

func matchesAny(selectors []ResourceSelector, mapper meta.RESTMapper, gvk schema.GroupVersionKind) (bool, error) {
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if meta.IsNoMatchError(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("resolve resource for %s: %w", gvk, err)
	}

	for _, selector := range selectors {
		if selector.Matches(mapping.Resource) {
			return true, nil
		}
	}

	return false, nil
}

func schemaReferences(schema *spec.Schema) map[string]struct{} {
	references := make(map[string]struct{})
	walker := schemamutation.Walker{
		SchemaCallback: schemamutation.SchemaCallBackNoop,
		RefCallback: func(ref *spec.Ref) *spec.Ref {
			if key := ref.String(); key != "" {
				references[key] = struct{}{}
			}
			return ref
		},
	}
	walker.WalkSchema(schema)
	return references
}

func asSupportDefinition(entry *SchemaEntry) *SchemaEntry {
	clonedSchema := *entry.Schema
	clonedSchema.Extensions = maps.Clone(entry.Schema.Extensions)
	delete(clonedSchema.Extensions, pmgateway.GVKExtensionKey)

	return &SchemaEntry{
		Key:    entry.Key,
		Schema: &clonedSchema,
	}
}
