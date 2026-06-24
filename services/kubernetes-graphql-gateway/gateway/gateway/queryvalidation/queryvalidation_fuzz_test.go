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

package queryvalidation

import (
	"testing"
)

// FuzzValidate feeds arbitrary query strings to Validate to ensure it never
// panics, regardless of how malformed or deeply nested the input is. Parse
// errors are an expected, valid outcome; a panic is not.
func FuzzValidate(f *testing.F) {
	seeds := []string{
		"",
		"{ a }",
		"{ a { b { c } } }",
		"query { user { posts { comments { author { name } } } } }",
		"fragment X on T { a ...X }", // self-referential fragment (cycle)
		"{ ...frag } fragment frag on T { a b c }",
		"query Q { a @skip(if: true) }",
		"{ a { ... on T { b } } }",
		"this is not graphql at all",
		"{{{{{{{{{{",
		"query " + "{ a" + "}",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	cfg := Config{MaxDepth: 10, MaxComplexity: 100, MaxBatchSize: 10}

	f.Fuzz(func(t *testing.T, query string) {
		// We only assert the absence of panics; the returned error (or nil)
		// is a legitimate result for any input.
		_ = Validate(query, cfg)
	})
}
