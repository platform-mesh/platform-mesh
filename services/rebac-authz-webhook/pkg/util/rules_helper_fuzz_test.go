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
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func FuzzCapGroupToRelationLength(f *testing.F) {
	f.Add("apps", "v1", "deployments", 50)
	f.Add("apis.kcp.io", "", "apibindings", 50)
	f.Add("", "v1", "pods", 50)
	f.Add("very.long.group.name.example.io", "v1", "verylongresourcenamethatexceedslimits", 50)
	f.Add("a", "", "b", 10)
	f.Add("", "", "", 1)

	f.Fuzz(func(t *testing.T, group, version, resource string, maxLength int) {
		if maxLength < 1 {
			return
		}

		gvr := schema.GroupVersionResource{
			Group:    group,
			Version:  version,
			Resource: resource,
		}

		// Must not panic
		_ = CapGroupToRelationLength(gvr, maxLength)
	})
}

func FuzzResolveOnParent(f *testing.F) {
	f.Add("create")
	f.Add("list")
	f.Add("watch")
	f.Add("get")
	f.Add("delete")
	f.Add("")

	f.Fuzz(func(t *testing.T, verb string) {
		// Must not panic
		_ = ResolveOnParent(verb)
	})
}
