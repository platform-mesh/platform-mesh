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

	pmgateway "go.platform-mesh.io/apis/gateway"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

var (
	// ErrInvalidGVKFormat indicates the x-kubernetes-group-version-kind extension has an unexpected format.
	ErrInvalidGVKFormat = errors.New("invalid GVK extension format")

	// ErrScopeNotFound indicates the x-kubernetes-scope extension is missing.
	ErrScopeNotFound = errors.New("scope extension not found")

	// ErrInvalidScopeFormat indicates the scope extension has an unexpected format.
	ErrInvalidScopeFormat = errors.New("invalid scope extension format")
)

// ExtractGVK extracts GVK from schema extensions using type assertion.
// Returns nil if schema has no GVK extension (e.g., sub-resources).
func ExtractGVK(s *spec.Schema) (*schema.GroupVersionKind, error) {
	if s == nil || s.Extensions == nil {
		return nil, nil
	}

	gvksVal, ok := s.Extensions[pmgateway.GVKExtensionKey]
	if !ok {
		return nil, nil
	}

	// Try direct type assertion path first (most common)
	if gvkSlice, ok := gvksVal.([]any); ok {
		return extractFromInterfaceSlice(gvkSlice)
	}

	// Fallback: might be []map[string]any already
	if mapSlice, ok := gvksVal.([]map[string]any); ok {
		return extractFromMapSlice(mapSlice)
	}

	return nil, ErrInvalidGVKFormat
}

func extractFromInterfaceSlice(slice []any) (*schema.GroupVersionKind, error) {
	if len(slice) != 1 {
		return nil, nil // Skip schemas with multiple or zero GVKs
	}

	gvkMap, ok := slice[0].(map[string]any)
	if !ok {
		return nil, ErrInvalidGVKFormat
	}

	return gvkFromMap(gvkMap), nil
}

func extractFromMapSlice(slice []map[string]any) (*schema.GroupVersionKind, error) {
	if len(slice) != 1 {
		return nil, nil
	}

	return gvkFromMap(slice[0]), nil
}

// gvkFromMap extracts a GroupVersionKind from a map with group/version/kind keys.
func gvkFromMap(m map[string]any) *schema.GroupVersionKind {
	return &schema.GroupVersionKind{
		Group:   mapValue[string](m, "group"),
		Version: mapValue[string](m, "version"),
		Kind:    mapValue[string](m, "kind"),
	}
}

// mapValue extracts a typed value from a map, returning the zero value if not found or wrong type.
func mapValue[T any](m map[string]any, key string) T {
	var zero T
	if v, ok := m[key]; ok {
		if typed, ok := v.(T); ok {
			return typed
		}
	}
	return zero
}

// ExtractScope extracts the resource scope from schema extensions.
// Returns the scope (Namespaced or Cluster) or an error if not found.
func ExtractScope(schema *spec.Schema) (apiextensionsv1.ResourceScope, error) {
	if schema == nil || schema.Extensions == nil {
		return "", ErrScopeNotFound
	}

	scopeRaw, ok := schema.Extensions[pmgateway.ScopeExtensionKey]
	if !ok {
		return "", ErrScopeNotFound
	}

	// Handle both string and ResourceScope types
	switch v := scopeRaw.(type) {
	case string:
		return apiextensionsv1.ResourceScope(v), nil
	case apiextensionsv1.ResourceScope:
		return v, nil
	default:
		return "", ErrInvalidScopeFormat
	}
}
