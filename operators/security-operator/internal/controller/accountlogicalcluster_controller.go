package controller

import (
	"context"
	"fmt"
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	platformeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/controller/filter"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/fga"
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

	"k8s.io/client-go/util/workqueue"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

type AccountLogicalClusterController struct {
	log         *logger.Logger
	name        string
	lifecycle   *lifecycle.Lifecycle
	rateLimiter workqueue.TypedRateLimiter[mcreconcile.Request]
}

func NewAccountLogicalClusterController(log *logger.Logger, cfg config.Config, fgaClient openfgav1.OpenFGAServiceClient, storeIDGetter fga.StoreIDGetter, mgr mcmanager.Manager, kcpClientGetter iclient.KCPClientGetter, opts ControllerOptions) (*AccountLogicalClusterController, error) {
	rl, err := ratelimiter.NewStaticThenExponentialRateLimiter[mcreconcile.Request](ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %w", err)
	}

	lc := lifecycle.New(mgr, opts.Name, func() client.Object {
		return &kcpcorev1alpha1.LogicalCluster{}
	}, subroutine.NewAccountTuplesSubroutine(mgr, fgaClient, storeIDGetter, cfg.FGA.CreatorRelation, cfg.FGA.ParentRelation, cfg.FGA.ObjectType, kcpClientGetter))

	if opts.InitializerName != "" {
		lc = lc.WithInitializer(opts.InitializerName)
	}
	if opts.TerminatorName != "" {
		lc = lc.WithTerminator(opts.TerminatorName)
	}

	return &AccountLogicalClusterController{
		log:         log,
		name:        opts.Name,
		lifecycle:   lc,
		rateLimiter: rl,
	}, nil
}

func (r *AccountLogicalClusterController) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	start := time.Now()
	result, err := r.lifecycle.Reconcile(ctx, req)
	labelResult := "success"
	if err != nil {
		labelResult = "error"
	}
	metrics.ReconcileTotal.WithLabelValues(r.name, labelResult).Inc()
	metrics.ReconcileDuration.WithLabelValues(r.name).Observe(time.Since(start).Seconds())
	return result, err
}

func (r *AccountLogicalClusterController) SetupWithManager(mgr mcmanager.Manager, cfg *platformeshconfig.CommonServiceConfig, evp ...predicate.Predicate) error {
	opts := controller.TypedOptions[mcreconcile.Request]{
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles,
		RateLimiter:             r.rateLimiter,
	}
	predicates := append([]predicate.Predicate{filter.DebugResourcesBehaviourPredicate(cfg.DebugLabelValue)}, evp...)
	return mcbuilder.ControllerManagedBy(mgr).
		Named(r.name).
		For(&kcpcorev1alpha1.LogicalCluster{}).
		WithOptions(opts).
		WithEventFilter(predicate.And(predicates...)).
		Complete(r)
}
