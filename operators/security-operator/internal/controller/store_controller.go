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
	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	platformeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/controller/filter"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/metrics"
	"go.platform-mesh.io/security-operator/internal/subroutine"
	"go.platform-mesh.io/subroutines/conditions"
	"go.platform-mesh.io/subroutines/lifecycle"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

// StoreReconciler reconciles a Store object
type StoreReconciler struct {
	fga       openfgav1.OpenFGAServiceClient
	log       *logger.Logger
	lifecycle *lifecycle.Lifecycle
}

func NewStoreReconciler(ctx context.Context, log *logger.Logger, fga openfgav1.OpenFGAServiceClient, mcMgr mcmanager.Manager, cfg *config.Config, lister iclient.Lister) *StoreReconciler {
	lc := lifecycle.New(mcMgr, "StoreReconciler", func() client.Object {
		return &corev1alpha1.Store{}
	},
		subroutine.NewStoreSubroutine(fga, mcMgr, lister),
		subroutine.NewAuthorizationModelSubroutine(fga, mcMgr, lister, func(cfg *rest.Config) discovery.DiscoveryInterface {
			return discovery.NewDiscoveryClientForConfigOrDie(cfg)
		}, log),
		subroutine.NewTupleSubroutine(fga, mcMgr),
	).WithConditions(conditions.NewManager())

	return &StoreReconciler{
		fga:       fga,
		log:       log,
		lifecycle: lc,
	}
}

func (r *StoreReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	start := time.Now()
	result, err := r.lifecycle.Reconcile(ctx, req)
	labelResult := "success"
	if err != nil {
		labelResult = "error"
	}
	metrics.ReconcileTotal.WithLabelValues("store", labelResult).Inc()
	metrics.ReconcileDuration.WithLabelValues("store").Observe(time.Since(start).Seconds())
	return result, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *StoreReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformeshconfig.CommonServiceConfig, evp ...predicate.Predicate) error {
	predicates := append([]predicate.Predicate{filter.DebugResourcesBehaviourPredicate(cfg.DebugLabelValue)}, evp...)
	b := mcbuilder.ControllerManagedBy(mgr).
		Named("store").
		For(&corev1alpha1.Store{}).
		WithOptions(controller.TypedOptions[mcreconcile.Request]{MaxConcurrentReconciles: cfg.MaxConcurrentReconciles}).
		WithEventFilter(predicate.And(predicates...))

	return b.
		Watches(
			&corev1alpha1.AuthorizationModel{},
			func(_ multicluster.ClusterName, _ cluster.Cluster) ctrhandler.TypedEventHandler[client.Object, mcreconcile.Request] {
				return handler.TypedEnqueueRequestsFromMapFuncWithClusterPreservation(func(ctx context.Context, obj client.Object) []mcreconcile.Request {
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
							ClusterName: multicluster.ClusterName(model.Spec.StoreRef.Cluster),
						},
					}
				})
			},
			mcbuilder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).Complete(r)
}
