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

	krov1alpha1 "github.com/kubernetes-sigs/kro/api/v1alpha1"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/kro-composition-operator/pkg/app"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

// TestUncompilableRGDReportsWhy covers an RGD composing an API the workspace does not serve.
//
// A kcp workspace serves only its bound APIs plus a small core set, so this is what a tenant
// hits the moment they compose anything outside it, a workload API being the usual case. The
// reconcile keeps retrying, because the same failure is what a slow API looks like, and the
// reason has to be on the RGD or the two are indistinguishable.
func TestUncompilableRGDReportsWhy(t *testing.T) {
	ctx := t.Context()

	c := newConsumer(t, "uncompilable")
	eng := newEngine(t)

	const group = "uncompilable.example.com"
	const rgdName = "widgets." + group

	// apps/v1 is not among the groups a kcp workspace serves.
	deployment := map[string]any{
		"id": "workload",
		"template": map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name":      "${schema.spec.name}",
				"namespace": "${schema.metadata.namespace}",
			},
			"spec": map[string]any{
				"selector": map[string]any{"matchLabels": map[string]any{"app": "${schema.spec.name}"}},
				"template": map[string]any{
					"metadata": map[string]any{"labels": map[string]any{"app": "${schema.spec.name}"}},
					"spec": map[string]any{
						"containers": []any{map[string]any{"name": "app", "image": "nginx"}},
					},
				},
			},
		},
	}
	require.NoError(t, c.Client.Create(ctx, compositeRGD(rgdName, group, "Widget",
		map[string]any{"name": "string"}, []any{deployment})))

	err := eng.ReconcileRGD(ctx, c.ClusterName, rgdName)
	require.Error(t, err)
	require.True(t, app.IsTransient(err),
		"a slow API looks the same from here, so this must keep retrying rather than give up")

	rgd := &krov1alpha1.ResourceGraphDefinition{}
	require.NoError(t, c.Client.Get(ctx, types.NamespacedName{Name: rgdName}, rgd))
	require.Equal(t, krov1alpha1.ResourceGraphDefinitionStateInactive, rgd.Status.State)

	accepted := condition(t, rgd, krov1alpha1.RGDConditionTypeGraphAccepted)
	require.Equal(t, metav1.ConditionFalse, accepted.Status)
	require.NotNil(t, accepted.Message)
	// kro reports the unresolvable group version and the id of the resource that wanted it,
	// which is what points a tenant at the line to fix.
	require.Contains(t, *accepted.Message, "apps/v1")
	require.Contains(t, *accepted.Message, "workload")

	// Nothing is published for a graph that never compiled.
	require.True(t, apierrors.IsNotFound(
		c.Client.Get(ctx, types.NamespacedName{Name: "kro-" + rgdName}, &kcpapisv1alpha2.APIExport{})),
		"an uncompilable RGD must not publish a type")

	t.Log("composing a served API instead clears it")
	updateRGD(ctx, t, c, rgdName, func(u *unstructured.Unstructured) {
		require.NoError(t, unstructured.SetNestedSlice(u.Object, []any{
			cmResource("cm", "${schema.spec.name}-cm", map[string]any{"name": "${schema.spec.name}"}),
		}, "spec", "resources"))
	})
	converge(ctx, t, eng, c.ClusterName, rgdName)

	require.NoError(t, c.Client.Get(ctx, types.NamespacedName{Name: rgdName}, rgd))
	require.Equal(t, krov1alpha1.ResourceGraphDefinitionStateActive, rgd.Status.State)
	require.Equal(t, metav1.ConditionTrue,
		condition(t, rgd, krov1alpha1.RGDConditionTypeGraphAccepted).Status)
}
