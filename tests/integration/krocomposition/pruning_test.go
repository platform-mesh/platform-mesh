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
	"fmt"
	"strings"
	"testing"
	"time"

	krov1alpha1 "github.com/kubernetes-sigs/kro/api/v1alpha1"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// These cover the operator's destructive paths. They exist against a real kcp because the
// behaviour they assert depends on finalizers, owner-reference collection and the APIExport
// lifecycle, none of which a fake client represents.

var nsGVK = schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}

// TestPruneOnForEachShrink covers a collection losing an element. Applying alone never
// removes the child that element produced, so without pruning it would outlive the spec.
func TestPruneOnForEachShrink(t *testing.T) {
	ctx := t.Context()

	c := newConsumer(t, "shrink")
	eng := newEngine(t)

	const rgdName = "regions.shrink.example.com"
	require.NoError(t, c.Client.Create(ctx, compositeRGD(rgdName, "shrink.example.com", "Regions",
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
					"data": map[string]any{"region": "${value}"},
				},
			},
		})))
	converge(ctx, t, eng, c.ClusterName, rgdName)

	require.NoError(t, c.Client.Create(ctx, compositeInstance("shrink.example.com", "Regions", "edge", "default",
		map[string]any{"name": "edge", "values": []any{"eu", "us", "ap"}})))

	t.Log("all three elements materialize")
	requireConfigMaps(ctx, t, c, []string{"edge-eu", "edge-us", "edge-ap"}, nil)

	t.Log("drop one element from the list")
	setInstanceValues(ctx, t, c, "shrink.example.com", "Regions", "edge", []any{"eu", "us"})

	t.Log("the dropped element's ConfigMap goes, the remaining two stay")
	requireConfigMaps(ctx, t, c, []string{"edge-eu", "edge-us"}, []string{"edge-ap"})
}

// TestPruneOnIncludeWhenFalse covers a conditional resource being switched off. The RGD
// schema documents that resources may be pruned as conditions change.
func TestPruneOnIncludeWhenFalse(t *testing.T) {
	ctx := t.Context()

	c := newConsumer(t, "toggle")
	eng := newEngine(t)

	const rgdName = "toggles.toggle.example.com"
	require.NoError(t, c.Client.Create(ctx, compositeRGD(rgdName, "toggle.example.com", "Toggle",
		map[string]any{"name": "string", "extras": "boolean"},
		[]any{
			cmResource("base", "${schema.spec.name}-base", map[string]any{"kind": "base"}),
			map[string]any{
				"id":          "extras",
				"includeWhen": []any{"${schema.spec.extras}"},
				"template": map[string]any{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]any{
						"name":      "${schema.spec.name}-extras",
						"namespace": "${schema.metadata.namespace}",
					},
					"data": map[string]any{"kind": "extras"},
				},
			},
		})))
	converge(ctx, t, eng, c.ClusterName, rgdName)

	require.NoError(t, c.Client.Create(ctx, compositeInstance("toggle.example.com", "Toggle", "flags", "default",
		map[string]any{"name": "flags", "extras": true})))

	t.Log("with the condition true both children exist")
	requireConfigMaps(ctx, t, c, []string{"flags-base", "flags-extras"}, nil)

	t.Log("switch the condition off")
	patchInstance(ctx, t, c, "toggle.example.com", "Toggle", "flags", func(u *unstructured.Unstructured) {
		require.NoError(t, unstructured.SetNestedField(u.Object, false, "spec", "extras"))
	})

	t.Log("the conditional child goes, the unconditional one stays")
	requireConfigMaps(ctx, t, c, []string{"flags-base"}, []string{"flags-extras"})
}

