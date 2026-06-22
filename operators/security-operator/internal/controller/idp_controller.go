package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	platformeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/controller/filter"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/metrics"
	"go.platform-mesh.io/security-operator/internal/subroutine/idp"
	"go.platform-mesh.io/subroutines/conditions"
	"go.platform-mesh.io/subroutines/lifecycle"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"k8s.io/client-go/util/workqueue"
)

type IdentityProviderConfigurationReconciler struct {
	log         *logger.Logger
	lifecycle   *lifecycle.Lifecycle
	rateLimiter workqueue.TypedRateLimiter[mcreconcile.Request]
}

func NewIdentityProviderConfigurationReconciler(ctx context.Context, mgr mcmanager.Manager, kcpClientGetter iclient.KCPClientGetter, cfg *config.Config, log *logger.Logger) (*IdentityProviderConfigurationReconciler, error) {
	idpSubroutine, err := idp.New(ctx, cfg, mgr, kcpClientGetter)
	if err != nil {
		return nil, fmt.Errorf("creating IDP subroutine: %w", err)
	}

	rl, err := ratelimiter.NewStaticThenExponentialRateLimiter[mcreconcile.Request](ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %w", err)
	}

	lc := lifecycle.New(mgr, "IdentityProviderConfigurationReconciler", func() client.Object {
		return &corev1alpha1.IdentityProviderConfiguration{}
	}, idpSubroutine).
		WithConditions(conditions.NewManager())

	return &IdentityProviderConfigurationReconciler{
		log:         log,
		lifecycle:   lc,
		rateLimiter: rl,
	}, nil
}

func (r *IdentityProviderConfigurationReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	start := time.Now()
	result, err := r.lifecycle.Reconcile(ctx, req)
	labelResult := "success"
	if err != nil {
		labelResult = "error"
	}
	metrics.ReconcileTotal.WithLabelValues("identityprovider", labelResult).Inc()
	metrics.ReconcileDuration.WithLabelValues("identityprovider").Observe(time.Since(start).Seconds())
	return result, err
}

func (r *IdentityProviderConfigurationReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformeshconfig.CommonServiceConfig, log *logger.Logger, evp ...predicate.Predicate) error {
	opts := controller.TypedOptions[mcreconcile.Request]{
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles,
		RateLimiter:             r.rateLimiter,
	}
	predicates := append([]predicate.Predicate{filter.DebugResourcesBehaviourPredicate(cfg.DebugLabelValue)}, evp...)
	return mcbuilder.ControllerManagedBy(mgr).
		Named("identityprovider").
		For(&corev1alpha1.IdentityProviderConfiguration{}, mcbuilder.WithClusterFilter(func(clusterName multicluster.ClusterName, _ cluster.Cluster) bool {
			return strings.HasPrefix(string(clusterName), config.SystemProviderName)
		})).
		WithOptions(opts).
		WithEventFilter(predicate.And(predicates...)).
		Complete(r)
}
