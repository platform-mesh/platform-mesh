package controller

import (
	"context"
	"fmt"

	platformmeshconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/filter"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/ratelimiter"
	"github.com/platform-mesh/golang-commons/logger"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"k8s.io/client-go/util/workqueue"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/pkg/subroutines/manageaccountinfo"
	"github.com/platform-mesh/account-operator/pkg/subroutines/workspace"
	"github.com/platform-mesh/account-operator/pkg/subroutines/workspaceready"
	"github.com/platform-mesh/account-operator/pkg/subroutines/workspacetype"
	"github.com/platform-mesh/subroutines"
	"github.com/platform-mesh/subroutines/conditions"
	"github.com/platform-mesh/subroutines/lifecycle"
)

const (
	operatorName          = "account-operator"
	accountReconcilerName = "AccountReconciler"
)

// AccountReconciler orchestrates Account resources across logical clusters.
type AccountReconciler struct {
	cfg         config.OperatorConfig
	lifecycle   *lifecycle.Lifecycle
	rateLimiter workqueue.TypedRateLimiter[mcreconcile.Request]
}

func NewAccountReconciler(log *logger.Logger, mgr mcmanager.Manager, cfg config.OperatorConfig) (*AccountReconciler, error) {
	localMgr := mgr.GetLocalManager()
	localCfg := rest.CopyConfig(localMgr.GetConfig())
	serverCA := string(localCfg.CAData)

	subs := []subroutines.Subroutine{}

	if cfg.Subroutines.WorkspaceType.Enabled {
		subs = append(subs, workspacetype.New(mgr))
	}

	if cfg.Subroutines.Workspace.Enabled {
		wsSub, err := workspace.New(mgr)
		if err != nil {
			return nil, fmt.Errorf("creating Workspace subroutine: %w", err)
		}
		subs = append(subs, wsSub)
	}

	if cfg.Subroutines.AccountInfo.Enabled {
		maiSub, err := manageaccountinfo.New(mgr, serverCA)
		if err != nil {
			return nil, fmt.Errorf("creating ManageAccountInfo subroutine: %w", err)
		}
		subs = append(subs, maiSub)
	}

	if cfg.Subroutines.WorkspaceReady.Enabled {
		wsReadySub, err := workspaceready.New(mgr)
		if err != nil {
			return nil, fmt.Errorf("creating WorkspaceReady subroutine: %w", err)
		}
		subs = append(subs, wsReadySub)
	}

	rl, err := ratelimiter.NewStaticThenExponentialRateLimiter[mcreconcile.Request](ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %w", err)
	}

	lc := lifecycle.New(mgr, accountReconcilerName, func() client.Object {
		return &v1alpha1.Account{}
	}, subs...).WithConditions(conditions.NewManager())

	return &AccountReconciler{
		cfg:         cfg,
		lifecycle:   lc,
		rateLimiter: rl,
	}, nil
}

func (r *AccountReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformmeshconfig.CommonServiceConfig, log *logger.Logger, eventPredicates ...predicate.Predicate) error {
	opts := controller.TypedOptions[mcreconcile.Request]{
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles,
		RateLimiter:             r.rateLimiter,
	}
	predicates := append([]predicate.Predicate{filter.DebugResourcesBehaviourPredicate(cfg.DebugLabelValue)}, eventPredicates...)
	return mcbuilder.ControllerManagedBy(mgr).
		Named(accountReconcilerName).
		For(&v1alpha1.Account{}).
		WithOptions(opts).
		WithEventFilter(predicate.And(predicates...)).
		Complete(r)
}

func (r *AccountReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req)
}
