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

	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	platformeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/controller/filter"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/metrics"
	"go.platform-mesh.io/security-operator/internal/subroutine"
	"go.platform-mesh.io/subroutines/lifecycle"

	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
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

	"github.com/kcp-dev/logicalcluster/v3"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

func NewAPIBindingReconciler(logger *logger.Logger, mcMgr mcmanager.Manager, lister iclient.Lister, cfg *config.Config) *APIBindingReconciler {
	lc := lifecycle.New(mcMgr, "APIBindingReconciler", func() ctrlruntimeclient.Object {
		return &kcpapisv1alpha2.APIBinding{}
	}, subroutine.NewAuthorizationModelGenerationSubroutine(mcMgr, lister))

	return &APIBindingReconciler{
		log:       logger,
		lifecycle: lc,
		lister:    lister,
	}
}

type APIBindingReconciler struct {
	log       *logger.Logger
	lifecycle *lifecycle.Lifecycle
	lister    iclient.Lister
}

func (r *APIBindingReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	start := time.Now()
	result, err := r.lifecycle.Reconcile(ctx, req)
	labelResult := "success"
	if err != nil {
		labelResult = "error"
	}
	metrics.ReconcileTotal.WithLabelValues("apibinding", labelResult).Inc()
	metrics.ReconcileDuration.WithLabelValues("apibinding").Observe(time.Since(start).Seconds())
	return result, err
}

func (r *APIBindingReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformeshconfig.CommonServiceConfig, evp ...predicate.Predicate) error {
	opts := controller.TypedOptions[mcreconcile.Request]{
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles,
	}
	predicates := append([]predicate.Predicate{filter.DebugResourcesBehaviourPredicate(cfg.DebugLabelValue)}, evp...)
	return mcbuilder.ControllerManagedBy(mgr).
		Named("apibinding").
		For(&kcpapisv1alpha2.APIBinding{}).
		WithOptions(opts).
		WithEventFilter(predicate.And(predicates...)).
		Watches(
			&corev1alpha1.ProviderPermissions{},
			func(_ multicluster.ClusterName, _ cluster.Cluster) ctrhandler.TypedEventHandler[client.Object, mcreconcile.Request] {
				return handler.TypedEnqueueRequestsFromMapFuncWithClusterPreservation(r.mapProviderPermissionsToAPIBindings)
			},
			mcbuilder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Complete(r)
}

func (r *APIBindingReconciler) mapProviderPermissionsToAPIBindings(ctx context.Context, obj client.Object) []mcreconcile.Request {
	pp, ok := obj.(*corev1alpha1.ProviderPermissions)
	if !ok {
		return nil
	}

	var bindings kcpapisv1alpha2.APIBindingList
	if err := r.lister.List(ctx, &bindings); err != nil {
		r.log.Error().Err(err).Msg("failed to list APIBindings for ProviderPermissions")
		return nil
	}

	var requests []mcreconcile.Request
	for _, binding := range bindings.Items {
		if binding.Spec.Reference.Export == nil ||
			binding.Spec.Reference.Export.Name != pp.Spec.APIExportRef.Name {
			continue
		}

		requests = append(requests, mcreconcile.Request{
			Request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: binding.Name,
				},
			},
			ClusterName: multicluster.ClusterName(logicalcluster.From(&binding).String()),
		})
	}

	r.log.Debug().
		Str("providerPermissions", pp.Name).
		Int("affectedBindings", len(requests)).
		Msg("ProviderPermissions change enqueuing APIBinding reconciliations")

	return requests
}
