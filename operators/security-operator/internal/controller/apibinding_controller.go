package controller

import (
	"context"

	kcpv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	platformeshconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/builder"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/logger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/platform-mesh/golang-commons/controller/lifecycle/multicluster"
	"github.com/platform-mesh/security-operator/internal/subroutine"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

func NewAPIBindingReconciler(logger *logger.Logger, mcMgr mcmanager.Manager) *APIBindingReconciler {
	return &APIBindingReconciler{
		log: logger,
		mclifecycle: builder.NewBuilder("apibinding", "apibinding-controller", []lifecyclesubroutine.Subroutine{
			subroutine.NewAuthorizationModelGenerationSubroutine(mcMgr),
		}, logger).
			BuildMultiCluster(mcMgr),
	}
}

type APIBindingReconciler struct {
	log         *logger.Logger
	mclifecycle *multicluster.LifecycleManager
}

func (r *APIBindingReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	ctxWithCluster := mccontext.WithCluster(ctx, req.ClusterName)
	return r.mclifecycle.Reconcile(ctxWithCluster, req, &kcpv1alpha2.APIBinding{})
}

func (r *APIBindingReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformeshconfig.CommonServiceConfig, evp ...predicate.Predicate) error {
	return r.mclifecycle.SetupWithManager(mgr, cfg.MaxConcurrentReconciles, "apibinding-controller", &kcpv1alpha2.APIBinding{}, cfg.DebugLabelValue, r, r.log, evp...)
}
