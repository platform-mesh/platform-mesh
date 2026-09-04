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

	"github.com/spf13/cobra"

	platformmeshcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/traces"
	"go.platform-mesh.io/search-operator/internal/controller"
	"go.platform-mesh.io/search-operator/internal/opensearch"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/kcp-dev/multicluster-provider/apiexport"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var operatorCmd = &cobra.Command{
	Use:   "operator",
	Short: "run the search-operator controller manager",
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

	if defaultCfg.Tracing.Enabled {
		traceShutdown, traceErr := traces.InitProvider(ctx, defaultCfg.Tracing.Collector)
		if traceErr != nil {
			log.Fatal().Err(traceErr).Msg("unable to start gRPC-Sidecar TracerProvider")
		}
		defer func() {
			if shutdownErr := traceShutdown(ctx); shutdownErr != nil {
				log.Fatal().Err(shutdownErr).Msg("failed to shutdown TracerProvider")
			}
		}()
	}

	kcpCfg, err := clientcmd.BuildConfigFromFlags("", operatorCfg.KCPKubeconfig)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to load kcp kubeconfig")
	}

	provider, err := apiexport.New(kcpCfg, operatorCfg.APIExportEndpointSliceName, apiexport.Options{
		Scheme: scheme,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("unable to create cluster provider")
	}

	mgr, err := mcmanager.New(kcpCfg, provider, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: defaultCfg.Metrics.BindAddress,
			TLSOpts:     tlsOpts,
		},
		HealthProbeBindAddress: defaultCfg.HealthProbeBindAddress,
		LeaderElection:         defaultCfg.LeaderElectionEnabled,
		LeaderElectionID:       "search-operator.platform-mesh.io",
	})
	if err != nil {
		log.Fatal().Err(err).Msg("unable to create manager")
	}

	cfg, err := opensearch.NewConfigFromEnv()
	if err != nil {
		log.Fatal().Err(err).Msg("OpenSearch not configured")
	}

	osClient, err := opensearch.NewClientFromConfig(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to create OpenSearch client")
	}

	if err := osClient.Ping(context.Background()); err != nil {
		log.Fatal().Err(err).Msg("unable to connect to OpenSearch")
	}
	log.Info().Msg("OpenSearch client connected successfully")

	if err := controller.NewSearchIndexReconciler(
		log, mgr, osClient, operatorCfg.OpenSearchIndexNamePrefix, operatorCfg.OpenSearchSemanticModelID,
	).SetupWithManager(mgr, defaultCfg.MaxConcurrentReconciles); err != nil {
		log.Fatal().Err(err).Str("controller", "SearchIndex").Msg("unable to create controller")
	}

	for _, gvk := range operatorCfg.SearchableResources {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk.GroupVersionKind)
		idxReconciler, err := controller.NewIndexableResource(
			log, operatorCfg, mgr, osClient, operatorCfg.APIExportEndpointSliceName, obj)
		if err != nil {
			log.Fatal().Err(err).Msg("unable to create IndexableResource reconciler")
		}
		if err := idxReconciler.SetupWithManager(mgr, defaultCfg.MaxConcurrentReconciles, obj); err != nil {
			log.Fatal().Err(err).Str("controller", "IndexableResource").Msg("unable to create controller")
		}
	}

	apiBindingReconciler, err := controller.NewAPIBindingReconciler(log, mgr, operatorCfg)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to create APIBinding reconciler")
	}
	if err := apiBindingReconciler.SetupWithManager(mgr, defaultCfg.MaxConcurrentReconciles); err != nil {
		log.Fatal().Err(err).Str("controller", "APIBinding").Msg("unable to create controller")
	}

	//+kubebuilder:scaffold:builder

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
