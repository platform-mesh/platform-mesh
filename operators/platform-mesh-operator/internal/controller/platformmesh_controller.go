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
	"fmt"
	"time"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	featuremanifests "go.platform-mesh.io/apis/static-assets/features"
	kcpmanifests "go.platform-mesh.io/apis/static-assets/kcp"
	pmconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/controller/filter"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"
	"go.platform-mesh.io/platform-mesh-operator/internal/metrics"
	pmsubs "go.platform-mesh.io/platform-mesh-operator/pkg/subroutines"
	"go.platform-mesh.io/subroutines"
	"go.platform-mesh.io/subroutines/conditions"
	"go.platform-mesh.io/subroutines/lifecycle"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

var (
	pmReconcilerName = "PlatformMeshReconciler"
)

// PlatformMeshReconciler reconciles a PlatformMesh object
type PlatformMeshReconciler struct {
	lifecycle   *lifecycle.Lifecycle
	rateLimiter workqueue.TypedRateLimiter[mcreconcile.Request]
	client      ctrlruntimeclient.Client
}

// +kubebuilder:rbac:groups=core.platform-mesh.io,resources=platformmeshes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.platform-mesh.io,resources=platformmeshes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.platform-mesh.io,resources=platformmeshes/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch

func (r *PlatformMeshReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	result, err := r.lifecycle.Reconcile(ctx, req)
	labelResult := "success"
	if err != nil {
		labelResult = "error"
	}
	metrics.ReconcileTotal.WithLabelValues(pmReconcilerName, labelResult).Inc()
	return result, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *PlatformMeshReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *pmconfig.CommonServiceConfig,
	eventPredicates ...predicate.Predicate) error {
	opts := controller.TypedOptions[mcreconcile.Request]{
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles,
		RateLimiter:             r.rateLimiter,
	}
	predicates := append([]predicate.Predicate{filter.DebugResourcesBehaviourPredicate(cfg.DebugLabelValue)}, eventPredicates...)
	return mcbuilder.ControllerManagedBy(mgr).
		Named(pmReconcilerName).
		For(&pmcorev1alpha1.PlatformMesh{}, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(false)).
		WithOptions(opts).
		WithEventFilter(predicate.And(predicates...)).
		Complete(r)
}

// mapConfigMapToPlatformMesh finds all PlatformMesh resources that reference the given ConfigMap
// via spec.profileConfigMap and returns reconcile requests for them.
func (r *PlatformMeshReconciler) mapConfigMapToPlatformMesh(ctx context.Context, obj ctrlruntimeclient.Object) []reconcile.Request {
	var requests []reconcile.Request
	configMap, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return requests
	}

	platformMeshList := &pmcorev1alpha1.PlatformMeshList{}
	if err := r.client.List(ctx, platformMeshList); err != nil {
		return requests
	}

	for _, pm := range platformMeshList.Items {
		configMapNamespace := pm.Namespace

		var configMapName string
		if pm.Spec.ProfileConfigMap != nil {
			configMapName = pm.Spec.ProfileConfigMap.Name
			if pm.Spec.ProfileConfigMap.Namespace != "" {
				configMapNamespace = pm.Spec.ProfileConfigMap.Namespace
			}
		} else {
			configMapName = pm.Name + "-profile"
		}

		if configMap.Name == configMapName && configMap.Namespace == configMapNamespace {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      pm.Name,
					Namespace: pm.Namespace,
				},
			})
		}
	}

	return requests
}

func NewPlatformMeshReconciler(mgr mcmanager.Manager, cfg *config.OperatorConfig, commonCfg *pmconfig.CommonServiceConfig, clientInfra ctrlruntimeclient.Client, imageVersionStore *pmsubs.ImageVersionStore) (*PlatformMeshReconciler, error) {
	kcpUrl := fmt.Sprintf("https://%s-front-proxy.%s:%s", cfg.KCP.FrontProxyName, cfg.KCP.Namespace, cfg.KCP.FrontProxyPort)
	if cfg.KCP.Url != "" {
		kcpUrl = cfg.KCP.Url
	}

	localCl := mgr.GetLocalManager().GetClient()

	var subs []subroutines.Subroutine
	if cfg.Subroutines.Deployment.Enabled {
		deploymentSub := pmsubs.NewDeploymentSubroutine(localCl, clientInfra, commonCfg, cfg)
		deploymentSub.SetImageVersionStore(imageVersionStore)
		subs = append(subs, deploymentSub)
	}
	if cfg.Subroutines.KcpSetup.Enabled {
		subs = append(subs, pmsubs.NewKcpsetupSubroutine(localCl, &pmsubs.Helper{}, cfg, kcpmanifests.Resources, kcpUrl))
	}
	if cfg.Subroutines.ProviderSecret.Enabled {
		subs = append(subs, pmsubs.NewProviderSecretSubroutine(localCl, &pmsubs.Helper{}, pmsubs.DefaultHelmGetter{}, kcpUrl))
	}
	if cfg.Subroutines.FeatureToggles.Enabled {
		subs = append(subs, pmsubs.NewFeatureToggleSubroutine(localCl, &pmsubs.Helper{}, featuremanifests.Resources, kcpUrl))
	}
	if cfg.Subroutines.Wait.Enabled {
		subs = append(subs, pmsubs.NewWaitSubroutine(clientInfra, localCl, cfg, &pmsubs.Helper{}, kcpUrl))
	}

	rl, err := ratelimiter.NewStaticThenExponentialRateLimiter[mcreconcile.Request](ratelimiter.NewConfig(
		ratelimiter.WithRequeueDelay(30*time.Second),
		ratelimiter.WithExponentialMaxBackoff(1*time.Minute),
		ratelimiter.WithStaticWindow(20*time.Minute),
		ratelimiter.WithExponentialInitialBackoff(30*time.Second),
	))
	if err != nil {
		return nil, fmt.Errorf("creating rate limiter: %w", err)
	}

	lc := lifecycle.New(mgr, pmReconcilerName, func() ctrlruntimeclient.Object {
		return &pmcorev1alpha1.PlatformMesh{}
	}, subs...).WithConditions(conditions.NewManager())

	return &PlatformMeshReconciler{
		lifecycle:   lc,
		rateLimiter: rl,
		client:      localCl,
	}, nil
}
