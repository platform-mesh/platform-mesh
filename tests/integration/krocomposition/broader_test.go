//go:build integration

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

package krocomposition

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/kro-composition-operator/pkg/app"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// TestMultiResourceComposition covers one RGD composing two distinct kinds
// (ConfigMap + Secret) from a single instance, and instance-delete garbage
// collection: deleting the instance collects every child via its owner refs.
func TestMultiResourceComposition(t *testing.T) {
	ctx := t.Context()

	c := newConsumer(t, "multi")
	eng := newEngine(t)

	const rgdName = "bundles.multi.example.com"
	require.NoError(t, c.Client.Create(ctx, compositeRGD(rgdName, "multi.example.com", "Bundle",
		map[string]any{"name": "string", "token": "string"},
		[]any{
			cmResource("cm", "${schema.spec.name}-cm", map[string]any{"who": "${schema.spec.name}"}),
			map[string]any{
				"id": "sec",
				"template": map[string]any{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]any{
						"name":      "${schema.spec.name}-sec",
						"namespace": "${schema.metadata.namespace}",
					},
					"stringData": map[string]any{"token": "${schema.spec.token}"},
				},
			},
		})))
	converge(ctx, t, eng, c.ClusterName, rgdName)

	inst := compositeInstance("multi.example.com", "Bundle", "b1", "default",
		map[string]any{"name": "b1", "token": "s3cr3t"})
	require.NoError(t, c.Client.Create(ctx, inst))

	t.Log("both children (ConfigMap + Secret) materialize from the single instance")
	wait.Until(t, func() bool {
		cm, err := uGet(ctx, c.Client, cmGVK, "default", "b1-cm")
		if err != nil {
			return false
		}
		who, _, _ := unstructured.NestedString(cm.Object, "data", "who")
		_, secErr := uGet(ctx, c.Client, secretGVK, "default", "b1-sec")
		return who == "b1" && secErr == nil
	}, 60*time.Second, 2*time.Second, "both children not materialized")

	t.Log("delete the instance; both children are garbage-collected via owner refs")
	require.NoError(t, c.Client.Delete(ctx, inst))
	wait.Until(t, func() bool {
		_, cmErr := uGet(ctx, c.Client, cmGVK, "default", "b1-cm")
		_, secErr := uGet(ctx, c.Client, secretGVK, "default", "b1-sec")
		return apierrors.IsNotFound(cmErr) && apierrors.IsNotFound(secErr)
	}, 60*time.Second, 2*time.Second, "children not garbage-collected after instance delete")
}

// TestForEachCollection covers a forEach collection resource: one templated
// resource expands into N children keyed by a list field on the instance.
func TestForEachCollection(t *testing.T) {
	ctx := t.Context()

	c := newConsumer(t, "foreach")
	eng := newEngine(t)

	const rgdName = "multicms.foreach.example.com"
	require.NoError(t, c.Client.Create(ctx, compositeRGD(rgdName, "foreach.example.com", "MultiCM",
		map[string]any{"name": "string", "values": "[]string"},
		[]any{
			map[string]any{
				"id":      "cms",
				"forEach": []any{map[string]any{"value": "${schema.spec.values}"}},
				"template": map[string]any{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]any{
						"name":      "${schema.spec.name}-${value}",
						"namespace": "${schema.metadata.namespace}",
					},
					"data": map[string]any{"key": "${value}"},
				},
			},
		})))
	converge(ctx, t, eng, c.ClusterName, rgdName)

	require.NoError(t, c.Client.Create(ctx, compositeInstance("foreach.example.com", "MultiCM", "batch", "default",
		map[string]any{"name": "batch", "values": []any{"alpha", "beta", "gamma"}})))

	want := map[string]string{"batch-alpha": "alpha", "batch-beta": "beta", "batch-gamma": "gamma"}
	t.Log("the collection expands into one ConfigMap per value, each carrying its element")
	wait.Until(t, func() bool {
		for name, val := range want {
			cm, err := uGet(ctx, c.Client, cmGVK, "default", name)
			if err != nil {
				return false
			}
			if k, _, _ := unstructured.NestedString(cm.Object, "data", "key"); k != val {
				return false
			}
		}
		return true
	}, 60*time.Second, 2*time.Second, "forEach ConfigMaps not all materialized")

	t.Log("delete the instance; every collection child is garbage-collected")
	require.NoError(t, c.Client.Delete(ctx, compositeInstance("foreach.example.com", "MultiCM", "batch", "default", nil)))
	wait.Until(t, func() bool {
		for name := range want {
			if _, err := uGet(ctx, c.Client, cmGVK, "default", name); !apierrors.IsNotFound(err) {
				return false
			}
		}
		return true
	}, 60*time.Second, 2*time.Second, "forEach ConfigMaps not garbage-collected")
}

// TestExternalRef covers an externalRef resource: the RGD reads an existing
// resource it does not own and feeds its values into a composed child.
func TestExternalRef(t *testing.T) {
	ctx := t.Context()

	c := newConsumer(t, "extref")
	eng := newEngine(t)

	// the external resource the RGD reads (created outside the composition).
	require.NoError(t, c.Client.Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "ext-src", Namespace: "default"},
		Data:       map[string]string{"color": "blue"},
	}))

	const rgdName = "painters.paint.example.com"
	require.NoError(t, c.Client.Create(ctx, compositeRGD(rgdName, "paint.example.com", "Painter",
		map[string]any{"name": "string"},
		[]any{
			map[string]any{
				"id": "src",
				"externalRef": map[string]any{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata":   map[string]any{"name": "ext-src", "namespace": "default"},
				},
			},
			cmResource("out", "${schema.spec.name}-out", map[string]any{"color": "${src.data.color}"}),
		})))
	converge(ctx, t, eng, c.ClusterName, rgdName)

	require.NoError(t, c.Client.Create(ctx, compositeInstance("paint.example.com", "Painter", "p1", "default",
		map[string]any{"name": "p1"})))

	t.Log("the child carries the value read from the external resource")
	wait.Until(t, func() bool {
		cm, err := uGet(ctx, c.Client, cmGVK, "default", "p1-out")
		if err != nil {
			return false
		}
		color, _, _ := unstructured.NestedString(cm.Object, "data", "color")
		return color == "blue"
	}, 60*time.Second, 2*time.Second, "external-ref value not propagated to child")
}

