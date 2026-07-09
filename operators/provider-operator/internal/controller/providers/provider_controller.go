/*
Copyright 2026.

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

package providers

import (
	"context"
	"fmt"

	pmconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/controller/filter"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	"go.platform-mesh.io/provider-operator/internal/config"
	pmsubroutines "go.platform-mesh.io/provider-operator/pkg/subroutines"
	"go.platform-mesh.io/subroutines"
	"go.platform-mesh.io/subroutines/conditions"
	"go.platform-mesh.io/subroutines/lifecycle"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	providersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	pmsubs "go.platform-mesh.io/provider-operator/pkg/subroutines/providers"
)

const ProviderControllerName = "ProviderReconciler"

// ProviderReconciler reconciles Provider objects in kcp workspaces via the
// providers.platform-mesh.io virtual workspace. For each Provider it creates
// a workspace, ServiceAccount, RBAC, and a kubeconfig Secret inside the provider workspace.
type ProviderReconciler struct {
	lifecycle   *lifecycle.Lifecycle
	rateLimiter workqueue.TypedRateLimiter[mcreconcile.Request]
}

func (r *ProviderReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req)
}

// SetupWithManager sets up the Provider controller with the Manager.
func (r *ProviderReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *pmconfig.CommonServiceConfig,
	eventPredicates ...predicate.Predicate) error {
	opts := controller.TypedOptions[mcreconcile.Request]{
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles,
		RateLimiter:             r.rateLimiter,
	}
	predicates := append([]predicate.Predicate{filter.DebugResourcesBehaviourPredicate(cfg.DebugLabelValue)}, eventPredicates...)
	return mcbuilder.ControllerManagedBy(mgr).
		Named(ProviderControllerName).
		For(&providersv1alpha1.Provider{}, mcbuilder.WithEngageWithProviderClusters(true)).
		WithOptions(opts).
		WithEventFilter(predicate.And(predicates...)).
		Complete(r)
}

func NewProviderReconciler(mgr mcmanager.Manager, operatorCfg *config.OperatorConfig, commonCfg *pmconfig.CommonServiceConfig, localClient client.Client) (*ProviderReconciler, error) {
	kcpUrl := operatorCfg.KCP.Url
	if kcpUrl == "" {
		kcpUrl = fmt.Sprintf("https://%s-front-proxy.%s:%s", operatorCfg.KCP.FrontProxyName, operatorCfg.KCP.Namespace, operatorCfg.KCP.FrontProxyPort)
	}

	rl, err := ratelimiter.NewStaticThenExponentialRateLimiter[mcreconcile.Request](ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating rate limiter: %w", err)
	}

	kcpHelper := &pmsubroutines.Helper{}

	var subs []subroutines.Subroutine

	if operatorCfg.Subroutines.Provider.Workspace.Enabled {
		sub, err := pmsubs.NewProviderWorkspaceSubroutine(localClient, kcpHelper, operatorCfg.KCP, kcpUrl)
		if err != nil {
			return nil, fmt.Errorf("error creating ProviderWorkspaceSubroutine: %v", err)
		}
		subs = append(subs, sub)
	}

	if operatorCfg.Subroutines.Provider.Kubeconfig.Enabled {
		subs = append(subs, pmsubs.NewScopedKubeconfigSubroutine(
			localClient,
			kcpHelper,
			operatorCfg.KCP,
			kcpUrl,
			func(ctx context.Context) (client.Client, error) {
				cluster, err := mgr.ClusterFromContext(ctx)
				if err != nil {
					return nil, err
				}
				return cluster.GetClient(), nil
			},
		))
	}

	lc := lifecycle.New(mgr, ProviderControllerName, func() client.Object {
		return &providersv1alpha1.Provider{}
	}, subs...).WithConditions(conditions.NewManager())

	return &ProviderReconciler{
		lifecycle:   lc,
		rateLimiter: rl,
	}, nil
}
