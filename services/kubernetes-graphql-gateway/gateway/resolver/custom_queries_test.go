package resolver

import (
	"context"
	"errors"
	"testing"

	"github.com/graphql-go/graphql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/kubernetes-graphql-gateway/internal/testfakes"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestResourcesByCategory(t *testing.T) {
	t.Run("fan-out query for two types", func(t *testing.T) {
		foo := unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "resolver.bar/v1",
				"kind":       "Foo",
				"metadata": map[string]any{
					"name": "first",
				},
			},
		}
		bar := unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "resolver.bar/v1",
				"kind":       "Bar",
				"metadata": map[string]any{
					"name": "second",
				},
			},
		}

		client := testfakes.NewClient(
			testfakes.ListItems(foo, bar), nil)
		svc := New(client, nil)

		categoryName := "the-category"
		typeByCat := map[string][]TypeByCategory{
			categoryName: {
				TypeByCategory{Group: "resolver.bar", Version: "v1", Kind: "Foo"},
				TypeByCategory{Group: "resolver.bar", Version: "v1", Kind: "Bar"},
			},
		}

		resolve := svc.ResourcesByCategory(typeByCat)

		result, err := resolve(graphql.ResolveParams{
			Context: t.Context(),
			Args:    map[string]any{"name": categoryName},
		})
		require.NoError(t, err)

		records, ok := result.([]map[string]any)
		require.True(t, ok, "unexpected result type")

		require.Len(t, records, 2)

		receivedNames := make([]string, len(records))
		for i, v := range records {
			receivedNames[i], _, _ = unstructured.NestedString(v, "metadata", "name")
		}
		assert.ElementsMatch(t, []string{"first", "second"}, receivedNames)
	})
	t.Run("zero types in category", func(t *testing.T) {
		client := testfakes.NewClient(testfakes.ListItems(), nil)
		svc := New(client, nil)

		categoryName := "emptycat"
		typeByCat := map[string][]TypeByCategory{
			categoryName: {},
		}

		resolve := svc.ResourcesByCategory(typeByCat)

		result, err := resolve(graphql.ResolveParams{
			Context: t.Context(),
			Args:    map[string]any{"name": categoryName},
		})
		require.NoError(t, err)

		records, ok := result.([]map[string]any)
		require.True(t, ok, "unexpected result type")

		assert.Empty(t, records)
	})
	t.Run("error on List", func(t *testing.T) {
		client := testfakes.NewClient(
			func(ctx context.Context, list ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) error {
				return errors.New("could not list baz")
			},
			nil)
		svc := New(client, nil)

		categoryName := "baz-category"
		typeByCat := map[string][]TypeByCategory{
			categoryName: {
				TypeByCategory{Group: "resolver.baz", Version: "v1", Kind: "Blap"},
			},
		}

		resolve := svc.ResourcesByCategory(typeByCat)

		result, err := resolve(graphql.ResolveParams{
			Context: t.Context(),
			Args:    map[string]any{"name": categoryName},
		})
		require.Error(t, err)
		assert.Nil(t, result)
	})
}
