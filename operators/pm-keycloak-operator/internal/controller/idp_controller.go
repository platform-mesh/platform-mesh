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
	pmkeycloakv1alpha1 "go.platform-mesh.io/apis/keycloak/v1alpha1"
	platformeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/controller/filter"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/pm-keycloak-operator/internal/client"
	"go.platform-mesh.io/pm-keycloak-operator/internal/legacy"
	"go.platform-mesh.io/pm-keycloak-operator/internal/metrics"
	"go.platform-mesh.io/pm-keycloak-operator/internal/subroutine/idp"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/config"
	"go.platform-mesh.io/subroutines"
	"go.platform-mesh.io/subroutines/conditions"
	"go.platform-mesh.io/subroutines/lifecycle"

	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

// IdentityProviderConfigurationReconciler reconciles IdentityProviderConfigurations
// of one API group against Keycloak.
type IdentityProviderConfigurationReconciler struct {
	log         *logger.Logger
	name        string
	newObj      func() ctrlruntimeclient.Object
	engageOpts  mcbuilder.EngageOptions
	lifecycle   *lifecycle.Lifecycle
	rateLimiter workqueue.TypedRateLimiter[mcreconcile.Request]
}

// NewIdentityProviderConfigurationReconciler reconciles keycloak.platform-mesh.io
// IdentityProviderConfigurations engaged through the keycloak provider.
func NewIdentityProviderConfigurationReconciler(ctx context.Context, mgr mcmanager.Manager, kcpClientGetter iclient.KCPClientGetter, cfg *config.Config, log *logger.Logger) (*IdentityProviderConfigurationReconciler, error) {
	return newIdentityProviderConfigurationReconciler(ctx, mgr, kcpClientGetter, cfg, log, "identityprovider",
		func() ctrlruntimeclient.Object { return &pmkeycloakv1alpha1.IdentityProviderConfiguration{} },
		func(sub subroutines.Subroutine) subroutines.Subroutine { return sub },
		WithClustersFromNamedProvider(config.KeycloakProviderName, config.ProviderSeparator))
}

// NewLegacyIdentityProviderConfigurationReconciler reconciles the deprecated
// core.platform-mesh.io IdentityProviderConfigurations engaged through the
// system provider (feature gate LegacyAPIs).
func NewLegacyIdentityProviderConfigurationReconciler(ctx context.Context, mgr mcmanager.Manager, kcpClientGetter iclient.KCPClientGetter, cfg *config.Config, log *logger.Logger) (*IdentityProviderConfigurationReconciler, error) {
	return newIdentityProviderConfigurationReconciler(ctx, mgr, kcpClientGetter, cfg, log, "identityprovider-legacy",
		//nolint:staticcheck // legacy kind served on purpose
		func() ctrlruntimeclient.Object { return &pmcorev1alpha1.IdentityProviderConfiguration{} },
		func(sub subroutines.Subroutine) subroutines.Subroutine {
			return legacy.IdentityProviderConfiguration(sub)
		},
		WithClustersFromNamedProvider(config.SystemProviderName, config.ProviderSeparator))
}

func newIdentityProviderConfigurationReconciler(ctx context.Context, mgr mcmanager.Manager, kcpClientGetter iclient.KCPClientGetter, cfg *config.Config, log *logger.Logger,
	name string, newObj func() ctrlruntimeclient.Object, wrap func(subroutines.Subroutine) subroutines.Subroutine, engageOpts mcbuilder.EngageOptions,
) (*IdentityProviderConfigurationReconciler, error) {
	idpSubroutine, err := idp.New(ctx, cfg, mgr, kcpClientGetter)
	if err != nil {
		return nil, fmt.Errorf("creating IDP subroutine: %w", err)
	}

	rl, err := ratelimiter.NewStaticThenExponentialRateLimiter[mcreconcile.Request](ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %w", err)
	}

	lc := lifecycle.New(mgr, "IdentityProviderConfigurationReconciler", newObj, wrap(idpSubroutine)).
		WithConditions(conditions.NewManager())

	return &IdentityProviderConfigurationReconciler{
		log:         log,
		name:        name,
		newObj:      newObj,
		engageOpts:  engageOpts,
		lifecycle:   lc,
		rateLimiter: rl,
	}, nil
}

func (r *IdentityProviderConfigurationReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	start := time.Now()
	result, err := r.lifecycle.Reconcile(ctx, req)
	labelResult := "success"
	if err != nil {
		labelResult = "error"
	}
	metrics.ReconcileTotal.WithLabelValues(r.name, labelResult).Inc()
	metrics.ReconcileDuration.WithLabelValues(r.name).Observe(time.Since(start).Seconds())
	return result, err
}

func (r *IdentityProviderConfigurationReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformeshconfig.CommonServiceConfig, log *logger.Logger, evp ...predicate.Predicate) error {
	opts := controller.TypedOptions[mcreconcile.Request]{
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles,
		RateLimiter:             r.rateLimiter,
	}
	predicates := append([]predicate.Predicate{filter.DebugResourcesBehaviourPredicate(cfg.DebugLabelValue)}, evp...)
	return mcbuilder.ControllerManagedBy(mgr).
		Named(r.name).
		For(r.newObj(), r.engageOpts).
		WithOptions(opts).
		WithEventFilter(predicate.And(predicates...)).
		Complete(r)
}
