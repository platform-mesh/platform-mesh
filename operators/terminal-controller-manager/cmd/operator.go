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
	"net/http"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	platformmeshcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/traces"
	"go.platform-mesh.io/terminal-controller-manager/internal/controller"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/kcp-dev/multicluster-provider/apiexport"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
)

var operatorCmd = &cobra.Command{
	Use:   "operator",
	Short: "operator to reconcile Terminals",
	Run:   RunController,
}

func RunController(_ *cobra.Command, _ []string) { // coverage-ignore
	var err error
	ctrl.SetLogger(log.ComponentLogger("controller-runtime").Logr())

	ctx, _, shutdown := platformmeshcontext.StartContext(log, operatorCfg, defaultCfg.ShutdownTimeout)
	defer shutdown()

	disableHTTP2 := func(c *tls.Config) {
		log.Info().Msg("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	var tlsOpts []func(*tls.Config)
	if !defaultCfg.EnableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	var traceShutdown func(ctx context.Context) error
	if defaultCfg.Tracing.Enabled {
		traceShutdown, err = traces.InitProvider(ctx, defaultCfg.Tracing.Collector)
		if err != nil {
			shutdown()
			log.Fatal().Err(err).Msg("unable to start gRPC-Sidecar TracerProvider") //nolint:gocritic // shutdown() is called before Fatal
		}
	}
	defer func() {
		if traceShutdown != nil {
			if err := traceShutdown(ctx); err != nil {
				log.Fatal().Err(err).Msg("failed to shutdown TracerProvider")
			}
		}
	}()

	// kcp config for watching Terminal CRs via APIExport
	// Uses --kcp-kubeconfig flag if set, otherwise falls back to in-cluster config
	kcpCfg, err := loadKcpConfig(operatorCfg.Kcp.Kubeconfig)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to load kcp kubeconfig")
	}
	kcpCfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return otelhttp.NewTransport(rt)
	})

	// Runtime cluster config for pod management
	// Uses standard KUBECONFIG env var or in-cluster config
	runtimeCfg := ctrl.GetConfigOrDie()

	var leaderCfg *rest.Config
	if defaultCfg.LeaderElectionEnabled {
		leaderCfg = rest.CopyConfig(runtimeCfg)
	}

	provider, err := apiexport.New(kcpCfg, operatorCfg.Kcp.APIExportEndpointSliceName, apiexport.Options{
		Log:    &ctrl.Log,
		Scheme: scheme,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("creating APIExport provider")
	}

	mgr, err := mcmanager.New(kcpCfg, provider, mcmanager.Options{
		Scheme: scheme,
		Cache: cache.Options{
			SyncPeriod: ptr.To(operatorCfg.Terminal.Lifetime),
		},
		Metrics: metricsserver.Options{
			BindAddress:   defaultCfg.Metrics.BindAddress,
			SecureServing: defaultCfg.Metrics.Secure,
			TLSOpts:       tlsOpts,
		},
		BaseContext:                   func() context.Context { return ctx },
		HealthProbeBindAddress:        defaultCfg.HealthProbeBindAddress,
		LeaderElection:                defaultCfg.LeaderElectionEnabled,
		LeaderElectionID:              "terminal.platform-mesh.io",
		LeaderElectionConfig:          leaderCfg,
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("unable to start manager")
	}

	runtimeClient, err := client.New(runtimeCfg, client.Options{Scheme: scheme})
	if err != nil {
		log.Fatal().Err(err).Msg("unable to create runtime cluster client")
	}

	terminalReconciler := controller.NewTerminalReconciler(log, mgr, operatorCfg, runtimeClient)
	if err := terminalReconciler.SetupWithManager(mgr, defaultCfg, log); err != nil {
		log.Fatal().Err(err).Str("controller", "Terminal").Msg("unable to create controller")
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Fatal().Err(err).Msg("unable to set up health check")
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Fatal().Err(err).Msg("unable to set up ready check")
	}

	log.Info().Msg("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Fatal().Err(err).Msg("problem running manager")
	}
}

// loadKcpConfig loads the kubeconfig for kcp.
// If kubeconfigPath is provided, it loads from that file.
// Otherwise, it falls back to in-cluster config.
func loadKcpConfig(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath != "" {
		log.Info().Str("kubeconfig", kubeconfigPath).Msg("loading kcp config from file")
		return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	}

	log.Info().Msg("loading kcp config from in-cluster")
	return rest.InClusterConfig()
}