// TestInstanceDeleteCollectsClusterScopedChild is the case owner-reference GC cannot handle:
// a namespaced instance may not own a cluster-scoped object, so without the explicit cleanup
// the Namespace would outlive the instance that created it.
func TestInstanceDeleteCollectsClusterScopedChild(t *testing.T) {
	ctx := t.Context()

	c := newConsumer(t, "clusterchild")
	eng := newEngine(t)

	const rgdName = "projects.cluster.example.com"
	require.NoError(t, c.Client.Create(ctx, compositeRGD(rgdName, "cluster.example.com", "Project",
		map[string]any{"name": "string"},
		[]any{
			map[string]any{
				"id": "ns",
				"template": map[string]any{
					"apiVersion": "v1",
					"kind":       "Namespace",
					"metadata":   map[string]any{"name": "proj-${schema.spec.name}"},
				},
			},
		})))
	converge(ctx, t, eng, c.ClusterName, rgdName)

	instance := compositeInstance("cluster.example.com", "Project", "apollo", "default",
		map[string]any{"name": "apollo"})
	require.NoError(t, c.Client.Create(ctx, instance))

	t.Log("the cluster-scoped child is created")
	wait.Until(t, func() bool {
		_, err := uGet(ctx, c.Client, nsGVK, "", "proj-apollo")
		return err == nil
	}, 60*time.Second, 2*time.Second, "composed Namespace never appeared")

	t.Log("kro's instance reconciler holds the instance with its finalizer while it owns children")
	wait.Until(t, func() bool {
		cur, err := uGet(ctx, c.Client, instance.GroupVersionKind(), "default", "apollo")
		if err != nil {
			return false
		}
		for _, f := range cur.GetFinalizers() {
			if f == "kro.run/finalizer" {
				return true
			}
		}
		return false
	}, 60*time.Second, 2*time.Second, "instance never took kro's finalizer")

	t.Log("deleting the instance collects the Namespace and releases the finalizer")
	require.NoError(t, c.Client.Delete(ctx, instance))
	wait.Until(t, func() bool {
		_, instErr := uGet(ctx, c.Client, instance.GroupVersionKind(), "default", "apollo")
		return namespaceCollected(ctx, c, "proj-apollo") && apierrors.IsNotFound(instErr)
	}, 90*time.Second, 2*time.Second, "cluster-scoped child or instance never cleaned up")
}

// TestRGDDeleteDrainsInstances covers the ordering hazard the drain exists for: deleting the
// APIBinding unserves the type, and an instance still holding the cleanup finalizer would
// then be unreachable forever. Instances must be drained while the type still exists.
func TestRGDDeleteDrainsInstances(t *testing.T) {
	ctx := t.Context()

	c := newConsumer(t, "drain")
	eng := newEngine(t)

	const rgdName = "drainers.drain.example.com"
	require.NoError(t, c.Client.Create(ctx, compositeRGD(rgdName, "drain.example.com", "Drainer",
		map[string]any{"name": "string"},
		[]any{
			// A cluster-scoped child, so the instance carries the finalizer and the drain
			// has something real to order against.
			map[string]any{
				"id": "ns",
				"template": map[string]any{
					"apiVersion": "v1",
					"kind":       "Namespace",
					"metadata":   map[string]any{"name": "drain-${schema.spec.name}"},
				},
			},
		})))
	converge(ctx, t, eng, c.ClusterName, rgdName)

	instance := compositeInstance("drain.example.com", "Drainer", "d1", "default",
		map[string]any{"name": "d1"})
	require.NoError(t, c.Client.Create(ctx, instance))
	wait.Until(t, func() bool {
		_, err := uGet(ctx, c.Client, nsGVK, "", "drain-d1")
		return err == nil
	}, 60*time.Second, 2*time.Second, "composed Namespace never appeared")

	t.Log("delete the RGD while an instance is live")
	rgdObj := compositeRGD(rgdName, "drain.example.com", "Drainer", nil, nil)
	require.NoError(t, c.Client.Delete(ctx, rgdObj))

	t.Log("teardown completes: the instance is drained rather than stranded")
	wait.Until(t, func() bool {
		if err := eng.ReconcileRGD(ctx, c.ClusterName, rgdName); err != nil {
			return false
		}
		_, err := uGet(ctx, c.Client, rgdObj.GroupVersionKind(), "", rgdName)
		return apierrors.IsNotFound(err)
	}, 120*time.Second, 2*time.Second, "RGD teardown never completed")

	t.Log("no instance is left holding a finalizer against a type that no longer exists")
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(instance.GroupVersionKind())
	err := c.Client.List(ctx, list)
	if err == nil {
		require.Empty(t, list.Items, "instances outlived the composite type")
	} else {
		// The type being unserved is the expected end state.
		require.True(t, apierrors.IsNotFound(err) || meta.IsNoMatchError(err), "unexpected list error: %v", err)
	}
}

