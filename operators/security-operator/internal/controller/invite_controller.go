package controller // coverage-ignore

import (
	"context"
	"os"

	platformeshconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/builder"
	lifecycle "github.com/platform-mesh/golang-commons/controller/lifecycle/multicluster"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/logger"
	ctrl "sigs.k8s.io/controller-runtime"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/platform-mesh/security-operator/api/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/config"
	"github.com/platform-mesh/security-operator/internal/subroutine/invite"
)

type InviteReconciler struct {
	lifecycle *lifecycle.LifecycleManager
}

func NewInviteReconciler(ctx context.Context, mgr mcmanager.Manager, cfg *config.Config, log *logger.Logger) *InviteReconciler {
	pwd, err := os.ReadFile(cfg.Invite.KeycloakPasswordFile)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to read keycloak password file")
	}

	inviteSubroutine, err := invite.New(ctx, cfg, mgr, string(pwd))
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create invite subroutine")
	}

	return &InviteReconciler{
		lifecycle: builder.NewBuilder(
			"invite",
			"InviteReconciler",
			[]lifecyclesubroutine.Subroutine{
				inviteSubroutine,
			}, log,
		).WithConditionManagement().BuildMultiCluster(mgr),
	}
}

func (r *InviteReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	return r.lifecycle.Reconcile(mccontext.WithCluster(ctx, req.ClusterName), req, &v1alpha1.Invite{})
}

func (r *InviteReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformeshconfig.CommonServiceConfig, log *logger.Logger) error { // coverage-ignore
	return r.lifecycle.
		SetupWithManager(
			mgr,
			cfg.MaxConcurrentReconciles,
			"invite",
			&v1alpha1.Invite{},
			cfg.DebugLabelValue,
			r,
			log,
		)
}
