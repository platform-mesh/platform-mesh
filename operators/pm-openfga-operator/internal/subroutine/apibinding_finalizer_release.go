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

package subroutine

import (
	"context"
	"fmt"
	"time"

	iclient "go.platform-mesh.io/pm-openfga-operator/internal/client"
	"go.platform-mesh.io/subroutines"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kcp-dev/logicalcluster/v3"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

// apiBindingDeletionRequeue is how long the terminator waits for kcp to mark
// the APIBindings of a terminating workspace for deletion.
const apiBindingDeletionRequeue = 5 * time.Second

// APIBindingFinalizerReleaseSubroutine removes the APIBinding finalizer of the
// model generator from the APIBindings of an account workspace that is being
// deleted.
//
// The generator sees APIBindings only through the core.platform-mesh.io
// APIExport virtual workspace, which serves only bindings that carry that
// export's claim label. kcp removes the label as soon as the workspace is
// deleted, so the generator never finalizes them and the workspace hangs.
// The terminator reaches the workspace directly instead. It leaves the
// AuthorizationModels alone: a later binding in the organization updates
// them, and the organization's Store deletes them with the organization.
type APIBindingFinalizerReleaseSubroutine struct {
	kcpClientGetter iclient.KCPClientGetter
}

func NewAPIBindingFinalizerReleaseSubroutine(kcpClientGetter iclient.KCPClientGetter) *APIBindingFinalizerReleaseSubroutine {
	return &APIBindingFinalizerReleaseSubroutine{kcpClientGetter: kcpClientGetter}
}

var _ subroutines.Terminator = &APIBindingFinalizerReleaseSubroutine{}

// GetName implements subroutines.Subroutine.
func (s *APIBindingFinalizerReleaseSubroutine) GetName() string {
	return "APIBindingFinalizerReleaseSubroutine"
}

// Terminate implements subroutines.Terminator.
func (s *APIBindingFinalizerReleaseSubroutine) Terminate(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	// The lifecycle puts controller-runtime's logger into the context, which
	// follows the operator's log flags.
	logger := log.FromContext(ctx)
	lc := obj.(*kcpcorev1alpha1.LogicalCluster)

	clusterName := logicalcluster.From(lc)
	if clusterName.Empty() {
		return subroutines.OK(), fmt.Errorf("cluster-annotation kcp.io/cluster on LogicalCluster is not set")
	}

	cl, err := s.kcpClientGetter.NewClientForLogicalCluster(ctx, clusterName.String())
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting client for workspace %s: %w", clusterName, err)
	}

	var bindings kcpapisv1alpha2.APIBindingList
	if err := cl.List(ctx, &bindings); err != nil {
		return subroutines.OK(), fmt.Errorf("listing APIBindings: %w", err)
	}

	unmarked := 0
	for i := range bindings.Items {
		binding := &bindings.Items[i]
		if !controllerutil.ContainsFinalizer(binding, APIBindingFinalizer) {
			continue
		}
		// kcp marks the APIBindings for deletion some seconds after the
		// workspace. Until then the generator still sees them and would add
		// the finalizer back; once they are marked, the API server rejects new
		// finalizers.
		if binding.GetDeletionTimestamp().IsZero() {
			unmarked++
			continue
		}

		// The optimistic lock keeps the patch from restoring finalizers that
		// kcp removed in the meantime.
		patch := ctrlruntimeclient.MergeFromWithOptions(binding.DeepCopy(), ctrlruntimeclient.MergeFromWithOptimisticLock{})
		controllerutil.RemoveFinalizer(binding, APIBindingFinalizer)
		if err := cl.Patch(ctx, binding, patch); err != nil {
			return subroutines.OK(), fmt.Errorf("removing finalizer %s from APIBinding %s: %w", APIBindingFinalizer, binding.Name, err)
		}
		logger.Info("removed the APIBinding finalizer of the terminating workspace", "apibinding", binding.Name)
	}

	if unmarked > 0 {
		return subroutines.StopWithRequeue(apiBindingDeletionRequeue, "waiting for kcp to mark the APIBindings for deletion"), nil
	}
	return subroutines.OK(), nil
}
