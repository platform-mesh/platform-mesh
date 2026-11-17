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

	krov1alpha1 "github.com/kubernetes-sigs/kro/api/v1alpha1"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

var rgdGVK = schema.GroupVersionKind{Group: "kro.run", Version: "v1alpha1", Kind: "ResourceGraphDefinition"}

// TestSchemaChangeCompatibility covers RGD edits that change the composite type's schema.
//
// It runs against a real kcp because the thing under test is what the workspace serves:
// publishing means minting a new immutable APIResourceSchema and pointing the APIExport at it,
// and kcp validates each schema on its own without regard for the one it replaces. Nothing but
// this check stands between an RGD edit and a schema swap under live instances.
func TestSchemaChangeCompatibility(t *testing.T) {
	ctx := t.Context()

	c := newConsumer(t, "schemachange")
	eng := newEngine(t)

	const group = "schemachange.example.com"
	const rgdName = "widgets." + group

	// tier is in the schema but not referenced by the template, so removing it later leaves an
	// RGD that still compiles and reaches the compatibility check.
	require.NoError(t, c.Client.Create(ctx, compositeRGD(rgdName, group, "Widget",
		map[string]any{"name": "string", "image": "string", "tier": "string"},
		[]any{cmResource("cm", "${schema.spec.name}-cm", map[string]any{"image": "${schema.spec.image}"})})))
	converge(ctx, t, eng, c.ClusterName, rgdName)

	first := servedSchemaName(ctx, t, c, rgdName)

	require.NoError(t, c.Client.Create(ctx, compositeInstance(group, "Widget", "w1", "default",
		map[string]any{"name": "w1", "image": "nginx", "tier": "gold"})))
	requireConfigMaps(ctx, t, c, []string{"w1-cm"}, nil)

	t.Log("a compatible change is published")
	// The schema round-trips through the APIResourceSchema before it can be compared, so an
	// added field also covers the conversion not inventing changes of its own. If it did, every
	// edit would look breaking, and a refusal is not an error, so only the served schema moving
	// shows the change went through.
	setSchemaSpec(ctx, t, c, rgdName, map[string]any{
		"name": "string", "image": "string", "tier": "string", "notes": "string",
	})
	converge(ctx, t, eng, c.ClusterName, rgdName)
	widened := servedSchemaName(ctx, t, c, rgdName)
	require.NotEqual(t, first, widened, "widening the schema must be published")

	t.Log("a breaking change is refused and the served schema does not move")
	setSchemaSpec(ctx, t, c, rgdName, map[string]any{"name": "string", "image": "string"})
	require.NoError(t, eng.ReconcileRGD(ctx, c.ClusterName, rgdName),
		"a refusal is terminal: only an RGD edit can resolve it, and that reconciles on its own")
	require.Equal(t, widened, servedSchemaName(ctx, t, c, rgdName),
		"the APIExport must still point at the schema it was serving")

	rgd := &krov1alpha1.ResourceGraphDefinition{}
	require.NoError(t, c.Client.Get(ctx, types.NamespacedName{Name: rgdName}, rgd))
	require.Equal(t, krov1alpha1.ResourceGraphDefinitionStateInactive, rgd.Status.State)
	kindReady := condition(t, rgd, krov1alpha1.RGDConditionTypeKindReady)
	require.Equal(t, metav1.ConditionFalse, kindReady.Status)
	require.NotNil(t, kindReady.Message)
	require.Contains(t, *kindReady.Message, "tier", "the message must name what would break")
	require.Contains(t, *kindReady.Message, krov1alpha1.AllowBreakingChangesAnnotation,
		"the message must name the way out")
	require.Equal(t, metav1.ConditionTrue, condition(t, rgd, krov1alpha1.RGDConditionTypeGraphAccepted).Status,
		"the RGD still compiles, so only KindReady moves")

	t.Log("the existing instance keeps working while the change is refused")
	requireConfigMaps(ctx, t, c, []string{"w1-cm"}, nil)

	t.Log("the opt-out annotation publishes the same change")
	updateRGD(ctx, t, c, rgdName, func(u *unstructured.Unstructured) {
		u.SetAnnotations(map[string]string{krov1alpha1.AllowBreakingChangesAnnotation: "true"})
	})
	converge(ctx, t, eng, c.ClusterName, rgdName)
	require.NotEqual(t, widened, servedSchemaName(ctx, t, c, rgdName),
		"with the annotation set the new schema must be served")

	rgd = &krov1alpha1.ResourceGraphDefinition{}
	require.NoError(t, c.Client.Get(ctx, types.NamespacedName{Name: rgdName}, rgd))
	require.Equal(t, krov1alpha1.ResourceGraphDefinitionStateActive, rgd.Status.State,
		"the RGD must recover once the change goes through")
}

