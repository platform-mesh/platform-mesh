package resolver

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/graphql-go/graphql"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/metrics"
	"go.platform-mesh.io/kubernetes-graphql-gateway/internal/testfakes"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

func TestResourcesByCategory_Permissions(t *testing.T) {
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

	forbidden := apierrors.NewForbidden(
		schema.GroupResource{Group: "resolver.bar", Resource: "bars"}, "",
		errors.New("no access"))

	tests := []struct {
		name          string
		deniedKinds   []string
		listErr       error
		expectedNames []string
		expectError   bool
	}{
		{
			name:          "all types allowed",
			expectedNames: []string{"first", "second"},
			expectError:   false,
		},
		{
			name:          "one of two types forbidden",
			deniedKinds:   []string{"Bar"},
			listErr:       forbidden,
			expectedNames: []string{"first"},
			expectError:   false,
		},
		{
			name:          "every type forbidden returns empty result",
			deniedKinds:   []string{"Foo", "Bar"},
			listErr:       forbidden,
			expectedNames: nil,
			expectError:   false,
		},
		{
			name:        "unauthorized is not suppressed",
			deniedKinds: []string{"Bar"},
			listErr:     apierrors.NewUnauthorized("token expired"),
			expectError: true,
		},
		{
			name:        "internal error is not suppressed",
			deniedKinds: []string{"Bar"},
			listErr:     apierrors.NewInternalError(errors.New("etcd unreachable")),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := testfakes.NewClient(
				listErrForKinds(tt.listErr, tt.deniedKinds, foo, bar), nil)
			svc := New(client, nil)

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
			if tt.expectError {
				require.Error(t, err)
				assert.Nil(t, raw)
				return
			}
			require.NoError(t, err)

			result, ok := raw.(*CategoryListResult)
			require.True(t, ok, "unexpected result type")

			receivedNames := make([]string, len(result.Items))
			for i, v := range result.Items {
				receivedNames[i], _, _ = unstructured.NestedString(v, "metadata", "name")
			}
			assert.ElementsMatch(t, tt.expectedNames, receivedNames)
		})
	}
}

func TestSubscribeResourcesByCategory_Permissions(t *testing.T) {
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

	forbidden := apierrors.NewForbidden(
		schema.GroupResource{Group: "resolver.bar", Resource: "bars"}, "",
		errors.New("no access"))

	tests := []struct {
		name          string
		deniedKinds   []string
		listErr       error
		expectedNames []string
		expectError   bool
	}{
		{
			name:          "all types allowed",
			expectedNames: []string{"first", "second"},
			expectError:   false,
		},
		{
			name:          "one of two types forbidden",
			deniedKinds:   []string{"Bar"},
			listErr:       forbidden,
			expectedNames: []string{"first"},
			expectError:   false,
		},
		{
			name:          "all types forbidden",
			deniedKinds:   []string{"Foo", "Bar"},
			listErr:       forbidden,
			expectedNames: nil,
			expectError:   false,
		},
		{
			name:          "unauthorized is not suppressed",
			deniedKinds:   []string{"Bar"},
			listErr:       apierrors.NewUnauthorized("token expired"),
			expectedNames: []string{"first"},
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			client := testfakes.NewClient(
				listErrForKinds(tt.listErr, tt.deniedKinds, foo, bar), nil)
			svc := New(client, nil)

			categoryName := "the-category"
			typeByCat := map[string][]TypeByCategory{
				categoryName: {
					TypeByCategory{Group: "resolver.bar", Version: "v1", Kind: "Foo"},
					TypeByCategory{Group: "resolver.bar", Version: "v1", Kind: "Bar"},
				},
			}

			subscribe := svc.SubscribeResourcesByCategory(typeByCat)

			raw, err := subscribe(graphql.ResolveParams{
				Context: ctx,
				Args:    map[string]any{"name": categoryName},
			})
			require.NoError(t, err)

			outCh, ok := raw.(chan any)
			require.True(t, ok, "unexpected result type")

			receivedNames, receivedErr := drainEvents(t, outCh)
			assert.ElementsMatch(t, tt.expectedNames, receivedNames)

			if tt.expectError {
				assert.Error(t, receivedErr)
				return
			}
			assert.NoError(t, receivedErr)
		})
	}
}

// drainEvents collects the names of all objects, as well as the last error.
func drainEvents(t *testing.T, outCh <-chan any) (names []string, err error) {
	t.Helper()
	for {
		select {
		case event, ok := <-outCh:
			if !ok {
				return names, err
			}
			switch v := event.(type) {
			case error:
				err = v
			case SubscriptionEnvelope:
				obj, _ := v.Object.(map[string]any)
				name, _, _ := unstructured.NestedString(obj, "metadata", "name")
				names = append(names, name)
			default:
				t.Fatalf("unexpected event type %T", event)
			}
		case <-time.After(200 * time.Millisecond):
			return names, err
		}
	}
}

// listErrForKinds returns a List function which fails with listErr for deniedKinds.
func listErrForKinds(
	listErr error,
	deniedKinds []string,
	objs ...unstructured.Unstructured,
) func(context.Context, ctrlruntimeclient.ObjectList, ...ctrlruntimeclient.ListOption) error {
	serve := testfakes.ListItems(objs...)
	return func(ctx context.Context, list ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) error {
		kind := strings.TrimSuffix(list.GetObjectKind().GroupVersionKind().Kind, "List")
		if slices.Contains(deniedKinds, kind) {
			return listErr
		}
		return serve(ctx, list, opts...)
	}
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
