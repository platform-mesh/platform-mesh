package controller

import (
	"context"
	"net/url"
	"strings"

	kcpv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	"github.com/kcp-dev/logicalcluster/v3"
	platformeshconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/builder"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/rs/zerolog/log"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/platform-mesh/golang-commons/controller/lifecycle/multicluster"
	"github.com/platform-mesh/security-operator/internal/subroutine"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
)

func getAllClient(mcMgr mcmanager.Manager) (client.Client, error) {
	allCfg := rest.CopyConfig(mcMgr.GetLocalManager().GetConfig())

	parsed, err := url.Parse(allCfg.Host)
	if err != nil {
		log.Error().Err(err).Msg("unable to parse host from config")
		return nil, err
	}

	parts := strings.Split(parsed.Path, "clusters")

	parsed.Path, err = url.JoinPath(parts[0], "clusters", logicalcluster.Wildcard.String())
	if err != nil {
		log.Error().Err(err).Msg("unable to join path")
		return nil, err
	}

	allCfg.Host = parsed.String()

	log.Info().Str("host", allCfg.Host).Msg("using host")

	allClient, err := client.New(allCfg, client.Options{
		Scheme: mcMgr.GetLocalManager().GetScheme(),
	})
	if err != nil {
		return nil, err
	}
	return allClient, nil
}

func NewAPIBindingReconciler(logger *logger.Logger, mcMgr mcmanager.Manager) *APIBindingReconciler {
	allclient, err := getAllClient(mcMgr)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to create new client")
	}

	return &APIBindingReconciler{
		log: logger,
		mclifecycle: builder.NewBuilder("apibinding", "apibinding-controller", []lifecyclesubroutine.Subroutine{
			subroutine.NewAuthorizationModelGenerationSubroutine(mcMgr, allclient),
		}, logger).
			BuildMultiCluster(mcMgr),
	}
}

type APIBindingReconciler struct {
	log         *logger.Logger
	mclifecycle *multicluster.LifecycleManager
}

func (r *APIBindingReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	ctxWithCluster := mccontext.WithCluster(ctx, req.ClusterName)
	return r.mclifecycle.Reconcile(ctxWithCluster, req, &kcpv1alpha1.APIBinding{})
}

func (r *APIBindingReconciler) SetupWithManager(mgr mcmanager.Manager, cfg *platformeshconfig.CommonServiceConfig, evp ...predicate.Predicate) error {
	return r.mclifecycle.SetupWithManager(mgr, cfg.MaxConcurrentReconciles, "apibinding-controller", &kcpv1alpha1.APIBinding{}, cfg.DebugLabelValue, r, r.log, evp...)
}
