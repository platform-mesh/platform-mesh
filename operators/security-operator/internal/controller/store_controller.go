package controller

import (
	"context"

	"github.com/platform-mesh/golang-commons/controller/lifecycle/builder"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	platformeshconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/multicluster"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/logger"
	corev1alpha1 "github.com/platform-mesh/security-operator/api/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/subroutine"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/multicluster-runtime/pkg/handler"
)

// StoreReconciler reconciles a Store object
type StoreReconciler struct {
	fga         openfgav1.OpenFGAServiceClient
	log         *logger.Logger
	mclifecycle *multicluster.LifecycleManager
}

func NewStoreReconciler(log *logger.Logger, fga openfgav1.OpenFGAServiceClient, mcMgr mcmanager.Manager) *StoreReconciler {
	allClient, err := getAllClient(mcMgr)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to create new client")
	}

	return &StoreReconciler{
		fga: fga,
		log: log,
		mclifecycle: builder.NewBuilder("store", "StoreReconciler", []lifecyclesubroutine.Subroutine{
			subroutine.NewStoreSubroutine(fga, mcMgr),
			subroutine.NewAuthorizationModelSubroutine(fga, mcMgr, allClient, func(cfg *rest.Config) discovery.DiscoveryInterface {
				return discovery.NewDiscoveryClientForConfigOrDie(cfg)
			}, log),
			subroutine.NewTupleSubroutine(fga, mcMgr),
		}, log).WithConditionManagement().
			BuildMultiCluster(mcMgr),
	}
}

func (r *StoreReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	ctxWithCluster := mccontext.WithCluster(ctx, req.ClusterName)
	return r.mclifecycle.Reconcile(ctxWithCluster, req, &corev1alpha1.Store{})
}

// SetupWithManager sets up the controller with the Manager.
func (r *StoreReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformeshconfig.CommonServiceConfig, evp ...predicate.Predicate) error { // coverage-ignore
	builder, err := r.mclifecycle.SetupWithManagerBuilder(mgr, cfg.MaxConcurrentReconciles, "store", &corev1alpha1.Store{}, cfg.DebugLabelValue, r.log, evp...)
	if err != nil {
		return err
	}
	return builder.
		Watches(
			&corev1alpha1.AuthorizationModel{},
			handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []mcreconcile.Request {
				model, ok := obj.(*corev1alpha1.AuthorizationModel)
				if !ok {
					return nil
				}

				return []mcreconcile.Request{
					{
						Request: reconcile.Request{
							NamespacedName: types.NamespacedName{
								Name: model.Spec.StoreRef.Name,
							},
						},
						ClusterName: model.Spec.StoreRef.Path,
					},
				}
			}),
			mcbuilder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).Complete(r)
}
