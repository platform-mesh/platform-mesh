package controller // coverage-ignore

import (
	"context"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	platformeshconfig "github.com/platform-mesh/golang-commons/config"
	lifecyclecontrollerruntime "github.com/platform-mesh/golang-commons/controller/lifecycle/controllerruntime"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/logger"
	corev1alpha1 "github.com/platform-mesh/security-operator/api/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/subroutine"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/kcp"
)

type AuthorizationModelReconciler struct {
	lifecycle *lifecyclecontrollerruntime.LifecycleManager
}

func NewAuthorizationModelReconciler(log *logger.Logger, clt client.Client, fga openfgav1.OpenFGAServiceClient, lcClientFunc subroutine.NewLogicalClusterClientFunc) *AuthorizationModelReconciler {
	return &AuthorizationModelReconciler{
		lifecycle: lifecyclecontrollerruntime.NewLifecycleManager(
			[]lifecyclesubroutine.Subroutine{
				subroutine.NewTupleSubroutine(fga, clt, lcClientFunc),
			},
			"authorizationmodel",
			"AuthorizationModelReconciler",
			clt,
			log,
		),
	}
}

func (r *AuthorizationModelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req, &corev1alpha1.AuthorizationModel{})
}

func (r *AuthorizationModelReconciler) SetupWithManager(mgr ctrl.Manager, cfg *platformeshconfig.CommonServiceConfig, log *logger.Logger) error { // coverage-ignore
	return r.lifecycle.
		WithConditionManagement().
		SetupWithManager(
			mgr,
			cfg.MaxConcurrentReconciles,
			"authorizationmodel",
			&corev1alpha1.AuthorizationModel{},
			cfg.DebugLabelValue,
			kcp.WithClusterInContext(r),
			log,
		)
}
