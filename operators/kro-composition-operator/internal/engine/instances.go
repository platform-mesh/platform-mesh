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

package engine

import (
	"time"

	krov1alpha1 "github.com/kubernetes-sigs/kro/api/v1alpha1"
	"github.com/kubernetes-sigs/kro/pkg/controller/instance"
	"github.com/kubernetes-sigs/kro/pkg/dynamiccontroller"
	"github.com/kubernetes-sigs/kro/pkg/graph"
	"github.com/kubernetes-sigs/kro/pkg/graph/revisions"
	krometadata "github.com/kubernetes-sigs/kro/pkg/metadata"

	"go.platform-mesh.io/kro-composition-operator/internal/composition"
	"go.platform-mesh.io/kro-composition-operator/internal/workspace"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// deletionGracePeriod is how long kro waits on a child delete before calling it failed.
const deletionGracePeriod = 30 * time.Second

// kroInstanceFinalizer is kro's instance finalizer, named here because the
// workspace-terminating path strips it directly.
const kroInstanceFinalizer = "kro.run/finalizer"

// activeRevision is fixed: each RGD gets one registry entry, replaced on every recompile.
const activeRevision = 1

// instanceHandler returns kro's instance reconciler for one type in one workspace. kro takes
// every dependency as a parameter, so one controller per workspace and GVR gives
// multi-workspace behaviour unmodified: the fan-out lives here.
func (e *Engine) instanceHandler(
	clusterName string,
	wc *workspace.Clients,
	rgd *krov1alpha1.ResourceGraphDefinition,
	g *graph.Graph,
	registry *revisions.Registry,
	dc *dynamiccontroller.DynamicController,
) dynamiccontroller.Handler {
	gvr := instanceGVR(g.CRD)

	// kro resolves graphs through the registry, so hold one active revision per RGD. kro does
	// not read SpecHash, so it carries the published schema's hash to tie the two together.
	registry.Put(revisions.Entry{
		OwnerKey:      rgd.Name,
		Revision:      activeRevision,
		SpecHash:      schemaHash(g.CRD),
		State:         revisions.RevisionStateActive,
		CompiledGraph: g,
	})

	c := instance.NewController(
		logf.Log.WithName("instance").WithValues(
			"cluster", clusterName,
			"controllerKind", g.CRD.Spec.Names.Kind,
		),
		instance.ReconcileConfig{
			DefaultRequeueDuration:    instanceRequeueInterval,
			DeletionGraceTimeDuration: deletionGracePeriod,
			// kro accepts but never reads this. Matches what kro's own controller passes.
			DeletionPolicy: "Delete",
			RGDConfig:      composition.RGDConfig(),
		},
		gvr,
		registry.ResolverFor(rgd.Name),
		g.Instance.Meta.Namespaced,
		wc.KroSet,
		krometadata.NewResourceGraphDefinitionLabeler(rgd),
		krometadata.NewKROMetaLabeler(),
		dc.Coordinator(),
		wc.Recorder,
	)
	return c.Reconcile
}
