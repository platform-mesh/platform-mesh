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
	"unicode"
)

func TestEncodeObjectIDPart(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "plain", input: "cluster-admin", want: "cluster-admin"},
		{name: "allowed punctuation", input: "name?with@punctuation", want: "name?with@punctuation"},
		{name: "unicode", input: "münchen", want: "münchen"},
		{name: "colon", input: "system:controller:foo", want: "system%3Acontroller%3Afoo"},
		{name: "hash", input: "name#fragment", want: "name%23fragment"},
		{name: "space", input: "name with space", want: "name%20with%20space"},
		{name: "ascii controls", input: "tab\tline\n", want: "tab%09line%0A"},
		{name: "unicode control", input: "next\u0085line", want: "next%C2%85line"},
		{name: "percent", input: "literal%3Avalue", want: "literal%253Avalue"},
		{name: "invalid utf8", input: string([]byte{'a', 0xff, 'b'}), want: "a%FFb"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := EncodeObjectIDPart(test.input); got != test.want {
				t.Fatalf("EncodeObjectIDPart(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func FuzzEncodeObjectIDPartIsInjective(f *testing.F) {
	f.Add("system:controller:foo", "system.controller.foo")
	f.Add("name#fragment", "name%23fragment")
	f.Add("name with space", "name%20with%20space")
	f.Add("line\nfeed", "line%0Afeed")
	f.Add("", ":")

	f.Fuzz(func(t *testing.T, first, second string) {
		encodedFirst := EncodeObjectIDPart(first)
		encodedSecond := EncodeObjectIDPart(second)
		if first != second && encodedFirst == encodedSecond {
			t.Fatalf("distinct values %q and %q encoded to %q", first, second, encodedFirst)
		}

		for _, r := range encodedFirst {
			if r == ':' || r == '#' || r == ' ' || unicode.IsControl(r) {
				t.Fatalf("EncodeObjectIDPart(%q) = %q contains OpenFGA-forbidden rune %q", first, encodedFirst, r)
			}
		}
	})
}
