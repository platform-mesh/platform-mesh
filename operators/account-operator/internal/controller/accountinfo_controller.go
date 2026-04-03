package controller

import (
	"context"
	"fmt"

	platformmeshconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/filter"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/ratelimiter"
	"github.com/platform-mesh/golang-commons/logger"
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
	"github.com/platform-mesh/account-operator/pkg/subroutines/finalizeaccountinfo"
	"github.com/platform-mesh/subroutines"
	"github.com/platform-mesh/subroutines/lifecycle"
)

const accountInfoReconcilerName = "AccountInfoReconciler"

// AccountInfoReconciler orchestrates AccountInfo resources across logical clusters.
type AccountInfoReconciler struct {
	cfg         config.OperatorConfig
	lifecycle   *lifecycle.Lifecycle
	rateLimiter workqueue.TypedRateLimiter[mcreconcile.Request]
}

func NewAccountInfoReconciler(log *logger.Logger, mgr mcmanager.Manager, cfg config.OperatorConfig) (*AccountInfoReconciler, error) {
	subs := []subroutines.Subroutine{}

	if cfg.Controllers.AccountInfo.Enabled {
		faSub, err := finalizeaccountinfo.New(mgr)
		if err != nil {
			return nil, fmt.Errorf("creating FinalizeAccountInfo subroutine: %w", err)
		}
		subs = append(subs, faSub)
	}

	rl, err := ratelimiter.NewStaticThenExponentialRateLimiter[mcreconcile.Request](ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %w", err)
	}

	lc := lifecycle.New(mgr, accountInfoReconcilerName, func() client.Object {
		return &v1alpha1.AccountInfo{}
	}, subs...)

	return &AccountInfoReconciler{
		cfg:         cfg,
		lifecycle:   lc,
		rateLimiter: rl,
	}, nil
}

func (r *AccountInfoReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformmeshconfig.CommonServiceConfig, log *logger.Logger, eventPredicates ...predicate.Predicate) error {
	opts := controller.TypedOptions[mcreconcile.Request]{
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles,
		RateLimiter:             r.rateLimiter,
	}
	predicates := append([]predicate.Predicate{filter.DebugResourcesBehaviourPredicate(cfg.DebugLabelValue)}, eventPredicates...)
	return mcbuilder.ControllerManagedBy(mgr).
		Named(accountInfoReconcilerName).
		For(&v1alpha1.AccountInfo{}).
		WithOptions(opts).
		WithEventFilter(predicate.And(predicates...)).
		Complete(r)
}

func (r *AccountInfoReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req)
}
