package cmd

import (
	"context"
	"crypto/tls"

	platformeshcontext "github.com/platform-mesh/golang-commons/context"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/kcp"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	securityv1alpha1 "github.com/platform-mesh/security-operator/api/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/controller"
)

var modelGeneratorCmd = &cobra.Command{
	Use: "model-generator",
	RunE: func(cmd *cobra.Command, args []string) error {

		ctrl.SetLogger(log.ComponentLogger("controller-runtime").Logr())

		ctx, _, shutdown := platformeshcontext.StartContext(log, defaultCfg, defaultCfg.ShutdownTimeout)
		defer shutdown()

		cfg := ctrl.GetConfigOrDie()

		mgrOpts := manager.Options{
			Scheme: scheme,
			Metrics: server.Options{
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
			LeaderElectionID:       "security-operator-generator.platform-mesh.io",
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

		mgr, err := kcp.NewClusterAwareManager(cfg, mgrOpts)
		if err != nil {
			setupLog.Error(err, "unable to setup manager")
		}

		runtimeScheme := runtime.NewScheme()
		utilruntime.Must(appsv1.AddToScheme(runtimeScheme))
		utilruntime.Must(securityv1alpha1.AddToScheme(runtimeScheme))

		if err := controller.NewAPIBindingReconciler(mgr.GetClient(), log, logicalClusterClientFromKey(mgr, log)).
			SetupWithManager(mgr, log, defaultCfg); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Resource")
			return err
		}

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
			setupLog.Error(err, "problem running manager")
			return err
		}

		return nil
	},
}
