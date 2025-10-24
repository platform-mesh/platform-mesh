package controller // coverage-ignore

import (
	"context"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	platformeshconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/builder"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/multicluster"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/logger"
	corev1alpha1 "github.com/platform-mesh/security-operator/api/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/subroutine"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

type AuthorizationModelReconciler struct {
	log         *logger.Logger
	mclifecycle *multicluster.LifecycleManager
}

func NewAuthorizationModelReconciler(log *logger.Logger, fga openfgav1.OpenFGAServiceClient, mcMgr mcmanager.Manager) *AuthorizationModelReconciler {
	return &AuthorizationModelReconciler{
		log: log,
		mclifecycle: builder.NewBuilder("authorizationmodel", "AuthorizationModelReconciler", []lifecyclesubroutine.Subroutine{
			subroutine.NewTupleSubroutine(fga, mcMgr),
		}, log).
			BuildMultiCluster(mcMgr),
	}
}

func (r *AuthorizationModelReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	ctxWithCluster := mccontext.WithCluster(ctx, req.ClusterName)
	return r.mclifecycle.Reconcile(ctxWithCluster, req, &corev1alpha1.AuthorizationModel{})
}

func (r *AuthorizationModelReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformeshconfig.CommonServiceConfig, evp ...predicate.Predicate) error { // coverage-ignore
	return r.mclifecycle.SetupWithManager(mgr, cfg.MaxConcurrentReconciles, "authorizationmodel", &corev1alpha1.AuthorizationModel{}, cfg.DebugLabelValue, r, r.log, evp...)
}
