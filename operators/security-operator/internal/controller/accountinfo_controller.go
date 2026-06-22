package controller

import (
	"context"
	"time"

	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	platformeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/controller/filter"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/security-operator/internal/metrics"
	"go.platform-mesh.io/security-operator/internal/subroutine"
	"go.platform-mesh.io/subroutines/lifecycle"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

type AccountInfoReconciler struct {
	log       *logger.Logger
	lifecycle *lifecycle.Lifecycle
}

func NewAccountInfoReconciler(log *logger.Logger, mcMgr mcmanager.Manager) *AccountInfoReconciler {
	lc := lifecycle.New(mcMgr, "AccountInfoReconciler", func() client.Object {
		return &corev1alpha1.AccountInfo{}
	}, subroutine.NewAccountInfoFinalizerSubroutine(mcMgr))

	return &AccountInfoReconciler{
		log:       log,
		lifecycle: lc,
	}
}

func (r *AccountInfoReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	start := time.Now()
	result, err := r.lifecycle.Reconcile(ctx, req)
	labelResult := "success"
	if err != nil {
		labelResult = "error"
	}
	metrics.ReconcileTotal.WithLabelValues("accountinfo", labelResult).Inc()
	metrics.ReconcileDuration.WithLabelValues("accountinfo").Observe(time.Since(start).Seconds())
	return result, err
}

func (r *AccountInfoReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformeshconfig.CommonServiceConfig, evp ...predicate.Predicate) error {
	opts := controller.TypedOptions[mcreconcile.Request]{
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles,
	}
	predicates := append([]predicate.Predicate{filter.DebugResourcesBehaviourPredicate(cfg.DebugLabelValue)}, evp...)
	return mcbuilder.ControllerManagedBy(mgr).
		Named("accountinfo").
		For(&corev1alpha1.AccountInfo{}).
		WithOptions(opts).
		WithEventFilter(predicate.And(predicates...)).
		Complete(r)
}
