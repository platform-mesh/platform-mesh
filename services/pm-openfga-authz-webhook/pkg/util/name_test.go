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

func TestEncodeName(t *testing.T) {
	testCases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "encodes a colon",
			in:   "system:controller:foo",
			want: "system%3Acontroller%3Afoo",
		},
		{
			name: "leaves a plain name untouched",
			in:   "test-sample",
			want: "test-sample",
		},
		{
			name: "leaves a dotted name untouched",
			in:   "my-app.example.com",
			want: "my-app.example.com",
		},
		{
			name: "leaves an empty name untouched",
			in:   "",
			want: "",
		},
		{
			// Kubernetes names cannot contain '%', but encoding it keeps the
			// transformation injective for any non-k8s caller.
			name: "escapes a literal percent before encoding colons",
			in:   "a%b:c",
			want: "a%25b%3Ac",
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, EncodeName(test.in))
		})
	}
}
