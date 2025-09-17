package controller

import (
	"context"

	kcpcorev1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	platformeshconfig "github.com/platform-mesh/golang-commons/config"
	lifecyclecontrollerruntime "github.com/platform-mesh/golang-commons/controller/lifecycle/controllerruntime"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/logger"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/kcp"

	"github.com/platform-mesh/security-operator/internal/config"
	"github.com/platform-mesh/security-operator/internal/subroutine"
)

type LogicalClusterReconciler struct {
	lifecycle *lifecyclecontrollerruntime.LifecycleManager
}

func NewLogicalClusterReconciler(log *logger.Logger, restCfg *rest.Config, cl, orgClient client.Client, cfg config.Config, inClusterClient client.Client) *LogicalClusterReconciler {
	return &LogicalClusterReconciler{
		lifecycle: lifecyclecontrollerruntime.NewLifecycleManager(
			[]lifecyclesubroutine.Subroutine{
				subroutine.NewWorkspaceInitializer(cl, orgClient, restCfg, cfg),
				subroutine.NewRealmSubroutine(inClusterClient, cfg.BaseDomain),
			},
			"logicalcluster",
			"LogicalClusterReconciler",
			cl,
			log,
		),
	}
}

func (r *LogicalClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req, &kcpcorev1alpha1.LogicalCluster{})
}

func (r *LogicalClusterReconciler) SetupWithManager(mgr ctrl.Manager, cfg *platformeshconfig.CommonServiceConfig, log *logger.Logger) error {
	return r.lifecycle.WithReadOnly().SetupWithManager(
		mgr,
		cfg.MaxConcurrentReconciles,
		"logicalcluster",
		&kcpcorev1alpha1.LogicalCluster{},
		cfg.DebugLabelValue,
		kcp.WithClusterInContext(r),
		log,
	)
}
