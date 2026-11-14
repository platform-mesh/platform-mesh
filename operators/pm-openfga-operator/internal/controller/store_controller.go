/*
Copyright The Platform Mesh Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"

	platformeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/controller/filter"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/pm-openfga-operator/internal/client"
	"go.platform-mesh.io/pm-openfga-operator/internal/metrics"
	"go.platform-mesh.io/pm-openfga-operator/internal/subroutine"
	"go.platform-mesh.io/pm-openfga-operator/pkg/config"
	"go.platform-mesh.io/subroutines/conditions"
	"go.platform-mesh.io/subroutines/lifecycle"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrhandler "sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	"sigs.k8s.io/multicluster-runtime/pkg/handler"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

// StoreReconciler reconciles the Stores of one API group.
type StoreReconciler struct {
	fga       openfgav1.OpenFGAServiceClient
	log       *logger.Logger
	lifecycle *lifecycle.Lifecycle
	ctx       context.Context
	kinds     Kinds
}

// NewStoreReconciler reconciles the Stores of kinds; lister lists the
// AuthorizationModels of both groups that extend a Store's model.
func NewStoreReconciler(ctx context.Context, log *logger.Logger, fga openfgav1.OpenFGAServiceClient, mcMgr mcmanager.Manager, cfg *config.Config, lister iclient.Lister, kinds Kinds) *StoreReconciler {
	subs := chain(mcMgr, kinds.BeforeStore, kinds.WrapStore,
		subroutine.NewStoreSubroutine(fga, mcMgr, lister),
		subroutine.NewAuthorizationModelSubroutine(fga, mcMgr, lister, func(cfg *rest.Config) discovery.DiscoveryInterface {
			return discovery.NewDiscoveryClientForConfigOrDie(cfg)
		}, log),
		subroutine.NewTupleSubroutine(fga, mcMgr),
	)
	lc := lifecycle.New(mcMgr, "StoreReconciler"+kinds.Suffix, kinds.NewStore, subs...).
		WithConditions(conditions.NewManager())

	return &StoreReconciler{
		fga:       fga,
		log:       log,
		lifecycle: lc,
		ctx:       ctx,
		kinds:     kinds,
	}
}

func (r *StoreReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	start := time.Now()
	result, err := r.lifecycle.Reconcile(ctx, req)
	labelResult := "success"
	if err != nil {
		labelResult = "error"
	}
	metrics.ReconcileTotal.WithLabelValues("store"+r.kinds.Suffix, labelResult).Inc()
	metrics.ReconcileDuration.WithLabelValues("store" + r.kinds.Suffix).Observe(time.Since(start).Seconds())
	return result, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *StoreReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformeshconfig.CommonServiceConfig, evp ...predicate.Predicate) error {
	predicates := append([]predicate.Predicate{filter.DebugResourcesBehaviourPredicate(cfg.DebugLabelValue)}, evp...)
	b := mcbuilder.ControllerManagedBy(mgr).
		Named("store"+r.kinds.Suffix).
		For(r.kinds.NewStore(), r.kinds.StoreEngageOptions()).
		WithOptions(controller.TypedOptions[mcreconcile.Request]{MaxConcurrentReconciles: cfg.MaxConcurrentReconciles}).
		WithEventFilter(predicate.And(predicates...))

	return b.
		Watches(
			r.kinds.NewAuthorizationModel(),
			func(_ multicluster.ClusterName, _ cluster.Cluster) ctrhandler.TypedEventHandler[ctrlruntimeclient.Object, mcreconcile.Request] {
				return handler.TypedEnqueueRequestsFromMapFuncWithClusterPreservation(func(ctx context.Context, obj ctrlruntimeclient.Object) []mcreconcile.Request {
					name, clusterID, ok := r.kinds.AuthorizationModelStoreRef(obj)
					if !ok {
						return nil
					}

					return []mcreconcile.Request{
						{
							Request: reconcile.Request{
								NamespacedName: types.NamespacedName{
									Name: name,
								},
							},
							ClusterName: config.MultiProviderName(r.kinds.StoreProvider, clusterID),
						},
					}
				})
			},
			mcbuilder.WithPredicates(predicate.GenerationChangedPredicate{}),
			r.kinds.AuthorizationModelEngageOptions(),
		).Complete(r)
}
