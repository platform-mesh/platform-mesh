package workspaceready

import (
	"context"
	"fmt"

	kcpcorev1alpha "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/ratelimiter"
	"github.com/platform-mesh/subroutines"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/clusteredname"
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
	limiter workqueue.TypedRateLimiter[*v1alpha1.Account]
}

// New returns a new WorkspaceReadySubroutine.
func New(mgr mcmanager.Manager) (*WorkspaceReadySubroutine, error) {
	limiter, err := ratelimiter.NewStaticThenExponentialRateLimiter[*v1alpha1.Account](
		ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %w", err)
	}
	return &WorkspaceReadySubroutine{mgr: mgr, limiter: limiter}, nil
}

func (r *WorkspaceReadySubroutine) GetName() string {
	return WorkspaceReadySubroutineName
}

func (r *WorkspaceReadySubroutine) Process(ctx context.Context, obj client.Object) (subroutines.Result, error) {
	instance := obj.(*v1alpha1.Account)
	cn := clusteredname.MustGetClusteredName(ctx, obj)

	clusterRef, err := r.mgr.GetCluster(ctx, cn.ClusterID.String())
	if err != nil {
		return subroutines.OK(), fmt.Errorf("getting cluster: %w", err)
	}
	clusterClient := clusterRef.GetClient()

	ws := &kcptenancyv1alpha.Workspace{}
	if err := clusterClient.Get(ctx, client.ObjectKey{Name: instance.Name}, ws); err != nil {
		return subroutines.OK(), fmt.Errorf("getting Account's Workspace: %w", err)
	}

	if ws.Status.Phase != kcpcorev1alpha.LogicalClusterPhaseReady {
		return subroutines.StopWithRequeue(r.limiter.When(instance), "Workspace not ready yet"), nil
	}

	r.limiter.Forget(instance)
	return subroutines.OK(), nil
}