// TestMultiTenantIsolation covers per-workspace isolation: two consumer
// workspaces each publish their own composite type; neither the published
// APIBinding nor the materialized instances cross the workspace boundary.
func TestMultiTenantIsolation(t *testing.T) {
	ctx := t.Context()

	a := newConsumer(t, "iso-a")
	b := newConsumer(t, "iso-b")
	eng := newEngine(t)

	const rgdA = "widgets.a.example.com"
	const rgdB = "widgets.b.example.com"
	require.NoError(t, a.Client.Create(ctx, compositeRGD(rgdA, "a.example.com", "WidgetA",
		map[string]any{"name": "string"},
		[]any{cmResource("cm", "${schema.spec.name}-cm", map[string]any{"tenant": "a"})})))
	require.NoError(t, b.Client.Create(ctx, compositeRGD(rgdB, "b.example.com", "WidgetB",
		map[string]any{"name": "string"},
		[]any{cmResource("cm", "${schema.spec.name}-cm", map[string]any{"tenant": "b"})})))

	converge(ctx, t, eng, a.ClusterName, rgdA)
	converge(ctx, t, eng, b.ClusterName, rgdB)

	t.Log("each workspace only carries its own published APIBinding")
	_, err := uGet(ctx, a.Client, bindingGVK, "", "kro-"+rgdA)
	require.NoError(t, err, "tenant A's binding missing in A")
	_, err = uGet(ctx, b.Client, bindingGVK, "", "kro-"+rgdB)
	require.NoError(t, err, "tenant B's binding missing in B")
	_, err = uGet(ctx, b.Client, bindingGVK, "", "kro-"+rgdA)
	require.True(t, apierrors.IsNotFound(err), "tenant A's binding leaked into B")
	_, err = uGet(ctx, a.Client, bindingGVK, "", "kro-"+rgdB)
	require.True(t, apierrors.IsNotFound(err), "tenant B's binding leaked into A")

	t.Log("an instance in A materializes only in A, never in B")
	require.NoError(t, a.Client.Create(ctx, compositeInstance("a.example.com", "WidgetA", "wa", "default",
		map[string]any{"name": "wa"})))
	wait.Until(t, func() bool {
		_, err := uGet(ctx, a.Client, cmGVK, "default", "wa-cm")
		return err == nil
	}, 60*time.Second, 2*time.Second, "tenant A child not materialized")
	_, err = uGet(ctx, b.Client, cmGVK, "default", "wa-cm")
	require.True(t, apierrors.IsNotFound(err), "tenant A child leaked into B")
}

// --- helpers ---

var (
	cmGVK      = schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}
	secretGVK  = schema.GroupVersionKind{Version: "v1", Kind: "Secret"}
	bindingGVK = schema.GroupVersionKind{Group: "apis.kcp.io", Version: "v1alpha2", Kind: "APIBinding"}
)

// converge drives reconcile until the composite type is published and bound.
func converge(ctx context.Context, t *testing.T, eng *app.Engine, cluster, rgdName string) {
	t.Helper()
	wait.Until(t, func() bool {
		return eng.ReconcileRGD(ctx, cluster, rgdName) == nil
	}, 90*time.Second, 2*time.Second, "RGD %q never converged", rgdName)
}

// compositeRGD builds an RGD publishing a composite type with the given schema
// spec fields and resource list.
func compositeRGD(name, group, kind string, schemaSpec map[string]any, resources []any) *unstructured.Unstructured {
	o := &unstructured.Unstructured{}
	o.SetGroupVersionKind(schema.GroupVersionKind{Group: "kro.run", Version: "v1alpha1", Kind: "ResourceGraphDefinition"})
	o.SetName(name)
	_ = unstructured.SetNestedMap(o.Object, map[string]any{
		"apiVersion": "v1alpha1",
		"kind":       kind,
		"group":      group,
		"spec":       schemaSpec,
	}, "spec", "schema")
	_ = unstructured.SetNestedSlice(o.Object, resources, "spec", "resources")
	return o
}

func compositeInstance(group, kind, name, namespace string, spec map[string]any) *unstructured.Unstructured {
	o := &unstructured.Unstructured{}
	o.SetGroupVersionKind(schema.GroupVersionKind{Group: group, Version: "v1alpha1", Kind: kind})
	o.SetName(name)
	o.SetNamespace(namespace)
	if spec != nil {
		_ = unstructured.SetNestedMap(o.Object, spec, "spec")
	}
	return o
}

// cmResource is a ConfigMap template resource co-located with the instance
// (so instance-delete owner-ref GC applies).
func cmResource(id, name string, data map[string]any) map[string]any {
	return map[string]any{
		"id": id,
		"template": map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      name,
				"namespace": "${schema.metadata.namespace}",
			},
			"data": data,
		},
	}
}

func uGet(ctx context.Context, cl ctrlruntimeclient.Client, gvk schema.GroupVersionKind, ns, name string) (*unstructured.Unstructured, error) {
	o := &unstructured.Unstructured{}
	o.SetGroupVersionKind(gvk)
	err := cl.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, o)
	return o, err
}
