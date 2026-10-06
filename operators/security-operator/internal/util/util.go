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

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func CapGroupToRelationLength(gvr schema.GroupVersionResource, maxLength int) string {
	maxRelation := fmt.Sprintf("create_%s_%s", gvr.Group, gvr.Resource)

	group := gvr.Group
	if group == "" {
		group = "core"
	}

	if len(maxRelation) > maxLength {
		start := len(maxRelation) - maxLength
		if start > len(group) {
			return ""
		}
		return group[start:]
	}

	return group
}

// ResourceRelationName returns the group/resource part of a create, list, or
// watch relation. It reserves space for the longest prefix ("create_") and
// keeps a stable suffix when the Kubernetes names do not fit OpenFGA's limit.
func ResourceRelationName(gvr schema.GroupVersionResource, maxLength int) string {
	group := gvr.Group
	if group == "" {
		group = "core"
	}

	identifier := fmt.Sprintf("%s_%s", group, gvr.Resource)
	available := maxLength - len("create_")
	if available <= 0 {
		return ""
	}
	if len(identifier) > available {
		return identifier[len(identifier)-available:]
	}
	return identifier
}
