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

package types_test

import (
	"regexp"
	"testing"

	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/schema/types"
)

// validIdentifier matches a valid GraphQL identifier: it must start with a
// letter or underscore and contain only letters, digits, and underscores.
var validIdentifier = regexp.MustCompile(`^[_a-zA-Z][_a-zA-Z0-9]*$`)

// FuzzSanitizeFieldName checks that SanitizeFieldName always returns a valid
// GraphQL identifier for any input and that the operation is idempotent.
func FuzzSanitizeFieldName(f *testing.F) {
	seeds := []string{
		"",
		"validFieldName",
		"field-name",
		"1field",
		"field.name-with$special",
		"_privateField",
		"!@#$%",
		"日本語",
		"a b\tc\n",
		"\x00\x01\x02",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, in string) {
		got := types.SanitizeFieldName(in)

		if !validIdentifier.MatchString(got) {
			t.Fatalf("SanitizeFieldName(%q) = %q, which is not a valid GraphQL identifier", in, got)
		}

		// Sanitizing an already-sanitized name must not change it.
		if again := types.SanitizeFieldName(got); again != got {
			t.Fatalf("SanitizeFieldName not idempotent: SanitizeFieldName(%q) = %q", got, again)
		}
	})
}
