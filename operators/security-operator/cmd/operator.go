package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	apisv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	kcpcorev1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	"github.com/kcp-dev/logicalcluster/v3"
	accountsv1alpha1 "github.com/openmfp/account-operator/api/v1alpha1"
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
	"sigs.k8s.io/controller-runtime/pkg/kcp"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	openmfpcontext "github.com/openmfp/golang-commons/context"
	"github.com/openmfp/golang-commons/logger"
	"github.com/openmfp/golang-commons/sentry"
	"github.com/spf13/cobra"

	corev1alpha1 "github.com/openmfp/fga-operator/api/v1alpha1"
	"github.com/openmfp/fga-operator/internal/controller"
	"github.com/openmfp/fga-operator/internal/subroutine"
	// +kubebuilder:scaffold:imports
)

var (
	scheme = runtime.NewScheme()
)

func logicalClusterClientFromKey(mgr ctrl.Manager, log *logger.Logger) subroutine.NewLogicalClusterClientFunc {
	return func(clusterKey logicalcluster.Name) (client.Client, error) {
		cfg := rest.CopyConfig(mgr.GetConfig())

		parsed, err := url.Parse(cfg.Host)
		if err != nil {
			log.Error().Err(err).Msg("unable to parse host")
			return nil, err
		}

		parsed.Path = fmt.Sprintf("/clusters/%s", clusterKey)

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

		ctx, _, shutdown := openmfpcontext.StartContext(log, defaultCfg, defaultCfg.ShutdownTimeout)
		defer shutdown()

		if defaultCfg.Sentry.Dsn != "" {
			err := sentry.Start(ctx,
				defaultCfg.Sentry.Dsn, defaultCfg.Environment, defaultCfg.Region,
				defaultCfg.Image.Name, defaultCfg.Image.Tag,
			)
			if err != nil {
				log.Fatal().Err(err).Msg("Sentry init failed")
			}

			defer openmfpcontext.Recover(log)
		}

		cfg := ctrl.GetConfigOrDie()

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
			LeaderElectionID:       "fga-operator.openmfp.org",
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
			log.Error().Err(err).Msg("unable to start manager")
			return err
		}

		conn, err := grpc.NewClient(appCfg.FGA.Target, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Error().Err(err).Msg("unable to create grpc client")
			return err
		}

		fga := openfgav1.NewOpenFGAServiceClient(conn)

		if err = controller.NewStoreReconciler(log, mgr.GetClient(), fga, logicalClusterClientFromKey(mgr, log)).
			SetupWithManager(mgr, defaultCfg, log); err != nil {
			log.Error().Err(err).Str("controller", "store").Msg("unable to create controller")
			return err
		}
		if err = controller.
			NewAuthorizationModelReconciler(log, mgr.GetClient(), fga, logicalClusterClientFromKey(mgr, log)).
			SetupWithManager(mgr, defaultCfg, log); err != nil {
			log.Error().Err(err).Str("controller", "authorizationmodel").Msg("unable to create controller")
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

	utilruntime.Must(corev1alpha1.AddToScheme(scheme))
	utilruntime.Must(apisv1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpcorev1alpha1.AddToScheme(scheme))
	utilruntime.Must(accountsv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}
