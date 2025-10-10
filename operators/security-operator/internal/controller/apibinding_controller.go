package controller

import (
	"context"

	kcpv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	platformeshconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/builder"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/logger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	lifecyclecontrollerruntime "github.com/platform-mesh/golang-commons/controller/lifecycle/multicluster"
	"github.com/platform-mesh/security-operator/internal/subroutine"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

func NewAPIBindingReconciler(logger *logger.Logger, mcMgr mcmanager.Manager) *APIBindingReconciler {
	return &APIBindingReconciler{
		log: logger,
		lifecycle: builder.NewBuilder("apibinding", "apibinding-controller", []lifecyclesubroutine.Subroutine{
			subroutine.NewAuthorizationModelGenerationSubroutine(mcMgr),
		}, logger).
			BuildMultiCluster(mcMgr),
	}
}

type APIBindingReconciler struct {
	log       *logger.Logger
	lifecycle *lifecyclecontrollerruntime.LifecycleManager
}

func (r *APIBindingReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	ctxWithCluster := mccontext.WithCluster(ctx, req.ClusterName)
	return r.lifecycle.Reconcile(ctxWithCluster, req, &kcpv1alpha1.APIBinding{})
}

func (r *APIBindingReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformeshconfig.CommonServiceConfig, evp ...predicate.Predicate) error {
	return r.lifecycle.SetupWithManager(mgr, cfg.MaxConcurrentReconciles, "apibinding-controller", &kcpv1alpha1.APIBinding{}, cfg.DebugLabelValue, r, r.log, evp...)
}
