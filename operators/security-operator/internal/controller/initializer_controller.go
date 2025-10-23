package controller

import (
	"context"

	kcpcorev1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	platformeshconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/builder"
	lifecyclecontrollerruntime "github.com/platform-mesh/golang-commons/controller/lifecycle/multicluster"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/logger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/platform-mesh/security-operator/internal/config"
	"github.com/platform-mesh/security-operator/internal/subroutine"
)

type LogicalClusterReconciler struct {
	log *logger.Logger

	lifecycle *lifecyclecontrollerruntime.LifecycleManager
}

func NewLogicalClusterReconciler(log *logger.Logger, orgClient client.Client, cfg config.Config, inClusterClient client.Client, mgr mcmanager.Manager) *LogicalClusterReconciler {
	return &LogicalClusterReconciler{
		log: log,
		lifecycle: builder.NewBuilder("logicalcluster", "LogicalClusterReconciler", []lifecyclesubroutine.Subroutine{
			subroutine.NewWorkspaceInitializer(orgClient, cfg, mgr),
			subroutine.NewWorkspaceAuthConfigurationSubroutine(orgClient, inClusterClient, cfg),
			subroutine.NewRealmSubroutine(inClusterClient, &cfg, cfg.BaseDomain),
			subroutine.NewInviteSubroutine(orgClient, mgr),
			subroutine.NewRemoveInitializer(mgr, cfg.InitializerName),
		}, log).
			WithReadOnly().
			BuildMultiCluster(mgr),
	}
}

func (r *LogicalClusterReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	ctxWithCluster := mccontext.WithCluster(ctx, req.ClusterName)
	return r.lifecycle.Reconcile(ctxWithCluster, req, &kcpcorev1alpha1.LogicalCluster{})
}

func (r *LogicalClusterReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformeshconfig.CommonServiceConfig, evp ...predicate.Predicate) error {
	return r.lifecycle.SetupWithManager(mgr, cfg.MaxConcurrentReconciles, "LogicalCluster", &kcpcorev1alpha1.LogicalCluster{}, cfg.DebugLabelValue, r, r.log, evp...)
}
