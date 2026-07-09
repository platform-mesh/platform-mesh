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
	"go.platform-mesh.io/provider-operator/internal/config"
	pmsubroutines "go.platform-mesh.io/provider-operator/pkg/subroutines"
	pmsubs "go.platform-mesh.io/provider-operator/pkg/subroutines/providers"
)

const ManagedProviderControllerName = "ManagedProviderReconciler"

// ManagedProviderReconciler reconciles a ManagedProvider object
type ManagedProviderReconciler struct {
	lifecycle   *lifecycle.Lifecycle
	rateLimiter workqueue.TypedRateLimiter[mcreconcile.Request]
}

// +kubebuilder:rbac:groups=providers.platform-mesh.io,resources=managedproviders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=providers.platform-mesh.io,resources=managedproviders/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=providers.platform-mesh.io,resources=managedproviders/finalizers,verbs=update

func (r *ManagedProviderReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(ctx, req)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedProviderReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *pmconfig.CommonServiceConfig,
	eventPredicates ...predicate.Predicate) error {
	opts := controller.TypedOptions[mcreconcile.Request]{
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles,
		RateLimiter:             r.rateLimiter,
	}
	predicates := append([]predicate.Predicate{filter.DebugResourcesBehaviourPredicate(cfg.DebugLabelValue)}, eventPredicates...)
	return mcbuilder.ControllerManagedBy(mgr).
		Named(ManagedProviderControllerName).
		For(&providersv1alpha1.ManagedProvider{}, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(false)).
		WithOptions(opts).
		WithEventFilter(predicate.And(predicates...)).
		Complete(r)
}

func NewManagedProviderReconciler(mgr mcmanager.Manager, operatorCfg *config.OperatorConfig, commonCfg *pmconfig.CommonServiceConfig) (*ManagedProviderReconciler, error) {
	kcpUrl := operatorCfg.KCP.Url
	if kcpUrl == "" {
		kcpUrl = fmt.Sprintf("https://%s-front-proxy.%s:%s", operatorCfg.KCP.FrontProxyName, operatorCfg.KCP.Namespace, operatorCfg.KCP.FrontProxyPort)
	}

	localCl := mgr.GetLocalManager().GetClient()
	kcpHelper := &pmsubroutines.Helper{}

	var subs []subroutines.Subroutine
	if operatorCfg.Subroutines.ManagedProvider.ProviderResource.Enabled {
		sub, err := pmsubs.NewProviderResourceSubroutine(localCl, kcpHelper, operatorCfg, kcpUrl)
		if err != nil {
			return nil, fmt.Errorf("error creating ProviderResourceSubroutine: %v", err)
		}
		subs = append(subs, sub)
	}
	if operatorCfg.Subroutines.ManagedProvider.WaitProvider.Enabled {
		subs = append(subs, pmsubs.NewWaitProviderSubroutine(localCl, kcpHelper, operatorCfg, kcpUrl))
	}
	if operatorCfg.Subroutines.ManagedProvider.KubeconfigCopy.Enabled {
		subs = append(subs, pmsubs.NewKubeconfigCopySubroutine(localCl, kcpHelper, operatorCfg, kcpUrl))
	}
	if operatorCfg.Subroutines.ManagedProvider.Deploy.Enabled {
		sub, err := pmsubs.NewDeploySubroutine(localCl)
		if err != nil {
			return nil, fmt.Errorf("error creating DeploySubroutine: %v", err)
		}
		subs = append(subs, sub)
	}

	rl, err := ratelimiter.NewStaticThenExponentialRateLimiter[mcreconcile.Request](ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("error creating rate limiter: %w", err)
	}

	lc := lifecycle.New(mgr, ManagedProviderControllerName, func() client.Object {
		return &providersv1alpha1.ManagedProvider{}
	}, subs...).WithConditions(conditions.NewManager())

	return &ManagedProviderReconciler{
		lifecycle:   lc,
		rateLimiter: rl,
	}, nil
}
