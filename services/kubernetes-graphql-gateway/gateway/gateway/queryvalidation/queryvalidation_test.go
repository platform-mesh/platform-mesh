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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		cfg     Config
		wantErr string
	}{
		{
			name:  "query at exact depth limit passes",
			query: `{ a { b { c } } }`,
			cfg:   Config{MaxDepth: 3},
		},
		{
			name:    "query exceeds depth limit",
			query:   `{ a { b { c { d } } } }`,
			cfg:     Config{MaxDepth: 3},
			wantErr: "query depth 4 exceeds maximum allowed depth of 3",
		},
		{
			name:    "alias explosion rejected by complexity limit",
			query:   `{ a1: pod { name } a2: pod { name } a3: pod { name } a4: pod { name } a5: pod { name } }`,
			cfg:     Config{MaxComplexity: 5},
			wantErr: "exceeds maximum allowed complexity of 5",
		},
		{
			name:  "complexity at exact limit passes",
			query: `{ a { name } b { name } }`,
			cfg:   Config{MaxComplexity: 4},
			// 2 outer + 2 inner = 4
		},
		{
			name:    "complexity exceeds limit",
			query:   `{ a { name } b { name } c { name } }`,
			cfg:     Config{MaxComplexity: 4},
			wantErr: "exceeds maximum allowed complexity of 4",
		},
		{
			name: "fragment spread contributes to depth",
			query: `
				query { ...F }
				fragment F on Query { a { b { c { d } } } }
			`,
			cfg:     Config{MaxDepth: 3},
			wantErr: "query depth 4 exceeds maximum allowed depth of 3",
		},
		{
			name: "fragment spread contributes to complexity",
			query: `
				query { ...F }
				fragment F on Query { a b c d e f }
			`,
			cfg:     Config{MaxComplexity: 5},
			wantErr: "exceeds maximum allowed complexity of 5",
		},
		{
			name: "fragment cycle does not cause infinite loop",
			query: `
				query { ...A }
				fragment A on Query { a { ...B } }
				fragment B on Query { b { ...A } }
			`,
			cfg: Config{MaxDepth: 100, MaxComplexity: 100},
		},
		{
			name: "inline fragment contributes to depth",
			query: `{
				... on Query { a { b { c { d } } } }
			}`,
			cfg:     Config{MaxDepth: 3},
			wantErr: "query depth 4 exceeds maximum allowed depth of 3",
		},
		{
			name:  "disabled when both limits are zero",
			query: `{ a { b { c { d { e { f { g { h { i { j } } } } } } } } } }`,
			cfg:   Config{MaxDepth: 0, MaxComplexity: 0},
		},
		{
			name:    "depth check only when complexity is zero",
			query:   `{ a { b { c { d } } } }`,
			cfg:     Config{MaxDepth: 3, MaxComplexity: 0},
			wantErr: "query depth 4 exceeds maximum allowed depth of 3",
		},
		{
			name:    "complexity check only when depth is zero",
			query:   `{ a b c d e f }`,
			cfg:     Config{MaxDepth: 0, MaxComplexity: 5},
			wantErr: "exceeds maximum allowed complexity of 5",
		},
		{
			name:    "invalid query returns parse error",
			query:   `{ invalid query {{{{`,
			cfg:     Config{MaxDepth: 10},
			wantErr: "failed to parse query",
		},
		{
			name:  "mutation counted the same as query",
			query: `mutation { createPod { metadata { name namespace } } }`,
			cfg:   Config{MaxDepth: 3, MaxComplexity: 100},
		},
		{
			name: "multiple operations sum complexity",
			query: `
				query A { a b c }
				query B { d e f }
			`,
			cfg:     Config{MaxComplexity: 5},
			wantErr: "exceeds maximum allowed complexity of 5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.query, tt.cfg)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
