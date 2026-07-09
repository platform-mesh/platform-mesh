package resolver

import (
	"context"
	"errors"
	"testing"

	"github.com/graphql-go/graphql"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/metrics"
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

		metricsReg := prometheus.NewRegistry()
		m := metrics.NewResolverMetrics(metricsReg)
		svc := New(client, m)

		categoryName := "the-category"
		typeByCat := map[string][]TypeByCategory{
			categoryName: {
				TypeByCategory{Group: "resolver.bar", Version: "v1", Kind: "Foo"},
				TypeByCategory{Group: "resolver.bar", Version: "v1", Kind: "Bar"},
			},
		}

		resolve := svc.ResourcesByCategory(typeByCat)

		raw, err := resolve(graphql.ResolveParams{
			Context: t.Context(),
			Args:    map[string]any{"name": categoryName},
		})
		require.NoError(t, err)

		result, ok := raw.(*CategoryListResult)
		require.True(t, ok, "unexpected result type")

		require.Len(t, result.Items, 2)

		receivedNames := make([]string, len(result.Items))
		for i, v := range result.Items {
			receivedNames[i], _, _ = unstructured.NestedString(v, "metadata", "name")
		}
		assert.ElementsMatch(t, []string{"first", "second"}, receivedNames)

		found, count, sum := histObservation(t, metricsReg, "kubernetes_graphql_gateway_category_fanout_types", "category", categoryName)
		require.True(t, found, "fanout_types not recorded")
		assert.Equal(t, uint64(1), count)
		assert.Equal(t, float64(2), sum)

		found, count, sum = histObservation(t, metricsReg, "kubernetes_graphql_gateway_category_objects_returned", "category", categoryName)
		require.True(t, found, "objects_returned not recorded")
		assert.Equal(t, uint64(1), count)
		assert.Equal(t, float64(2), sum)
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

		records, ok := result.(*CategoryListResult)
		require.True(t, ok, "unexpected result type")
		assert.Empty(t, records.Items)
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

// histObservation returns the sample count and the total sum for a given histogram.
func histObservation(t *testing.T, reg *prometheus.Registry, name, labelName, labelValue string) (found bool, count uint64, sum float64) {
	t.Helper()
	mfs, err := reg.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == labelName && l.GetValue() == labelValue {
					h := m.GetHistogram()
					return true, h.GetSampleCount(), h.GetSampleSum()
				}
			}
		}
	}
	return false, 0, 0
}