// TestSchemaMetadataReachesPublishedSchema covers spec.schema.metadata. kro applies it to the
// CRD it generates, and the composite type's CRD here is the APIResourceSchema.
func TestSchemaMetadataReachesPublishedSchema(t *testing.T) {
	ctx := t.Context()

	c := newConsumer(t, "schemameta")
	eng := newEngine(t)

	const group = "schemameta.example.com"
	const rgdName = "gadgets." + group

	rgd := compositeRGD(rgdName, group, "Gadget",
		map[string]any{"name": "string"},
		[]any{cmResource("cm", "${schema.spec.name}-cm", map[string]any{"name": "${schema.spec.name}"})})
	require.NoError(t, unstructured.SetNestedMap(rgd.Object, map[string]any{
		"labels":      map[string]any{"team": "platform"},
		"annotations": map[string]any{"docs.example.com/url": "https://example.com/gadget"},
	}, "spec", "schema", "metadata"))
	require.NoError(t, c.Client.Create(ctx, rgd))
	converge(ctx, t, eng, c.ClusterName, rgdName)

	ars := &kcpapisv1alpha1.APIResourceSchema{}
	require.NoError(t, c.Client.Get(ctx,
		types.NamespacedName{Name: servedSchemaName(ctx, t, c, rgdName)}, ars))
	require.Equal(t, "platform", ars.Labels["team"])
	require.Equal(t, "https://example.com/gadget", ars.Annotations["docs.example.com/url"])
}

// servedSchemaName is the APIResourceSchema the consumer workspace currently serves the
// composite type from.
func servedSchemaName(ctx context.Context, t *testing.T, c *consumer, rgdName string) string {
	t.Helper()
	export := &kcpapisv1alpha2.APIExport{}
	require.NoError(t, c.Client.Get(ctx, types.NamespacedName{Name: "kro-" + rgdName}, export))
	require.Len(t, export.Spec.Resources, 1)
	return export.Spec.Resources[0].Schema
}

func setSchemaSpec(ctx context.Context, t *testing.T, c *consumer, rgdName string, spec map[string]any) {
	t.Helper()
	updateRGD(ctx, t, c, rgdName, func(u *unstructured.Unstructured) {
		require.NoError(t, unstructured.SetNestedMap(u.Object, spec, "spec", "schema", "spec"))
	})
}

func updateRGD(ctx context.Context, t *testing.T, c *consumer, rgdName string, mutate func(*unstructured.Unstructured)) {
	t.Helper()
	wait.Until(t, func() bool {
		cur, err := uGet(ctx, c.Client, rgdGVK, "", rgdName)
		if err != nil {
			return false
		}
		mutate(cur)
		return c.Client.Update(ctx, cur) == nil
	}, 30*time.Second, time.Second, "could not update RGD %s", rgdName)
}

func condition(t *testing.T, rgd *krov1alpha1.ResourceGraphDefinition, want krov1alpha1.ConditionType) krov1alpha1.Condition {
	t.Helper()
	for _, cond := range rgd.Status.Conditions {
		if cond.Type == want {
			return cond
		}
	}
	t.Fatalf("condition %s not set on RGD %s", want, rgd.Name)
	return krov1alpha1.Condition{}
}
