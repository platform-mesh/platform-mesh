package resolver

import (
	"testing"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// A per-type subscription selects fields directly on a concrete object type.
const perTypeSubscription = `
subscription {
  pods {
    type
    object {
      metadata { name }
      spec { nodeName }
    }
  }
}`

// A category subscription selects on a union, so the only way to reach a field
// is through an inline fragment.
const categorySubscription = `
subscription {
  resourcesByCategory(name: "cert-manager") {
    type
    object {
      ... on Certificate {
        metadata { name }
        spec { dnsNames }
      }
    }
  }
}`

const categorySubscriptionTwoMembers = `
subscription {
  resourcesByCategory(name: "cert-manager") {
    type
    object {
      ... on Certificate {
        spec { dnsNames }
      }
      ... on Issuer {
        status { conditions }
      }
    }
  }
}`

// A named fragment reaches the same fields by a different AST node: the body
// lives in a FragmentDefinition, not in the spread.
const categorySubscriptionNamedFragment = `
subscription {
  resourcesByCategory(name: "cert-manager") {
    type
    object {
      ...certFields
    }
  }
}

fragment certFields on Certificate {
  metadata { name }
  spec { dnsNames }
}`

func TestExtractRequestedFields(t *testing.T) {
	tests := []struct {
		name           string
		query          string
		expectedFields []string
	}{
		{
			name:           "concrete object type",
			query:          perTypeSubscription,
			expectedFields: []string{"metadata.name", "spec.nodeName"},
		},
		{
			name:           "inline fragment on one union member",
			query:          categorySubscription,
			expectedFields: []string{"metadata.name", "spec.dnsNames"},
		},
		{
			name:           "inline fragments on two union members",
			query:          categorySubscriptionTwoMembers,
			expectedFields: []string{"spec.dnsNames", "status.conditions"},
		},
		{
			name:           "named fragment spread on a union member",
			query:          categorySubscriptionNamedFragment,
			expectedFields: []string{"metadata.name", "spec.dnsNames"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := extractRequestedFields(resolveInfo(t, tt.query))

			assert.ElementsMatch(t, tt.expectedFields, fields)
		})
	}
}

func TestDetermineFieldChanged_FieldSelectedThroughInlineFragment(t *testing.T) {
	before := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{"name": "cert"},
			"spec":     map[string]any{"dnsNames": []any{"a.example.com"}},
		},
	}
	after := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{"name": "cert"},
			"spec":     map[string]any{"dnsNames": []any{"b.example.com"}},
		},
	}

	watched := extractRequestedFields(resolveInfo(t, categorySubscription))

	changed, err := determineFieldChanged(before, after, watched)
	require.NoError(t, err)

	assert.True(t, changed, "spec.dnsNames changed but was reported unchanged")
}

// resolveInfo parses query and returns the ResolveInfo runWatch would see for
// its single root subscription field.
func resolveInfo(t *testing.T, query string) graphql.ResolveInfo {
	t.Helper()

	doc, err := parser.Parse(parser.ParseParams{Source: query})
	require.NoError(t, err)

	var operation *ast.OperationDefinition
	fragments := map[string]ast.Definition{}

	for _, definition := range doc.Definitions {
		switch def := definition.(type) {
		case *ast.OperationDefinition:
			require.Nil(t, operation, "query has more than one operation")
			operation = def
		case *ast.FragmentDefinition:
			fragments[def.Name.Value] = def
		default:
			t.Fatalf("unexpected definition %T", def)
		}
	}
	require.NotNil(t, operation, "query has no operation")
	require.Len(t, operation.SelectionSet.Selections, 1)

	field, ok := operation.SelectionSet.Selections[0].(*ast.Field)
	require.True(t, ok, "root selection is not a field")

	return graphql.ResolveInfo{FieldASTs: []*ast.Field{field}, Fragments: fragments}
}
