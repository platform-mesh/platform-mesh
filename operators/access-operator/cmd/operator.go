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

package cmd

import (
	"context"
	"crypto/tls"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"go.platform-mesh.io/access-operator/pkg/config"
	"go.platform-mesh.io/access-operator/pkg/controller/accessgrant"
	pmaccessv1alpha1 "go.platform-mesh.io/apis/access/v1alpha1"
	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	pmconfig "go.platform-mesh.io/golang-commons/config"
	pmcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/logger"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	mcapiexportprovider "github.com/kcp-dev/multicluster-provider/apiexport"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

const defaultWaitForApiexportProviderPeriod = 15 * time.Second

var (
	scheme      = runtime.NewScheme()
	setupLog    logr.Logger
	operatorCfg config.OperatorConfig
	defaultCfg  *pmconfig.CommonServiceConfig
	log         *logger.Logger
)

var rootCmd = &cobra.Command{
	Use:   "access-operator",
	Short: "operator that drives the /access handshake against user-side access SDKs",
}

var operatorCmd = &cobra.Command{
	Use:   "operator",
	Short: "Run the access-operator",
	Run:   runController,
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(pmaccessv1alpha1.AddToScheme(scheme))
	utilruntime.Must(pmprovidersv1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(scheme))

	rootCmd.AddCommand(operatorCmd)

	defaultCfg = pmconfig.NewDefaultConfig()
	operatorCfg = config.NewOperatorConfig()
	defaultCfg.AddFlags(rootCmd.PersistentFlags())
	operatorCfg.AddFlags(operatorCmd.Flags())

	cobra.OnInitialize(initLog)
}

func initLog() { // coverage-ignore
	logcfg := logger.DefaultConfig()
	logcfg.Level = defaultCfg.Log.Level
	logcfg.NoJSON = defaultCfg.Log.NoJson

	var err error
	log, err = logger.New(logcfg)
	if err != nil {
		panic(err)
	}
	ctrl.SetLogger(log.Logr())
	setupLog = ctrl.Log.WithName("setup")
}

// Execute is the entrypoint called from the operator's main package.
func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}

func runController(_ *cobra.Command, _ []string) { // coverage-ignore
	ctrl.SetLogger(log.ComponentLogger("controller-runtime").Logr())

	log.Info().Msg("Starting access-operator")
	defer log.Info().Msg("Shutting down access-operator")

	ctx, _, shutdown := pmcontext.StartContext(log, operatorCfg, defaultCfg.ShutdownTimeout)
	defer shutdown()

	disableHTTP2 := func(c *tls.Config) {
		log.Info().Msg("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}
	var tlsOpts []func(*tls.Config)
	if !defaultCfg.EnableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	restCfg := ctrl.GetConfigOrDie()

	var leaderCfg *rest.Config
	if defaultCfg.LeaderElectionEnabled {
		var err error
		leaderCfg, err = rest.InClusterConfig()
		if err != nil {
			log.Fatal().Err(err).Msg("unable to get in-cluster config")
		}
	}

	kcpCfg, err := clientcmd.BuildConfigFromFlags("", operatorCfg.KCP.Kubeconfig)
	if err != nil {
		setupLog.Error(err, "failed to load kcp kubeconfig", "path", operatorCfg.KCP.Kubeconfig)
		os.Exit(1)
	}

	setupLog.Info("starting apiexport-access.platform-mesh.io provider")
	var provider *mcapiexportprovider.Provider
	err = wait.PollUntilContextCancel(ctx, defaultWaitForApiexportProviderPeriod, true, func(ctx context.Context) (bool, error) {
		var err error
		provider, err = mcapiexportprovider.New(kcpCfg, operatorCfg.KCP.APIExportEndpointSliceName, mcapiexportprovider.Options{
			Scheme: scheme,
		})
		if err != nil {
			setupLog.Error(err, "failed to create apiexport provider", "endpointslice", operatorCfg.KCP.APIExportEndpointSliceName)
		}
		return err == nil, nil
	})
	if err != nil {
		setupLog.Error(err, "failed to create apiexport provider")
		os.Exit(1)
	}

	mgr, err := mcmanager.New(restCfg, provider, mcmanager.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   defaultCfg.Metrics.BindAddress,
			SecureServing: defaultCfg.Metrics.Secure,
			TLSOpts:       tlsOpts,
		},
		BaseContext:                   func() context.Context { return ctx },
		HealthProbeBindAddress:        defaultCfg.HealthProbeBindAddress,
		LeaderElection:                defaultCfg.LeaderElectionEnabled,
		LeaderElectionID:              "access-operator.platform-mesh.io",
		LeaderElectionConfig:          leaderCfg,
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
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

	c, err := accessgrant.NewController(mgr, &operatorCfg)
	if err != nil {
		setupLog.Error(err, "failed to create AccessGrant controller")
		os.Exit(1)
	}
	if err := c.SetupWithManager(mgr, defaultCfg); err != nil {
		setupLog.Error(err, "failed to set up AccessGrant controller")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil && ctx.Err() == nil {
		log.Fatal().Err(err).Msg("problem running manager")
	}
}
