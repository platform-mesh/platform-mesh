/*
Copyright 2024.

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
	"os"

	"k8s.io/client-go/tools/clientcmd"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.platform-mesh.io/extension-manager-operator/internal/controller"
	platformmeshcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/traces"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

var operatorCmd = &cobra.Command{
	Use:   "operator",
	Short: "operator to reconcile ContentConfiguration",
	Run:   RunController,
}

func RunController(_ *cobra.Command, _ []string) { // coverage-ignore
	log.Info().Msg("Starting operator")
	ctrl.SetLogger(log.ComponentLogger("controller-runtime").Logr())

	ctx, _, shutdown := platformmeshcontext.StartContext(log, operatorCfg, defaultCfg.ShutdownTimeout)
	defer shutdown()

	var err error
	var traceProviderShutdown func(ctx context.Context) error
	if defaultCfg.Tracing.Enabled {
		traceProviderShutdown, err = traces.InitProvider(ctx, defaultCfg.Tracing.Collector)
		if err != nil {
			log.Fatal().Err(err).Msg("unable to start gRPC-Sidecar TracerProvider")
		}
	} else {
		traceProviderShutdown, err = traces.InitLocalProvider(ctx, defaultCfg.Tracing.Collector, false)
		if err != nil {
			log.Fatal().Err(err).Msg("unable to start local TracerProvider")
		}
	}
	defer func() {
		if err := traceProviderShutdown(ctx); err != nil {
			log.Fatal().Err(err).Msg("failed to shutdown TracerProvider")
		}
	}()

	kubeconfigPath := os.Getenv("KUBECONFIG")
	var reconcileCfg *rest.Config
	if kubeconfigPath != "" {
		reconcileCfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			log.Fatal().Err(err).Msg("unable to load kubeconfig from KUBECONFIG env var")
		}
	} else {
		log.Info().Msg("KUBECONFIG not set, using GetConfigOrDie()")
		reconcileCfg = ctrl.GetConfigOrDie()
	}
	reconcileCfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return otelhttp.NewTransport(rt)
	})

	var leaderElectionCfg *rest.Config
	if defaultCfg.LeaderElectionEnabled {
		leaderElectionCfg, err = rest.InClusterConfig()
		if err != nil {
			log.Fatal().Err(err).Msg("unable to get in-cluster config for leader election")
		}
	}

	var mcProvider multicluster.Provider
	if operatorCfg.KCPAPIExportEndpointSliceName != "" {
		var p *apiexport.Provider
		p, err = apiexport.New(reconcileCfg, operatorCfg.KCPAPIExportEndpointSliceName, apiexport.Options{
			Scheme: scheme,
		})
		if err != nil {
			log.Fatal().Err(err).Msg("unable to construct APIExportProvider")
		}
		log.Info().Msgf("Using APIExportProvider with EndpointSlice %s", operatorCfg.KCPAPIExportEndpointSliceName)

		mcProvider = p
	}

	mgr, err := mcmanager.New(reconcileCfg, mcProvider, manager.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: defaultCfg.Metrics.BindAddress,
			TLSOpts: []func(*tls.Config){
				func(c *tls.Config) { c.NextProtos = []string{"http/1.1"} },
			},
		},
		BaseContext:                   func() context.Context { return ctx },
		HealthProbeBindAddress:        defaultCfg.HealthProbeBindAddress,
		LeaderElection:                defaultCfg.LeaderElectionEnabled,
		LeaderElectionID:              "eengiex3.platform-mesh.io",
		LeaderElectionReleaseOnCancel: true,
		LeaderElectionConfig:          leaderElectionCfg,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("unable to set up overall controller manager")
	}

	contentConfigurationReconciler := controller.NewContentConfigurationReconciler(log, mgr, *operatorCfg)
	if err := contentConfigurationReconciler.SetupWithManager(mgr, defaultCfg, log); err != nil {
		log.Fatal().Err(err).Str("controller", "ContentConfiguration").Msg("unable to create controller")
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Fatal().Err(err).Msg("unable to set up health check")
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Fatal().Err(err).Msg("unable to set up ready check")
	}

	startCtx := ctrl.SetupSignalHandler()
	if err := mgr.Start(startCtx); err != nil {
		log.Fatal().Err(err).Msg("problem running manager")
	}
}
