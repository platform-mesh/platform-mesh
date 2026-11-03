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

import "testing"

func FuzzConvertToRelationName(f *testing.F) {
	f.Add("create", "apps", "deployments")
	f.Add("watch", "", "pods")
	f.Add("create", "generators.external-secrets.io", "beyondtrustworkloadcredentialsdynamicsecrets")
	f.Add("list", "", "")

	f.Fuzz(func(t *testing.T, verb, group, plural string) {
		relation, err := ConvertToRelationName(verb, group, plural)
		if err == nil && len(relation) > MaxRelationLength {
			t.Fatalf("relation %q exceeds %d characters", relation, MaxRelationLength)
		}
		if len(verb) <= len(longestVerb) && err != nil {
			t.Fatalf("unexpected error for verb %q: %v", verb, err)
		}
		if name := ConvertToTypeName(group, plural); len(name) > MaxTypeLength {
			t.Fatalf("type %q exceeds %d characters", name, MaxTypeLength)
		}
	})
}
