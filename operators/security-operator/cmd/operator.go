package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"

	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/kcp-dev/multicluster-provider/apiexport"
	kcpv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	apisv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	accountsv1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	platformeshcontext "github.com/platform-mesh/golang-commons/context"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/platform-mesh/golang-commons/sentry"
	"github.com/spf13/cobra"

	corev1alpha1 "github.com/platform-mesh/security-operator/api/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/controller"
	// +kubebuilder:scaffold:imports
)

var (
	scheme = runtime.NewScheme()
)

type NewLogicalClusterClientFunc func(clusterKey logicalcluster.Name) (client.Client, error)

func logicalClusterClientFromKey(mgr ctrl.Manager, log *logger.Logger) NewLogicalClusterClientFunc {
	return func(clusterKey logicalcluster.Name) (client.Client, error) {
		cfg := rest.CopyConfig(mgr.GetConfig())

		parsed, err := url.Parse(cfg.Host)
		if err != nil {
			log.Error().Err(err).Msg("unable to parse host")
			return nil, err
		}

		parsed.Path = fmt.Sprintf("/clusters/%s", clusterKey)

		log.Info().Msg(fmt.Sprintf("HOST from logical cluster client from key -- %s", parsed.String()))

		cfg.Host = parsed.String()

		return client.New(cfg, client.Options{
			Scheme: scheme,
		})
	}
}

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
			LeaderElection:         defaultCfg.LeaderElection.Enabled,
			LeaderElectionID:       "security-operator.platform-mesh.io",
			BaseContext:            func() context.Context { return ctx },
		}
		if defaultCfg.LeaderElection.Enabled {
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

		provider, err := apiexport.New(restCfg, apiexport.Options{
			Scheme:        mgrOpts.Scheme,
			ObjectToWatch: &kcpv1alpha2.APIBinding{},
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

		fga := openfgav1.NewOpenFGAServiceClient(conn)

		if err = controller.NewStoreReconciler(log, fga, mgr).
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
		if err = controller.NewInviteReconciler(ctx, mgr, &operatorCfg, log).SetupWithManager(mgr, defaultCfg, log); err != nil {
			log.Error().Err(err).Str("controller", "invite").Msg("unable to create controller")
			return err
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

		go func() {
			if err := provider.Run(ctx, mgr); err != nil {
				log.Fatal().Err(err).Msg("unable to run provider")
			}
		}()

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

	utilruntime.Must(kcpv1alpha2.AddToScheme(scheme))
	utilruntime.Must(apisv1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpcorev1alpha1.AddToScheme(scheme))

	utilruntime.Must(accountsv1alpha1.AddToScheme(scheme))
	utilruntime.Must(corev1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}
