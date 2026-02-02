package controller

import (
	"context"

	platformmeshconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/builder"
	mclifecycle "github.com/platform-mesh/golang-commons/controller/lifecycle/multicluster"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/logger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/pkg/subroutines/finalizeaccountinfo"
)

const accountInfoReconcilerName = "AccountInfoReconciler"

// AccountInfoReconciler orchestrates AccountInfo resources across logical clusters.
type AccountInfoReconciler struct {
	cfg       config.OperatorConfig
	lifecycle *mclifecycle.LifecycleManager
}

func NewAccountInfoReconciler(log *logger.Logger, mgr mcmanager.Manager, cfg config.OperatorConfig) *AccountInfoReconciler { // coverage-ignore
	subs := []lifecyclesubroutine.Subroutine{}

	if cfg.Controllers.AccountInfo.Enabled {
		subs = append(subs, finalizeaccountinfo.New(mgr))
	}

	return &AccountInfoReconciler{
		cfg: cfg,
		lifecycle: builder.NewBuilder(operatorName, accountInfoReconcilerName, subs, log).
			BuildMultiCluster(mgr),
	}
}

func (r *AccountInfoReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformmeshconfig.CommonServiceConfig, log *logger.Logger, eventPredicates ...predicate.Predicate) error { // coverage-ignore
	return r.lifecycle.SetupWithManager(mgr, cfg.MaxConcurrentReconciles, accountInfoReconcilerName, &v1alpha1.AccountInfo{}, cfg.DebugLabelValue, r, log, eventPredicates...)
}

func (r *AccountInfoReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) { // coverage-ignore
	return r.lifecycle.Reconcile(mccontext.WithCluster(ctx, req.ClusterName), req, &v1alpha1.AccountInfo{})
}
