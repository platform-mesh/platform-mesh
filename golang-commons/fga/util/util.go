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

// Package util derives the OpenFGA type and relation names Platform Mesh uses
// for Kubernetes API resources. Every component that builds or checks these
// names must go through this package so that they agree.
package util

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	// MaxRelationLength is OpenFGA's limit for relation names.
	MaxRelationLength = 50
	// MaxTypeLength is OpenFGA's limit for type names.
	MaxTypeLength = 254

	hashLength = 16
	// hashSeparator precedes the hash in a shortened relation. A normalized
	// group never has two underscores in a row and a plural has none.
	hashSeparator = "__"
	// longestVerb is the longest verb a resource relation is defined for in
	// the model (create, list, watch).
	longestVerb = "create"
	// minGroupPrefix keeps part of the group readable in a hashed relation
	// even when the plural alone would fill the budget.
	minGroupPrefix = 8
)

// ConvertToTypeName returns the OpenFGA type for an API group and singular
// resource name: "<group>_<singular>" with dots replaced by underscores,
// lowercased, and "core" for the empty group. It is never truncated; only a
// name above MaxTypeLength is replaced by a readable prefix and a hash.
//
//	ConvertToTypeName("apps", "deployment") → "apps_deployment"
//	ConvertToTypeName("", "Pod") → "core_pod"
func ConvertToTypeName(group, singular string) string {
	name := normalizeGroup(group) + "_" + strings.ToLower(singular)
	if len(name) <= MaxTypeLength {
		return name
	}
	return name[:MaxTypeLength-hashLength-1] + "_" + hash(group, singular)
}

// ConvertToRelationName returns the relation that grants verb on the resources
// group/plural, defined on the parent account or namespace:
// "<verb>_<group>_<plural>". When the create relation of the resource would
// exceed MaxRelationLength, the "<group>_<plural>" part is replaced by a
// readable prefix and a hash of group and plural, the same for every verb.
// The hash follows "__", which an unshortened name never contains, so a
// shortened relation cannot equal the relation of another resource.
// It returns an error when the relation for verb does not fit.
func ConvertToRelationName(verb, group, plural string) (string, error) {
	relation := verb + "_" + resourceRelationSuffix(group, plural)
	if len(relation) > MaxRelationLength {
		return "", fmt.Errorf("relation %q for %s %s/%s exceeds %d characters", relation, verb, group, plural, MaxRelationLength)
	}
	return relation, nil
}

func resourceRelationSuffix(group, plural string) string {
	g := normalizeGroup(group)
	p := strings.ToLower(plural)

	suffix := g + "_" + p
	if len(longestVerb)+1+len(suffix) <= MaxRelationLength {
		return suffix
	}

	budget := MaxRelationLength - len(longestVerb) - 1 - len(hashSeparator) - hashLength
	groupLen := min(len(g), max(budget-1-len(p), minGroupPrefix))
	pluralLen := min(len(p), budget-1-groupLen)

	return strings.TrimRight(g[:groupLen], "_-") + "_" + p[:pluralLen] + hashSeparator + hash(group, plural)
}

func normalizeGroup(group string) string {
	if group == "" {
		group = "core"
	}
	return strings.ToLower(strings.ReplaceAll(group, ".", "_"))
}

func hash(group, name string) string {
	sum := sha256.Sum256([]byte(group + "/" + name))
	return hex.EncodeToString(sum[:])[:hashLength]
}
