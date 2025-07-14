package controller // coverage-ignore

import (
	"context"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	corev1alpha1 "github.com/openmfp/fga-operator/api/v1alpha1"
	"github.com/openmfp/fga-operator/internal/subroutine"
	openmfpconfig "github.com/openmfp/golang-commons/config"
	"github.com/openmfp/golang-commons/controller/lifecycle"
	"github.com/openmfp/golang-commons/logger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/kcp"
)

type AuthorizationModelReconciler struct {
	lifecycle *lifecycle.LifecycleManager
}

func NewAuthorizationModelReconciler(log *logger.Logger, clt client.Client, fga openfgav1.OpenFGAServiceClient, lcClientFunc subroutine.NewLogicalClusterClientFunc) *AuthorizationModelReconciler {
	return &AuthorizationModelReconciler{
		lifecycle: lifecycle.NewLifecycleManager(
			log,
			"authorizationmodel",
			"AuthorizationModelReconciler",
			clt,
			[]lifecycle.Subroutine{
				subroutine.NewTupleSubroutine(fga, clt, lcClientFunc),
			},
		),
	}
}

func (r *AuthorizationModelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req, &corev1alpha1.AuthorizationModel{})
}

func (r *AuthorizationModelReconciler) SetupWithManager(mgr ctrl.Manager, cfg *openmfpconfig.CommonServiceConfig, log *logger.Logger) error { // coverage-ignore
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
