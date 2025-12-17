package controller // coverage-ignore

import (
	"context"

	platformeshconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/builder"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/multicluster"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/logger"
	corev1alpha1 "github.com/platform-mesh/security-operator/api/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/config"
	"github.com/platform-mesh/security-operator/internal/subroutine/idp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

type IdentityProviderConfigurationReconciler struct {
	log         *logger.Logger
	mclifecycle *multicluster.LifecycleManager
}

func NewIdentityProviderConfigurationReconciler(ctx context.Context, mgr mcmanager.Manager, orgsClient client.Client, cfg *config.Config, log *logger.Logger) *IdentityProviderConfigurationReconciler {
	idpSubroutine, err := idp.New(ctx, cfg, orgsClient, mgr)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create idp subroutine")
	}
	return &IdentityProviderConfigurationReconciler{
		log: log,
		mclifecycle: builder.NewBuilder("identityprovider", "IdentityProviderConfigurationReconciler", []lifecyclesubroutine.Subroutine{
			idpSubroutine,
		}, log).WithConditionManagement().WithStaticThenExponentialRateLimiter().
			BuildMultiCluster(mgr),
	}
}

func (r *IdentityProviderConfigurationReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	ctxWithCluster := mccontext.WithCluster(ctx, req.ClusterName)
	return r.mclifecycle.Reconcile(ctxWithCluster, req, &corev1alpha1.IdentityProviderConfiguration{})
}

func (r *IdentityProviderConfigurationReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformeshconfig.CommonServiceConfig, log *logger.Logger, evp ...predicate.Predicate) error { // coverage-ignore
	return r.mclifecycle.SetupWithManager(mgr, cfg.MaxConcurrentReconciles, "identityprovider", &corev1alpha1.IdentityProviderConfiguration{}, cfg.DebugLabelValue, r, r.log, evp...)
}