// requireConfigMaps waits until every name in want exists and every name in gone does not.
func requireConfigMaps(ctx context.Context, t *testing.T, c *consumer, want, gone []string) {
	t.Helper()
	wait.Until(t, func() bool {
		for _, name := range want {
			if _, err := uGet(ctx, c.Client, cmGVK, "default", name); err != nil {
				return false
			}
		}
		for _, name := range gone {
			if _, err := uGet(ctx, c.Client, cmGVK, "default", name); !apierrors.IsNotFound(err) {
				return false
			}
		}
		return true
	}, 90*time.Second, 2*time.Second, "want=%v gone=%v never reached", want, gone)
}

// patchInstance mutates a live instance, which is what re-triggers the instance handler.
func patchInstance(ctx context.Context, t *testing.T, c *consumer, group, kind, name string, mutate func(*unstructured.Unstructured)) {
	t.Helper()
	gvk := schema.GroupVersionKind{Group: group, Version: "v1alpha1", Kind: kind}
	wait.Until(t, func() bool {
		cur, err := uGet(ctx, c.Client, gvk, "default", name)
		if err != nil {
			return false
		}
		mutate(cur)
		return c.Client.Update(ctx, cur) == nil
	}, 30*time.Second, time.Second, "could not update instance %s/%s", kind, name)
}

func setInstanceValues(ctx context.Context, t *testing.T, c *consumer, group, kind, name string, values []any) {
	t.Helper()
	patchInstance(ctx, t, c, group, kind, name, func(u *unstructured.Unstructured) {
		require.NoError(t, unstructured.SetNestedSlice(u.Object, values, "spec", "values"))
	})
}

// namespaceCollected reports whether the operator's delete took effect. There is no
// kube-controller-manager here, so nothing finalizes a Namespace: Terminating is the expected
// end state rather than absence, and either counts.
func namespaceCollected(ctx context.Context, c *consumer, name string) bool {
	ns, err := uGet(ctx, c.Client, nsGVK, "", name)
	if apierrors.IsNotFound(err) {
		return true
	}
	if err != nil {
		return false
	}
	return !ns.GetDeletionTimestamp().IsZero()
}

