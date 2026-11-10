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
	"testing"
	"time"

	"github.com/kubernetes-sigs/kro/pkg/dynamiccontroller"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// dcConfig is tuned for tests, not a mirror of main.go: a short MaxRetryDelay and shutdown
// timeout keep a converging test fast, and the higher rate limit lets a single workspace churn
// without throttling.
func dcConfig() dynamiccontroller.Config {
	return dynamiccontroller.Config{
		Workers: 2, ResyncPeriod: 10 * time.Hour, QueueMaxRetries: 20,
		MinRetryDelay: 200 * time.Millisecond, MaxRetryDelay: 10 * time.Second,
		RateLimit: 50, BurstLimit: 100, QueueShutdownTimeout: 10 * time.Second,
	}
}

// TestCompositionLifecycle drives the operator's engine directly against a real
// kcp workspace: author an RGD → composite type published as a bound API +
// ContentConfiguration emitted → instance materializes its child + status →
// delete RGD garbage-collects.
func TestCompositionLifecycle(t *testing.T) {
	ctx := t.Context()

	c := newConsumer(t, "kco")
	eng := newEngine(t)

	const rgdName = "greetings.apps.example.com"
	require.NoError(t, c.Client.Create(ctx, rgd(rgdName)))

	// Drive reconcile until it converges: the composite type is published as a bound
	// API (APIResourceSchema + APIExport + self-binding reaches Bound), the instance
	// watch is registered, and the ContentConfiguration is emitted. Convergence (and
	// the instance materialization below) implies the bound type is served.
	wait.Until(t, func() bool {
		return eng.ReconcileRGD(ctx, c.ClusterName, rgdName) == nil
	}, 90*time.Second, 2*time.Second, "RGD never converged")

	t.Log("composite type is published as a bound API (APIBinding Bound)")
	binding := &unstructured.Unstructured{}
	binding.SetGroupVersionKind(schema.GroupVersionKind{Group: "apis.kcp.io", Version: "v1alpha2", Kind: "APIBinding"})
	require.NoError(t, c.Client.Get(ctx, types.NamespacedName{Name: "kro-" + rgdName}, binding))
	phase, _, _ := unstructured.NestedString(binding.Object, "status", "phase")
	require.Equal(t, "Bound", phase)

	t.Log("ContentConfiguration emitted for the generated type")
	cc := &unstructured.Unstructured{}
	cc.SetGroupVersionKind(schema.GroupVersionKind{Group: "ui.platform-mesh.io", Version: "v1alpha1", Kind: "ContentConfiguration"})
	require.NoError(t, c.Client.Get(ctx, types.NamespacedName{Name: "kro-" + rgdName}, cc))
	require.Equal(t, "ResourceGraphDefinition", cc.GetOwnerReferences()[0].Kind)

	t.Log("create an instance; the child ConfigMap materializes with the CEL value")
	require.NoError(t, c.Client.Create(ctx, greeting("g1", "hello from e2e")))
	wait.Until(t, func() bool {
		cm := &unstructured.Unstructured{}
		cm.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"})
		if err := c.Client.Get(ctx, types.NamespacedName{Namespace: "default", Name: "g1-cm"}, cm); err != nil {
			return false
		}
		msg, _, _ := unstructured.NestedString(cm.Object, "data", "message")
		return msg == "hello from e2e"
	}, 60*time.Second, 2*time.Second, "child ConfigMap never materialized")

	t.Log("instance status is written back: kro's ACTIVE state plus a Ready condition")
	wait.Until(t, func() bool {
		g := greeting("g1", "")
		if err := c.Client.Get(ctx, types.NamespacedName{Namespace: "default", Name: "g1"}, g); err != nil {
			return false
		}
		state, _, _ := unstructured.NestedString(g.Object, "status", "state")
		if state != "ACTIVE" {
			return false
		}
		conds, _, _ := unstructured.NestedSlice(g.Object, "status", "conditions")
		for _, raw := range conds {
			if c, ok := raw.(map[string]any); ok && c["type"] == "Ready" {
				return c["status"] == "True"
			}
		}
		return false
	}, 60*time.Second, 2*time.Second, "instance status never written")

	t.Log("delete the RGD; ordered teardown removes the APIBinding first, then the export/schema and ContentConfiguration")
	require.NoError(t, c.Client.Delete(ctx, rgd(rgdName)))
	// The RGD carries the operator's teardown finalizer, so deletion is reconcile-driven:
	// keep reconciling to advance the ordered teardown (APIBinding deleted and awaited
	// gone → APIExport + schemas → finalizer released → RGD + owned CC garbage-collected).
	wait.Until(t, func() bool {
		_ = eng.ReconcileRGD(ctx, c.ClusterName, rgdName)
		bindingObj := &unstructured.Unstructured{}
		bindingObj.SetGroupVersionKind(binding.GroupVersionKind())
		bindingGone := apierrors.IsNotFound(c.Client.Get(ctx, types.NamespacedName{Name: "kro-" + rgdName}, bindingObj))
		ccObj := &unstructured.Unstructured{}
		ccObj.SetGroupVersionKind(cc.GroupVersionKind())
		ccGone := apierrors.IsNotFound(c.Client.Get(ctx, types.NamespacedName{Name: "kro-" + rgdName}, ccObj))
		return bindingGone && ccGone
	}, 90*time.Second, 2*time.Second, "APIBinding/ContentConfiguration not garbage-collected")
}

func rgd(name string) ctrlruntimeclient.Object {
	o := &unstructured.Unstructured{}
	o.SetGroupVersionKind(schema.GroupVersionKind{Group: "kro.run", Version: "v1alpha1", Kind: "ResourceGraphDefinition"})
	o.SetName(name)
	_ = unstructured.SetNestedMap(o.Object, map[string]any{
		"apiVersion": "v1alpha1",
		"kind":       "Greeting",
		"group":      "apps.example.com",
		"spec":       map[string]any{"message": "string"},
	}, "spec", "schema")
	_ = unstructured.SetNestedSlice(o.Object, []any{
		map[string]any{
			"id": "cm",
			"template": map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]any{
					"name":      "${schema.metadata.name}-cm",
					"namespace": "${schema.metadata.namespace}",
				},
				"data": map[string]any{"message": "${schema.spec.message}"},
			},
		},
	}, "spec", "resources")
	return o
}

func greeting(name, message string) *unstructured.Unstructured {
	o := &unstructured.Unstructured{}
	o.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps.example.com", Version: "v1alpha1", Kind: "Greeting"})
	o.SetName(name)
	o.SetNamespace("default")
	if message != "" {
		_ = unstructured.SetNestedField(o.Object, message, "spec", "message")
	}
	return o
}
