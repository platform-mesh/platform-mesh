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

// Package util provides utility functions for converting API group and resource names
// into normalized type names suitable for use in authorization systems.
//
// This package is primarily designed for use with Fine-Grained Authorization (FGA)
// systems where consistent naming conventions are required for type definitions.
package util

import (
	"fmt"
	"strings"
)

// maxRelationLength defines the maximum allowed length for relation names in FGA systems.
// This limit ensures compatibility with authorization backends that have string length
// constraints on relation and type names.
//
// The value of 50 is chosen to accommodate the longest possible relation format:
// "create_<group>_<singular>s" while leaving room for reasonable group and resource names.
const maxRelationLength = 50

// ConvertToTypeName converts an API group and singular resource name into a normalized
// type name suitable for use in authorization systems.
//
// Parameters:
//   - group: The API group name (e.g., "apps", "networking.k8s.io", "")
//   - singular: The singular form of the resource name (e.g., "deployment", "pod", "Service")
//
// Returns:
//
//	A normalized type name string suitable for authorization system usage.
//
// Examples:
//
//	ConvertToTypeName("apps", "deployment") → "apps_deployment"
//	ConvertToTypeName("", "pod") → "core_pod"
//	ConvertToTypeName("networking.k8s.io", "ingress") → "networking_k8s_io_ingress"
//	ConvertToTypeName("Apps", "Deployment") → "apps_deployment"
//
// The function handles edge cases gracefully:
//   - Empty group names default to "core"
//   - Very long names are truncated to respect maxRelationLength
//   - Special characters like dots are normalized to underscores
//   - Mixed case is normalized to lowercase
func ConvertToTypeName(group, singular string) string {
	if group == "" {
		group = "core"
	}

	// Cap the length of the group_singular string to respect relation length limits
	objectType := capGroupSingularLength(group, singular, maxRelationLength)

	// Make sure the result does not start with an underscore (can happen with empty groups)
	objectType = strings.TrimPrefix(objectType, "_")

	// Replace dots with underscores in the final objectType for system compatibility
	objectType = strings.ReplaceAll(objectType, ".", "_")

	// Convert to lowercase for consistent naming conventions
	objectType = strings.ToLower(objectType)

	return objectType
}

// capGroupSingularLength creates a group_singular string and truncates it if necessary
// to ensure the resulting relation names don't exceed the specified maximum length.
//
// This function is used internally by ConvertToTypeName to handle length constraints
// imposed by authorization systems. It calculates the potential length of the longest
// relation that would be created ("create_<group>_<singular>") and truncates the
// group_singular combination if needed.
func capGroupSingularLength(group, singular string, maxLength int) string {
	groupSingular := fmt.Sprintf("%s_%s", group, singular)
	maxRelation := fmt.Sprintf("create_%ss", groupSingular)

	if len(maxRelation) > maxLength && maxLength > 0 {
		truncateLen := len(maxRelation) - maxLength
		return groupSingular[truncateLen:]
	}
	return groupSingular
}
