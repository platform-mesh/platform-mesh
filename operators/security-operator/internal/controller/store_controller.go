package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/kcp"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kcpcorev1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	"github.com/kcp-dev/logicalcluster/v3"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	platformeshconfig "github.com/platform-mesh/golang-commons/config"
	lifecyclecontrollerruntime "github.com/platform-mesh/golang-commons/controller/lifecycle/controllerruntime"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/logger"
	corev1alpha1 "github.com/platform-mesh/security-operator/api/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/subroutine"
)

// StoreReconciler reconciles a Store object
type StoreReconciler struct {
	lifecycle    *lifecyclecontrollerruntime.LifecycleManager
	lcClientFunc subroutine.NewLogicalClusterClientFunc
}

func NewStoreReconciler(log *logger.Logger, clt client.Client, fga openfgav1.OpenFGAServiceClient, lcClientFunc subroutine.NewLogicalClusterClientFunc) *StoreReconciler {
	return &StoreReconciler{
		lcClientFunc: lcClientFunc,
		lifecycle: lifecyclecontrollerruntime.NewLifecycleManager(
			[]lifecyclesubroutine.Subroutine{
				subroutine.NewStoreSubroutine(fga, clt, lcClientFunc),
				subroutine.NewAuthorizationModelSubroutine(fga, clt, lcClientFunc),
				subroutine.NewTupleSubroutine(fga, clt, lcClientFunc),
			},
			"store",
			"StoreReconciler",
			clt,
			log,
		),
	}
}

func (r *StoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req, &corev1alpha1.Store{})
}

// SetupWithManager sets up the controller with the Manager.
func (r *StoreReconciler) SetupWithManager(mgr ctrl.Manager, cfg *platformeshconfig.CommonServiceConfig, log *logger.Logger) error { // coverage-ignore
	controllerBuilder, err := r.lifecycle.
		WithConditionManagement().
		SetupWithManagerBuilder(mgr, cfg.MaxConcurrentReconciles, "store", &corev1alpha1.Store{}, cfg.DebugLabelValue, log)
	if err != nil {
		return err
	}

	return controllerBuilder.
		Watches(
			&corev1alpha1.AuthorizationModel{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				model := obj.(*corev1alpha1.AuthorizationModel)

				lcClient, err := r.lcClientFunc(logicalcluster.Name(model.Spec.StoreRef.Path))
				if err != nil {
					log.Error().Err(err).Msg("failed to get logical cluster client")
					return nil
				}

				var lc kcpcorev1alpha1.LogicalCluster
				err = lcClient.Get(ctx, client.ObjectKey{Name: "cluster"}, &lc)
				if err != nil {
					log.Error().Err(err).Msg("failed to get logical cluster")
					return nil
				}

				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Name: model.Spec.StoreRef.Name,
						},
						ClusterName: lc.Annotations["kcp.io/cluster"],
					},
				}
			}),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Complete(kcp.WithClusterInContext(r))
}
