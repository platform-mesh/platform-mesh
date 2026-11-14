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
	"strings"
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	platformeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/controller/filter"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/pm-openfga-operator/internal/client"
	"go.platform-mesh.io/pm-openfga-operator/internal/fga"
	"go.platform-mesh.io/pm-openfga-operator/internal/metrics"
	"go.platform-mesh.io/pm-openfga-operator/internal/subroutine"
	"go.platform-mesh.io/pm-openfga-operator/pkg/config"
	"go.platform-mesh.io/subroutines/conditions"
	"go.platform-mesh.io/subroutines/lifecycle"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

	"github.com/kcp-dev/logicalcluster/v3"
)

// APIExportPolicyReconciler reconciles the APIExportPolicies of one API group.
type APIExportPolicyReconciler struct {
	log       *logger.Logger
	lifecycle *lifecycle.Lifecycle
	kinds     Kinds
}

func NewAPIExportPolicyReconciler(log *logger.Logger, fgaClient openfgav1.OpenFGAServiceClient, mcMgr mcmanager.Manager, lister iclient.Lister, cfg *config.Config, storeIDGetter fga.StoreIDGetter, kcpClientGetter iclient.KCPClientGetter, kinds Kinds) *APIExportPolicyReconciler {
	subs := chain(mcMgr, kinds.BeforeAPIExportPolicy, kinds.WrapAPIExportPolicy,
		subroutine.NewAPIExportPolicySubroutine(fgaClient, cfg, storeIDGetter, lister, kcpClientGetter))
	lc := lifecycle.New(mcMgr, "APIExportPolicyReconciler"+kinds.Suffix, kinds.NewAPIExportPolicy, subs...).
		WithConditions(conditions.NewManager())

	return &APIExportPolicyReconciler{
		log:       log,
		lifecycle: lc,
		kinds:     kinds,
	}
}

func (r *APIExportPolicyReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	start := time.Now()
	result, err := r.lifecycle.Reconcile(ctx, req)
	labelResult := "success"
	if err != nil {
		labelResult = "error"
	}
	metrics.ReconcileTotal.WithLabelValues("apiexportpolicy"+r.kinds.Suffix, labelResult).Inc()
	metrics.ReconcileDuration.WithLabelValues("apiexportpolicy" + r.kinds.Suffix).Observe(time.Since(start).Seconds())
	return result, err
}

func (r *APIExportPolicyReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformeshconfig.CommonServiceConfig, evp ...predicate.Predicate) error {
	opts := controller.TypedOptions[mcreconcile.Request]{
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles,
	}
	predicates := append([]predicate.Predicate{filter.DebugResourcesBehaviourPredicate(cfg.DebugLabelValue)}, evp...)

	return mcbuilder.ControllerManagedBy(mgr).
		Named("apiexportpolicy"+r.kinds.Suffix).
		For(r.kinds.NewAPIExportPolicy(), r.kinds.APIExportPolicyEngageOptions()).
		WithOptions(opts).
		WithEventFilter(predicate.And(predicates...)).
		Watches(
			&pmcorev1alpha1.Account{},
			func(_ multicluster.ClusterName, _ cluster.Cluster) ctrhandler.TypedEventHandler[ctrlruntimeclient.Object, mcreconcile.Request] {
				return handler.TypedEnqueueRequestsFromMapFuncWithClusterPreservation(func(ctx context.Context, obj ctrlruntimeclient.Object) []mcreconcile.Request {
					acc, ok := obj.(*pmcorev1alpha1.Account)
					if !ok {
						return nil
					}

					// we need to enqueue only when a new org is ready
					if acc.Spec.Type != pmcorev1alpha1.AccountTypeOrg || !meta.IsStatusConditionTrue(acc.GetConditions(), "Ready") {
						return nil
					}

					// List all APIExportPolicy resources and enqueue those with root:orgs:* expression
					return r.enqueueAllAPIExportPolicies(ctx, mgr)
				})
			},
			mcbuilder.WithClusterFilter(func(clusterName multicluster.ClusterName, _ cluster.Cluster) bool {
				return strings.HasPrefix(string(clusterName), config.CoreProviderName)
			}),
		).Complete(r)
}

func (r *APIExportPolicyReconciler) enqueueAllAPIExportPolicies(ctx context.Context, mgr mcmanager.Manager) []mcreconcile.Request {
	policies := r.kinds.NewAPIExportPolicyList()

	cluster, err := mgr.GetCluster(ctx, config.MultiProviderName(r.kinds.APIExportPolicyProvider, config.OrgsClusterPath))
	if err != nil {
		r.log.Error().Err(err).Msg("failed to get root:orgs cluster")
		return nil
	}

	err = cluster.GetClient().List(ctx, policies)
	if err != nil {
		r.log.Error().Err(err).Msg("failed to list all APIExportPolicies")
		return nil
	}

	var requests []mcreconcile.Request
	err = meta.EachListItem(policies, func(obj runtime.Object) error {
		policy, ok := obj.(ctrlruntimeclient.Object)
		if !ok {
			return nil
		}
		// Check if policy has root:orgs:* expression
		for _, expr := range r.kinds.APIExportPolicyAllowPathExpressions(policy) {
			trimmedExpr := strings.TrimPrefix(expr, ":")

			if trimmedExpr == "root:orgs:*" {
				clusterName := config.MultiProviderName(r.kinds.APIExportPolicyProvider, logicalcluster.From(policy).String())
				requests = append(requests, mcreconcile.Request{
					Request: reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name: policy.GetName(),
						},
					},
					ClusterName: clusterName,
				})
				break
			}
		}
		return nil
	})
	if err != nil {
		r.log.Error().Err(err).Msg("failed to iterate APIExportPolicies")
		return nil
	}
	return requests
}
