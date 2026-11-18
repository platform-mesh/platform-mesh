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

package ocm

import "testing"

// FuzzParseRef fuzzes ParseRef, which parses arbitrary, untrusted OCM/OCI
// reference strings through a chain of anchored regexps in pkg/ocm/grammar.
// The property under test is robustness: ParseRef must never panic and must
// always return either a RefSpec or a non-nil error, regardless of input.
func FuzzParseRef(f *testing.F) {
	// Seed corpus covers the distinct parsing branches in ParseRef so the
	// fuzzer starts from coverage of every match path.
	seeds := []string{
		"",
		"+ghcr.io/platform-mesh/operator:1.0.0",
		"ghcr.io/platform-mesh/operator:1.0.0",
		"type::https://ghcr.io/repo:tag",
		"OCIRegistry::localhost:5000/repo:1.0.0",
		"./local/path//repo:tag",
		"ubuntu:24.04",
		"library/ubuntu@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"docker.io/library/nginx",
		"type::file//path//repo:tag",
		"host:1234/a/b:tag",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, ref string) {
		// We intentionally ignore the result: the contract being fuzzed is
		// "no panic / no crash", not the specific spec or error returned.
		_, _ = ParseRef(ref)
	})
}
