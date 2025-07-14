package controller

import (
	"context"

	kcpv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	openmfpconfig "github.com/openmfp/golang-commons/config"
	"github.com/openmfp/golang-commons/controller/lifecycle"
	"github.com/openmfp/golang-commons/logger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/kcp"

	"github.com/openmfp/fga-operator/internal/subroutine"
)

func NewAPIBindingReconciler(cl client.Client, logger *logger.Logger, lcClientFunc subroutine.NewLogicalClusterClientFunc) *APIBindingReconciler {
	return &APIBindingReconciler{
		lifecycle: lifecycle.NewLifecycleManager(
			logger,
			"apibinding",
			"apibinding",
			cl,
			[]lifecycle.Subroutine{
				subroutine.NewAuthorizationModelGenerationSubroutine(cl, lcClientFunc),
			},
		),
	}
}

type APIBindingReconciler struct {
	lifecycle *lifecycle.LifecycleManager
}

func (r *APIBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req, &kcpv1alpha1.APIBinding{})
}

func (r *APIBindingReconciler) SetupWithManager(mgr ctrl.Manager, logger *logger.Logger, cfg *openmfpconfig.CommonServiceConfig) error {
	return r.lifecycle.SetupWithManager(mgr, cfg.MaxConcurrentReconciles, "apibinding-controller", &kcpv1alpha1.APIBinding{}, cfg.DebugLabelValue, kcp.WithClusterInContext(r), logger)
}
