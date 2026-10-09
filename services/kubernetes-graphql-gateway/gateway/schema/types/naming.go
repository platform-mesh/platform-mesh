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

package types

import (
	"regexp"
	"strings"
)

var (
	// invalidFieldCharRegex matches characters that are not valid in GraphQL field names
	invalidFieldCharRegex = regexp.MustCompile(`[^_a-zA-Z0-9]`)
	// validFieldStartRegex matches valid starting characters for GraphQL field names
	validFieldStartRegex = regexp.MustCompile(`^[_a-zA-Z]`)
)

// SanitizeFieldName converts a field name to a valid GraphQL identifier.
// It replaces invalid characters with underscores and prepends '_' if needed.
func SanitizeFieldName(name string) string {
	// Replace any invalid characters with '_'
	name = invalidFieldCharRegex.ReplaceAllString(name, "_")

	// If the name doesn't start with a letter or underscore, prepend '_'
	if !validFieldStartRegex.MatchString(name) {
		name = "_" + name
	}

	return name
}

// GenerateTypeName creates a type name from a prefix and field path.
// This is used to generate unique names for nested types.
// Each path element is capitalized for readability (e.g., "PodSpecContainers").
func GenerateTypeName(typePrefix string, fieldPath []string) string {
	var b strings.Builder
	b.WriteString(typePrefix)
	for _, field := range fieldPath {
		b.WriteString(capitalize(field))
	}
	return b.String()
}

// capitalize returns the string with the first letter uppercased.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
