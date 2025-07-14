package controller

import (
	"context"

	kcpcorev1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	openmfpconfig "github.com/openmfp/golang-commons/config"
	"github.com/openmfp/golang-commons/controller/lifecycle"
	"github.com/openmfp/golang-commons/logger"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/kcp"

	"github.com/openmfp/fga-operator/internal/config"
	"github.com/openmfp/fga-operator/internal/subroutine"
)

type LogicalClusterReconciler struct {
	lifecycle *lifecycle.LifecycleManager
}

func NewLogicalClusterReconciler(log *logger.Logger, restCfg *rest.Config, cl, orgClient client.Client, cfg config.Config) *LogicalClusterReconciler {
	return &LogicalClusterReconciler{
		lifecycle: lifecycle.NewLifecycleManager(
			log,
			"logicalcluster",
			"LogicalClusterReconciler",
			cl,
			[]lifecycle.Subroutine{
				subroutine.NewWorkspaceInitializer(cl, orgClient, restCfg, cfg),
			},
		),
	}
}

func (r *LogicalClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req, &kcpcorev1alpha1.LogicalCluster{})
}

func (r *LogicalClusterReconciler) SetupWithManager(mgr ctrl.Manager, cfg *openmfpconfig.CommonServiceConfig, log *logger.Logger) error {
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
