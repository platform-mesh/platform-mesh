package controller

import (
	"context"

	kcpv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	platformeshconfig "github.com/platform-mesh/golang-commons/config"
	lifecyclecontrollerruntime "github.com/platform-mesh/golang-commons/controller/lifecycle/controllerruntime"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/logger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/kcp"

	"github.com/platform-mesh/security-operator/internal/subroutine"
)

func NewAPIBindingReconciler(cl client.Client, logger *logger.Logger, lcClientFunc subroutine.NewLogicalClusterClientFunc) *APIBindingReconciler {
	return &APIBindingReconciler{
		lifecycle: lifecyclecontrollerruntime.NewLifecycleManager(
			[]lifecyclesubroutine.Subroutine{
				subroutine.NewAuthorizationModelGenerationSubroutine(cl, lcClientFunc),
			},
			"apibinding",
			"apibinding",
			cl,
			logger,
		),
	}
}

type APIBindingReconciler struct {
	lifecycle *lifecyclecontrollerruntime.LifecycleManager
}

func (r *APIBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req, &kcpv1alpha1.APIBinding{})
}

func (r *APIBindingReconciler) SetupWithManager(mgr ctrl.Manager, logger *logger.Logger, cfg *platformeshconfig.CommonServiceConfig) error {
	return r.lifecycle.SetupWithManager(mgr, cfg.MaxConcurrentReconciles, "apibinding-controller", &kcpv1alpha1.APIBinding{}, cfg.DebugLabelValue, kcp.WithClusterInContext(r), logger)
}
