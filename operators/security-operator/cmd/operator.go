package cmd

import (
	"context"
	"crypto/tls"
	"fmt"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/spf13/cobra"
	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	platformeshcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/sentry"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/controller"
	fga2 "go.platform-mesh.io/security-operator/internal/fga"
	"go.platform-mesh.io/security-operator/internal/predicates"
	internalwebhook "go.platform-mesh.io/security-operator/internal/webhook"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	pathaware "github.com/kcp-dev/multicluster-provider/path-aware"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var (
	scheme = runtime.NewScheme()
)

var operatorCmd = &cobra.Command{
	Use: "fga",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctrl.SetLogger(log.ComponentLogger("controller-runtime").Logr())

		ctx, _, shutdown := platformeshcontext.StartContext(log, defaultCfg, defaultCfg.ShutdownTimeout)
		defer shutdown()

		restCfg, err := getKubeconfigFromPath(operatorCfg.KCP.Kubeconfig)
		if err != nil {
			log.Error().Err(err).Msg("unable to get KCP kubeconfig")
			return err
		}

		if defaultCfg.Sentry.Dsn != "" {
			err := sentry.Start(ctx,
				defaultCfg.Sentry.Dsn, defaultCfg.Environment, defaultCfg.Region,
				defaultCfg.Image.Name, defaultCfg.Image.Tag,
			)
			if err != nil {
				log.Fatal().Err(err).Msg("Sentry init failed")
			}

			defer platformeshcontext.Recover(log)
		}

		webhookServer := webhook.NewServer(webhook.Options{
			TLSOpts: []func(*tls.Config){
				func(c *tls.Config) {
					log.Info().Msg("disabling http/2")
					c.NextProtos = []string{"http/1.1"}
				},
			},
			CertDir: operatorCfg.Webhooks.CertDir,
			Port:    operatorCfg.Webhooks.Port,
		})

		mgrOpts := ctrl.Options{
			Scheme: scheme,
			Metrics: metricsserver.Options{
				BindAddress: defaultCfg.Metrics.BindAddress,
				TLSOpts: []func(*tls.Config){
					func(c *tls.Config) {
						log.Info().Msg("disabling http/2")
						c.NextProtos = []string{"http/1.1"}
					},
				},
			},
			HealthProbeBindAddress: defaultCfg.HealthProbeBindAddress,
			LeaderElection:         defaultCfg.LeaderElectionEnabled,
			LeaderElectionID:       "security-operator.platform-mesh.io",
			BaseContext:            func() context.Context { return ctx },
			WebhookServer:          webhookServer,
		}
		if defaultCfg.LeaderElectionEnabled {
			inClusterCfg, err := rest.InClusterConfig()
			if err != nil {
				log.Error().Err(err).Msg("unable to create in-cluster config")
				return err
			}
			mgrOpts.LeaderElectionConfig = inClusterCfg
		}

		if mgrOpts.Scheme == nil {
			log.Error().Err(fmt.Errorf("scheme should not be nil")).Msg("scheme should not be nil")
			return fmt.Errorf("scheme should not be nil")
		}

		provider, err := pathaware.New(restCfg, operatorCfg.APIExportEndpointSlices.CorePlatformMeshIO, apiexport.Options{
			Scheme: mgrOpts.Scheme,
		})
		if err != nil {
			setupLog.Error(err, "unable to construct cluster provider")
			return err
		}

		mgr, err := mcmanager.New(restCfg, provider, mgrOpts)
		if err != nil {
			setupLog.Error(err, "Failed to create manager")
			return err
		}

		conn, err := grpc.NewClient(operatorCfg.FGA.Target, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Error().Err(err).Msg("unable to create grpc client")
			return err
		}
		defer func() { _ = conn.Close() }()

		fga := openfgav1.NewOpenFGAServiceClient(conn)
		storeIDGetter := fga2.NewCachingStoreIDGetter(
			fga,
			operatorCfg.FGA.StoreIDCacheTTL,
			ctx,
			log,
		)

		k8sCfg := ctrl.GetConfigOrDie()

		runtimeClient, err := client.New(k8sCfg, client.Options{Scheme: scheme})
		if err != nil {
			log.Error().Err(err).Msg("Failed to create in cluster client")
			return err
		}
		providerLister := iclient.NewProviderLister(provider.Provider.Provider)

		if err = controller.NewStoreReconciler(ctx, log, fga, mgr, &operatorCfg, providerLister).
			SetupWithManager(mgr, defaultCfg); err != nil {
			log.Error().Err(err).Str("controller", "store").Msg("unable to create controller")
			return err
		}
		if err = controller.
			NewAuthorizationModelReconciler(log, fga, mgr).
			SetupWithManager(mgr, defaultCfg); err != nil {
			log.Error().Err(err).Str("controller", "authorizationmodel").Msg("unable to create controller")
			return err
		}

		kcpClientGetter := iclient.NewManagerKCPClientGetter(mgr, provider.Provider.Provider)
		kcpClientGetterWithConfig := iclient.NewConfigSchemeKCPClientGetter(restCfg, scheme)

		inviteReconciler, err := controller.NewInviteReconciler(ctx, mgr, &operatorCfg, log, kcpClientGetter)
		if err != nil {
			log.Error().Err(err).Str("controller", "invite").Msg("unable to create reconciler")
			return err
		}
		if err = inviteReconciler.SetupWithManager(mgr, defaultCfg, log); err != nil {
			log.Error().Err(err).Str("controller", "invite").Msg("unable to create controller")
			return err
		}
		orgReconciler, err := controller.NewOrgLogicalClusterController(log, kcpClientGetterWithConfig, operatorCfg, runtimeClient, mgr, controller.ControllerOptions{
			Name: "OrgLogicalClusterReconciler",
		})
		if err != nil {
			log.Error().Err(err).Str("controller", "logicalcluster").Msg("unable to create initializer")
			return err
		}
		if err = orgReconciler.SetupWithManager(mgr, defaultCfg,
			predicates.LogicalClusterIsAccountTypeOrg(),
			predicates.HasInitializerPredicate(operatorCfg.InitializerName()),
		); err != nil {
			log.Error().Err(err).Str("controller", "logicalcluster").Msg("unable to create controller")
			return err
		}

		alcReconciler, err := controller.NewAccountLogicalClusterController(log, operatorCfg, fga, storeIDGetter, mgr, kcpClientGetterWithConfig, controller.ControllerOptions{
			Name: "AccountLogicalClusterReconciler",
		})
		if err != nil {
			log.Error().Err(err).Str("controller", "accounttypelogicalcluster").Msg("unable to create reconciler")
			return err
		}
		if err = alcReconciler.SetupWithManager(mgr, defaultCfg,
			predicate.Not(predicates.LogicalClusterIsAccountTypeOrg()),
			predicates.HasInitializerPredicate(operatorCfg.InitializerName()),
		); err != nil {
			log.Error().Err(err).Str("controller", "accounttypelogicalcluster").Msg("unable to create controller")
			return err
		}
		if err = controller.NewAccountInfoReconciler(log, mgr).SetupWithManager(mgr, defaultCfg); err != nil {
			log.Error().Err(err).Str("controller", "accountinfo").Msg("unable to create controller")
			return err
		}

		if operatorCfg.Webhooks.Enabled {
			log.Info().Msg("validating webhooks are enabled")
			if err := internalwebhook.SetupIdentityProviderConfigurationValidatingWebhookWithManager(ctx, mgr.GetLocalManager(), &operatorCfg); err != nil {
				log.Error().Err(err).Str("webhook", "IdentityProviderConfiguration").Msg("unable to create webhook")
				return err
			}
		}
		// +kubebuilder:scaffold:builder

		if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
			log.Error().Err(err).Msg("unable to set up health check")
			return err
		}
		if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
			log.Error().Err(err).Msg("unable to set up ready check")
			return err
		}

		setupLog.Info("starting manager")
		if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
			log.Error().Err(err).Msg("problem running manager")
			return err
		}
		return nil
	},
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha1.AddToScheme(scheme))
	utilruntime.Must(corev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpapisv1alpha2.AddToScheme(scheme))
	utilruntime.Must(kcpcorev1alpha1.AddToScheme(scheme))
	utilruntime.Must(corev1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}
