package workspaceready

import (
	"context"
	"fmt"

	kcpcorev1alpha "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/ratelimiter"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/errors"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/clusteredname"
)

var _ subroutine.Subroutine = (*WorkspaceReadySubroutine)(nil)

const (
	WorkspaceReadySubroutineName = "WorkspaceReadySubroutine"
)

// WorkspaceReadySubroutine checks that the Account's Workspace is ready. This
// currently cannot be done the Workspace subroutine because it would block
// subsequent AccountInfo creation and the security-operator's initializer
// expects the AccountInfo to exist to release the Workspace(and thus it getting
// ready).
type WorkspaceReadySubroutine struct {
	mgr     mcmanager.Manager
	limiter workqueue.TypedRateLimiter[*v1alpha1.Account]
}

// New returns a new WorkspaceReadySubroutine.
func New(mgr mcmanager.Manager) *WorkspaceReadySubroutine {
	limiter, _ := ratelimiter.NewStaticThenExponentialRateLimiter[*v1alpha1.Account](ratelimiter.NewConfig()) //nolint:errcheck

	return &WorkspaceReadySubroutine{mgr: mgr, limiter: limiter}
}

func (r *WorkspaceReadySubroutine) GetName() string {
	return WorkspaceReadySubroutineName
}

func (r *WorkspaceReadySubroutine) Finalizers(_ runtimeobject.RuntimeObject) []string {
	return []string{}
}

func (r *WorkspaceReadySubroutine) Finalize(_ context.Context, _ runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	return ctrl.Result{}, nil
}

func (r *WorkspaceReadySubroutine) Process(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := ro.(*v1alpha1.Account)
	cn := clusteredname.MustGetClusteredName(ctx, ro)

	clusterRef, err := r.mgr.GetCluster(ctx, cn.ClusterID.String())
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("getting cluster: %w", err), true, true)
	}
	clusterClient := clusterRef.GetClient()

	ws := &kcptenancyv1alpha.Workspace{}
	if err := clusterClient.Get(ctx, client.ObjectKey{Name: instance.Name}, ws); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("getting Account's Workspace: %w", err), true, true)
	}

	if ws.Status.Phase != kcpcorev1alpha.LogicalClusterPhaseReady {
		return ctrl.Result{RequeueAfter: r.limiter.When(instance)}, nil
	}

	r.limiter.Forget(instance)
	return ctrl.Result{}, nil
}
