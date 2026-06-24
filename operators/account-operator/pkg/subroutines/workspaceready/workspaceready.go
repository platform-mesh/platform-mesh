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

package workspaceready

import (
	"context"
	"fmt"
	"time"

	"go.platform-mesh.io/account-operator/internal/metrics"
	"go.platform-mesh.io/account-operator/pkg/clusteredname"
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	"go.platform-mesh.io/subroutines"

	"k8s.io/client-go/util/workqueue"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

var _ subroutines.Processor = (*WorkspaceReadySubroutine)(nil)

const (
	WorkspaceReadySubroutineName = "WorkspaceReadySubroutine"
)

// WorkspaceReadySubroutine checks that the Account's Workspace is ready. This
// currently cannot be done the Workspace subroutine because it would block
// subsequent AccountInfo creation and the security-operator's initializer
// expects the AccountInfo to exist to release the Workspace(and thus it
// getting ready).
type WorkspaceReadySubroutine struct {
	mgr     mcmanager.Manager
	limiter workqueue.TypedRateLimiter[*pmcorev1alpha1.Account]
}

// New returns a new WorkspaceReadySubroutine.
func New(mgr mcmanager.Manager) (*WorkspaceReadySubroutine, error) {
	limiter, err := ratelimiter.NewStaticThenExponentialRateLimiter[*pmcorev1alpha1.Account](
		ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %w", err)
	}
	return &WorkspaceReadySubroutine{mgr: mgr, limiter: limiter}, nil
}

func (r *WorkspaceReadySubroutine) GetName() string {
	return WorkspaceReadySubroutineName
}

func (r *WorkspaceReadySubroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	instance := obj.(*pmcorev1alpha1.Account)
	cn := clusteredname.MustGetClusteredName(ctx, obj)

	clusterRef, err := r.mgr.GetCluster(ctx, multicluster.ClusterName(cn.ClusterID.String()))
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting cluster: %w", err)
	}
	clusterClient := clusterRef.GetClient()

	ws := &kcptenancyv1alpha1.Workspace{}
	if err := clusterClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: instance.Name}, ws); err != nil {
		return subroutines.OK(), fmt.Errorf("getting Account's Workspace: %w", err)
	}

	if ws.Status.Phase != kcpcorev1alpha1.LogicalClusterPhaseReady {
		return subroutines.StopWithRequeue(r.limiter.When(instance), "Workspace not ready yet"), nil
	}

	r.limiter.Forget(instance)

	duration := time.Since(instance.CreationTimestamp.Time).Seconds()
	metrics.WorkspaceReadyDuration.WithLabelValues(string(instance.Spec.Type)).Observe(duration)

	return subroutines.OK(), nil
}
