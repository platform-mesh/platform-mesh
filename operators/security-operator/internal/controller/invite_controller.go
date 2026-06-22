package controller

import (
	"context"
	"fmt"
	"time"

	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	platformeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/controller/filter"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/metrics"
	"go.platform-mesh.io/security-operator/internal/subroutine/invite"
	"go.platform-mesh.io/subroutines/conditions"
	"go.platform-mesh.io/subroutines/lifecycle"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"k8s.io/client-go/util/workqueue"
)

type InviteReconciler struct {
	log         *logger.Logger
	lifecycle   *lifecycle.Lifecycle
	rateLimiter workqueue.TypedRateLimiter[mcreconcile.Request]
}

func NewInviteReconciler(ctx context.Context, mgr mcmanager.Manager, cfg *config.Config, log *logger.Logger, kcpClientGetter iclient.KCPClientGetter) (*InviteReconciler, error) {
	inviteSubroutine, err := invite.New(ctx, cfg, kcpClientGetter)
	if err != nil {
		return nil, fmt.Errorf("creating Invite subroutine: %w", err)
	}

	rl, err := ratelimiter.NewStaticThenExponentialRateLimiter[mcreconcile.Request](ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %w", err)
	}

	lc := lifecycle.New(mgr, "InviteReconciler", func() client.Object {
		return &corev1alpha1.Invite{}
	}, inviteSubroutine).
		WithConditions(conditions.NewManager())

	return &InviteReconciler{
		log:         log,
		lifecycle:   lc,
		rateLimiter: rl,
	}, nil
}

func (r *InviteReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	start := time.Now()
	result, err := r.lifecycle.Reconcile(ctx, req)
	labelResult := "success"
	if err != nil {
		labelResult = "error"
	}
	metrics.ReconcileTotal.WithLabelValues("invite", labelResult).Inc()
	metrics.ReconcileDuration.WithLabelValues("invite").Observe(time.Since(start).Seconds())
	return result, err
}

func (r *InviteReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformeshconfig.CommonServiceConfig, log *logger.Logger) error {
	opts := controller.TypedOptions[mcreconcile.Request]{
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles,
		RateLimiter:             r.rateLimiter,
	}
	predicates := []predicate.Predicate{filter.DebugResourcesBehaviourPredicate(cfg.DebugLabelValue)}
	return mcbuilder.ControllerManagedBy(mgr).
		Named("invite").
		For(&corev1alpha1.Invite{}).
		WithOptions(opts).
		WithEventFilter(predicate.And(predicates...)).
		Complete(r)
}
