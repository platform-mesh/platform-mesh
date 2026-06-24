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
	"fmt"

	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/parser"
	"github.com/graphql-go/graphql/language/source"
)

// Config holds limits for query validation. Zero values disable the respective check.
type Config struct {
	// MaxDepth is the maximum allowed nesting depth for GraphQL queries.
	// 0 disables the limit.
	MaxDepth int

	// MaxComplexity is the maximum allowed complexity score for GraphQL queries.
	// Each field resolution counts as 1 point of complexity.
	// 0 disables the limit.
	MaxComplexity int

	// MaxBatchSize is the maximum number of queries allowed in a single batched request.
	// 0 disables the limit.
	MaxBatchSize int
}

// Validate parses a GraphQL query string and checks depth/complexity limits.
// Returns a non-nil error if limits are exceeded.
func Validate(query string, cfg Config) error {
	if cfg.MaxDepth <= 0 && cfg.MaxComplexity <= 0 {
		return nil
	}

	src := source.NewSource(&source.Source{Body: []byte(query)})
	doc, err := parser.Parse(parser.ParseParams{Source: src})
	if err != nil {
		return fmt.Errorf("failed to parse query: %w", err)
	}

	w := &walker{cfg: cfg, fragments: make(map[string]*ast.FragmentDefinition)}
	for _, def := range doc.Definitions {
		if frag, ok := def.(*ast.FragmentDefinition); ok {
			w.fragments[frag.Name.Value] = frag
		}
	}

	var maxDepth, totalComplexity int
	for _, def := range doc.Definitions {
		if op, ok := def.(*ast.OperationDefinition); ok && op.SelectionSet != nil {
			w.visiting = make(map[string]bool)
			depth, complexity := w.walk(op.SelectionSet, 1)
			if depth > maxDepth {
				maxDepth = depth
			}
			totalComplexity += complexity
		}
	}

	if cfg.MaxDepth > 0 && maxDepth > cfg.MaxDepth {
		return fmt.Errorf("query depth %d exceeds maximum allowed depth of %d", maxDepth, cfg.MaxDepth)
	}
	if cfg.MaxComplexity > 0 && totalComplexity > cfg.MaxComplexity {
		return fmt.Errorf("query complexity %d exceeds maximum allowed complexity of %d", totalComplexity, cfg.MaxComplexity)
	}

	return nil
}

// walker holds shared state for recursive AST traversal.
type walker struct {
	cfg       Config
	fragments map[string]*ast.FragmentDefinition
	visiting  map[string]bool // per-operation cycle detection for fragment spreads
}

// walk recursively traverses a selection set and returns the maximum depth
// and total field complexity. Exits early when configured limits are exceeded.
func (w *walker) walk(selSet *ast.SelectionSet, depth int) (int, int) {
	maxDepth := depth
	var complexity int

	if w.cfg.MaxDepth > 0 && depth > w.cfg.MaxDepth {
		return maxDepth, complexity
	}

	for _, sel := range selSet.Selections {
		switch s := sel.(type) {
		case *ast.Field:
			complexity++
			if w.cfg.MaxComplexity > 0 && complexity > w.cfg.MaxComplexity {
				return maxDepth, complexity
			}
			if s.SelectionSet != nil {
				childDepth, childComplexity := w.walk(s.SelectionSet, depth+1)
				if childDepth > maxDepth {
					maxDepth = childDepth
				}
				complexity += childComplexity
				if w.cfg.MaxComplexity > 0 && complexity > w.cfg.MaxComplexity {
					return maxDepth, complexity
				}
			}

		case *ast.InlineFragment:
			// Inline fragments don't increase depth — they narrow the type, not the nesting.
			if s.SelectionSet != nil {
				childDepth, childComplexity := w.walk(s.SelectionSet, depth)
				if childDepth > maxDepth {
					maxDepth = childDepth
				}
				complexity += childComplexity
				if w.cfg.MaxComplexity > 0 && complexity > w.cfg.MaxComplexity {
					return maxDepth, complexity
				}
			}

		case *ast.FragmentSpread:
			name := s.Name.Value
			if w.visiting[name] {
				continue // cycle detected, skip
			}
			frag, ok := w.fragments[name]
			if !ok {
				continue // unknown fragment, graphql validation will catch this
			}
			w.visiting[name] = true
			if frag.SelectionSet != nil {
				childDepth, childComplexity := w.walk(frag.SelectionSet, depth)
				if childDepth > maxDepth {
					maxDepth = childDepth
				}
				complexity += childComplexity
			}
			delete(w.visiting, name)
		}
	}

	return maxDepth, complexity
}
