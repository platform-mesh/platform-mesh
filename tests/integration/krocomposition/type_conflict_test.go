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

	krov1alpha1 "github.com/kubernetes-sigs/kro/api/v1alpha1"
	krometadata "github.com/kubernetes-sigs/kro/pkg/metadata"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/kro-composition-operator/pkg/app"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

// TestTypeConflictBetweenRGDs covers two RGDs in one workspace claiming the same composite
// type. The second must be refused rather than published over the first.
//
// This runs against a real kcp because the damage it prevents is in the published objects: an
// APIResourceSchema is immutable and named after a hash of its content, so identical schemas
// resolve to one object shared between both RGDs, and deleting the first collects it out from
// under the second.
func TestTypeConflictBetweenRGDs(t *testing.T) {
	ctx := t.Context()

	c := newConsumer(t, "typeconflict")
	eng := newEngine(t)

	const group = "typeconflict.example.com"
	resources := []any{cmResource("cm", "${schema.spec.name}-cm", map[string]any{"name": "${schema.spec.name}"})}

	require.NoError(t, c.Client.Create(ctx, compositeRGD("alpha", group, "Widget",
		map[string]any{"name": "string"}, resources)))
	converge(ctx, t, eng, c.ClusterName, "alpha")
	held := servedSchemaName(ctx, t, c, "alpha")

	t.Log("a second RGD claiming the same kind is refused")
	require.NoError(t, c.Client.Create(ctx, compositeRGD("beta", group, "Widget",
		map[string]any{"name": "string", "tier": "string"}, resources)))

	err := eng.ReconcileRGD(ctx, c.ClusterName, "beta")
	require.Error(t, err)
	require.True(t, app.IsTransient(err),
		"deleting the RGD holding the type resolves this without touching beta, so it must keep retrying")

	rgd := &krov1alpha1.ResourceGraphDefinition{}
	require.NoError(t, c.Client.Get(ctx, types.NamespacedName{Name: "beta"}, rgd))
	require.Equal(t, krov1alpha1.ResourceGraphDefinitionStateInactive, rgd.Status.State)
	kindReady := condition(t, rgd, krov1alpha1.RGDConditionTypeKindReady)
	require.Equal(t, metav1.ConditionFalse, kindReady.Status)
	require.NotNil(t, kindReady.Message)
	require.Contains(t, *kindReady.Message, "alpha", "the message must name the RGD holding the type")

	export := &kcpapisv1alpha2.APIExport{}
	require.True(t, apierrors.IsNotFound(
		c.Client.Get(ctx, types.NamespacedName{Name: "kro-beta"}, export)),
		"the refused RGD must not publish an APIExport")

	t.Log("the first RGD keeps serving its type untouched")
	require.Equal(t, held, servedSchemaName(ctx, t, c, "alpha"))
	require.NoError(t, c.Client.Get(ctx, types.NamespacedName{Name: held}, &kcpapisv1alpha1.APIResourceSchema{}))

	t.Log("releasing the type lets the second RGD publish it")
	require.NoError(t, c.Client.Delete(ctx, &krov1alpha1.ResourceGraphDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "alpha"},
	}))
	// Our finalizer holds the RGD until the published objects are torn down in order.
	wait.Until(t, func() bool {
		if err := eng.ReconcileRGD(ctx, c.ClusterName, "alpha"); err != nil {
			return false
		}
		return apierrors.IsNotFound(c.Client.Get(ctx,
			types.NamespacedName{Name: "alpha"}, &krov1alpha1.ResourceGraphDefinition{}))
	}, 90*time.Second, 2*time.Second, "alpha never finished tearing down")

	converge(ctx, t, eng, c.ClusterName, "beta")
	require.NotEqual(t, held, servedSchemaName(ctx, t, c, "beta"),
		"beta serves its own schema, not the one alpha published")

	require.NoError(t, c.Client.Get(ctx, types.NamespacedName{Name: "beta"}, rgd))
	require.Equal(t, krov1alpha1.ResourceGraphDefinitionStateActive, rgd.Status.State)
}

// TestSchemaOwnershipRestamped covers the adoption path: a schema whose ownership id no longer
// matches its RGD. Pruning and teardown key on that id, and an owner reference naming a uid
// that no longer exists is an orphan as far as garbage collection is concerned, so the
// reconcile has to restamp it. Against a real kcp because it also establishes that an
// immutable APIResourceSchema still accepts a metadata write.
func TestSchemaOwnershipRestamped(t *testing.T) {
	ctx := t.Context()

	c := newConsumer(t, "restamp")
	eng := newEngine(t)

	const group = "restamp.example.com"
	const rgdName = "widgets." + group
	require.NoError(t, c.Client.Create(ctx, compositeRGD(rgdName, group, "Widget",
		map[string]any{"name": "string"},
		[]any{cmResource("cm", "${schema.spec.name}-cm", map[string]any{"name": "${schema.spec.name}"})})))
	converge(ctx, t, eng, c.ClusterName, rgdName)

	served := servedSchemaName(ctx, t, c, rgdName)
	rgd := &krov1alpha1.ResourceGraphDefinition{}
	require.NoError(t, c.Client.Get(ctx, types.NamespacedName{Name: rgdName}, rgd))

	ars := &kcpapisv1alpha1.APIResourceSchema{}
	require.NoError(t, c.Client.Get(ctx, types.NamespacedName{Name: served}, ars))
	require.Equal(t, string(rgd.UID), ars.Labels[krometadata.ResourceGraphDefinitionIDLabel],
		"the schema must carry the RGD's id to begin with")

	t.Log("stand in for an RGD recreated under the same name by staling the id")
	ars.Labels[krometadata.ResourceGraphDefinitionIDLabel] = "stale-uid"
	require.NoError(t, c.Client.Update(ctx, ars))

	require.NoError(t, eng.ReconcileRGD(ctx, c.ClusterName, rgdName))

	require.NoError(t, c.Client.Get(ctx, types.NamespacedName{Name: served}, ars))
	require.Equal(t, string(rgd.UID), ars.Labels[krometadata.ResourceGraphDefinitionIDLabel],
		"the id must be restamped, or pruning and teardown stop recognising the schema")
	require.Equal(t, rgdName, ars.Labels[krometadata.ResourceGraphDefinitionNameLabel])
	require.Equal(t, served, servedSchemaName(ctx, t, c, rgdName),
		"restamping must not move the served schema")
}
