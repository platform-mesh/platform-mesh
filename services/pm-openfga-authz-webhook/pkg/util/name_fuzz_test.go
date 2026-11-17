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

	"github.com/stretchr/testify/assert"
)

// FuzzEncodeNameIsInjective asserts that distinct names never encode to the
// same key. A lossy substitution such as replacing ':' with '.' would collapse
// "system:controller:foo" and "system.controller.foo" onto one object, letting
// a grant on one resource authorize access to the other.
func FuzzEncodeNameIsInjective(f *testing.F) {
	f.Add("system:controller:foo", "system.controller.foo")
	f.Add("a%b:c", "a%25b%3Ac")
	f.Add("", ":")
	f.Add("cluster-admin", "cluster-admin")
	f.Add("a:b", "a:b:c")

	f.Fuzz(func(t *testing.T, a, b string) {
		encA, encB := EncodeName(a), EncodeName(b)

		if a != b {
			assert.NotEqualf(t, encA, encB, "distinct names %q and %q collided", a, b)
		}

		// The encoded form must never carry a raw colon, which OpenFGA
		// reserves as the separator between object type and identifier.
		assert.NotContainsf(t, encA, ":", "EncodeName(%q) = %q still contains a raw colon", a, encA)
	})
}
