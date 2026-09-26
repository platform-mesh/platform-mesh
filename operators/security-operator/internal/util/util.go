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

package util

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func CapGroupToRelationLength(gvr schema.GroupVersionResource, maxLength int) string {
	maxRelation := fmt.Sprintf("create_%s_%s", gvr.Group, gvr.Resource)

	group := gvr.Group
	if group == "" {
		group = "core"
	}

	if len(maxRelation) > maxLength {
		return group[len(maxRelation)-maxLength:]
	}

	return group
}

// NormalizeEmailDomains trims, lowercases, deduplicates, and drops empty entries
// from a list of email domains. Lowercasing keeps behaviour consistent with
// Keycloak, which matches organization domains case-insensitively.
func NormalizeEmailDomains(domains []string) []string {
	out := make([]string, 0, len(domains))
	seen := make(map[string]struct{}, len(domains))
	for _, domain := range domains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		if domain == "" {
			continue
		}
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}
		out = append(out, domain)
	}
	return out
}