// TestReadyWhenGatesDependents covers readyWhen: until the gate resource satisfies its
// condition, nothing downstream of it may be applied. Without gating, the dependent would be
// created immediately against a resource the author declared incomplete.
func TestReadyWhenGatesDependents(t *testing.T) {
	ctx := t.Context()

	c := newConsumer(t, "readywhen")
	eng := newEngine(t)

	// The gate lives outside the composition, so the test controls when it becomes ready.
	require.NoError(t, c.Client.Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "gate-cm", Namespace: "default"},
		Data:       map[string]string{"ready": "no", "value": "from-gate"},
	}))

	const rgdName = "gated.ready.example.com"
	require.NoError(t, c.Client.Create(ctx, compositeRGD(rgdName, "ready.example.com", "Gated",
		map[string]any{"name": "string"},
		[]any{
			map[string]any{
				"id": "gate",
				"externalRef": map[string]any{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata":   map[string]any{"name": "gate-cm", "namespace": "default"},
				},
				"readyWhen": []any{`${gate.data.ready == "yes"}`},
			},
			// Reading from the gate makes this a dependent, so it sits after the gate in
			// topological order.
			cmResource("out", "${schema.spec.name}-out", map[string]any{"value": "${gate.data.value}"}),
		})))
	converge(ctx, t, eng, c.ClusterName, rgdName)

	// The dependency edge the graph resolved is reported on the RGD, so the wiring is visible
	// without reading the operator's logs. Asserted here because this is the only fixture with
	// one resource reading from another.
	t.Log("status.resources reports the resolved dependency")
	rgd := &krov1alpha1.ResourceGraphDefinition{}
	require.NoError(t, c.Client.Get(ctx, ctrlruntimeclient.ObjectKey{Name: rgdName}, rgd))
	require.Equal(t, []krov1alpha1.ResourceInformation{
		{ID: "out", Dependencies: []krov1alpha1.Dependency{{ID: "gate"}}},
	}, rgd.Status.Resources)
	require.Equal(t, []string{"gate", "out"}, rgd.Status.TopologicalOrder)

	require.NoError(t, c.Client.Create(ctx, compositeInstance("ready.example.com", "Gated", "g1", "default",
		map[string]any{"name": "g1"})))

	t.Log("while the gate is not ready the dependent must not be created")
	require.Never(t, func() bool {
		_, err := uGet(ctx, c.Client, cmGVK, "default", "g1-out")
		return err == nil
	}, 15*time.Second, 2*time.Second, "dependent was created before its gate was ready")

	t.Log("and the instance says so: IN_PROGRESS, Ready=False, naming what it waits for")
	wait.Until(t, func() bool {
		st := instanceStatus(ctx, t, c, "ready.example.com", "Gated", "g1")
		if st["state"] != "IN_PROGRESS" {
			return false
		}
		cond := readyCond(st)
		return cond != nil && cond["status"] == "False" && strings.Contains(fmt.Sprint(cond["message"]), "readyWhen")
	}, 60*time.Second, 2*time.Second, "instance never reported IN_PROGRESS with a reason")

	t.Log("satisfy the gate")
	gate := &corev1.ConfigMap{}
	require.NoError(t, c.Client.Get(ctx, ctrlruntimeclient.ObjectKey{Namespace: "default", Name: "gate-cm"}, gate))
	gate.Data["ready"] = "yes"
	require.NoError(t, c.Client.Update(ctx, gate))

	t.Log("the dependent is now applied, carrying the value read from the gate")
	wait.Until(t, func() bool {
		cm, err := uGet(ctx, c.Client, cmGVK, "default", "g1-out")
		if err != nil {
			return false
		}
		v, _, _ := unstructured.NestedString(cm.Object, "data", "value")
		return v == "from-gate"
	}, 90*time.Second, 2*time.Second, "dependent never applied after the gate became ready")

	t.Log("and the instance flips to ACTIVE with Ready=True")
	wait.Until(t, func() bool {
		st := instanceStatus(ctx, t, c, "ready.example.com", "Gated", "g1")
		cond := readyCond(st)
		return st["state"] == "ACTIVE" && cond != nil && cond["status"] == "True"
	}, 60*time.Second, 2*time.Second, "instance never reported ACTIVE")
}

// instanceStatus reads a composite instance's status map.
func instanceStatus(ctx context.Context, t *testing.T, c *consumer, group, kind, name string) map[string]any {
	t.Helper()
	gvk := schema.GroupVersionKind{Group: group, Version: "v1alpha1", Kind: kind}
	cur, err := uGet(ctx, c.Client, gvk, "default", name)
	if err != nil {
		return nil
	}
	status, _, _ := unstructured.NestedMap(cur.Object, "status")
	return status
}

func readyCond(status map[string]any) map[string]any {
	conds, _ := status["conditions"].([]any)
	for _, raw := range conds {
		if c, ok := raw.(map[string]any); ok && c["type"] == "Ready" {
			return c
		}
	}
	return nil
}
