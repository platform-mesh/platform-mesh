package cmd

import (
	"crypto/tls"
	"os"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/kcp-dev/multicluster-provider/initializingworkspaces"
	pmcontext "github.com/platform-mesh/golang-commons/context"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/platform-mesh/security-operator/internal/controller"
)

var initializerCmd = &cobra.Command{
	Use:   "initializer",
	Short: "FGA initializer for the organization workspacetype",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, _, shutdown := pmcontext.StartContext(log, initializerCfg, defaultCfg.ShutdownTimeout)
		defer shutdown()

		restCfg, err := getKubeconfigFromPath(initializerCfg.KCP.Kubeconfig)
		if err != nil {
			log.Error().Err(err).Msg("unable to get KCP kubeconfig")
			os.Exit(1)
		}

		mgrOpts := ctrl.Options{
			Scheme:                 scheme,
			LeaderElection:         defaultCfg.LeaderElection.Enabled,
			LeaderElectionID:       "security-operator-initializer.platform-mesh.io",
			HealthProbeBindAddress: defaultCfg.HealthProbeBindAddress,
			Metrics: server.Options{
				BindAddress: defaultCfg.Metrics.BindAddress,
				TLSOpts: []func(*tls.Config){
					func(c *tls.Config) {
						log.Info().Msg("disabling http/2")
						c.NextProtos = []string{"http/1.1"}
					},
				},
			},
		}
		if defaultCfg.LeaderElection.Enabled {
			inClusterCfg, err := rest.InClusterConfig()
			if err != nil {
				log.Error().Err(err).Msg("unable to create in-cluster config")
				return err
			}
			mgrOpts.LeaderElectionConfig = inClusterCfg
		}

		provider, err := initializingworkspaces.New(restCfg, initializingworkspaces.Options{
			InitializerName: initializerCfg.InitializerName,
			Scheme:          mgrOpts.Scheme,
		})
		if err != nil {
			log.Error().Err(err).Msg("unable to construct cluster provider")
			os.Exit(1)
		}

		mgr, err := mcmanager.New(restCfg, provider, mgrOpts)
		if err != nil {
			setupLog.Error(err, "Failed to create manager")
			os.Exit(1)
		}

		runtimeScheme := runtime.NewScheme()
		utilruntime.Must(sourcev1.AddToScheme(runtimeScheme))
		utilruntime.Must(helmv2.AddToScheme(runtimeScheme))

		orgClient, err := logicalClusterClientFromKey(mgr.GetLocalManager(), log)(logicalcluster.Name("root:orgs"))
		if err != nil {
			setupLog.Error(err, "Failed to create org client")
			os.Exit(1)
		}

		k8sCfg := ctrl.GetConfigOrDie()

		runtimeClient, err := client.New(k8sCfg, client.Options{Scheme: scheme})
		if err != nil {
			log.Error().Err(err).Msg("Failed to create in cluster client")
			os.Exit(1)
		}

		if initializerCfg.IDP.AdditionalRedirectURLs == nil {
			initializerCfg.IDP.AdditionalRedirectURLs = []string{}
		}

		if err := controller.NewLogicalClusterReconciler(log, orgClient, initializerCfg, runtimeClient, mgr).
			SetupWithManager(mgr, defaultCfg); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "LogicalCluster")
			os.Exit(1)
		}

		if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
			setupLog.Error(err, "unable to set up health check")
			os.Exit(1)
		}
		if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
			setupLog.Error(err, "unable to set up ready check")
			os.Exit(1)
		}

		go func() {
			if err := provider.Run(ctx, mgr); err != nil {
				log.Fatal().Err(err).Msg("unable to run provider")
			}
		}()

		setupLog.Info("starting manager")

		return mgr.Start(ctrl.SetupSignalHandler())
	},
}
