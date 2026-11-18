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
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	pmcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/traces"
	"go.platform-mesh.io/platform-mesh-operator/internal/controller"
	"go.platform-mesh.io/platform-mesh-operator/internal/controller/providers"
	"go.platform-mesh.io/platform-mesh-operator/pkg/subroutines"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcmultiprovider "sigs.k8s.io/multicluster-runtime/providers/multi"

	mcapiexportprovider "github.com/kcp-dev/multicluster-provider/apiexport"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var operatorCmd = &cobra.Command{
	Use:   "operator",
	Short: "operator to setup platform-mesh",
	Run:   RunController,
}

const defaultWaitForKcpAdminKubeconfigPeriod = time.Second * 15

func RunController(_ *cobra.Command, _ []string) { // coverage-ignore
	var err error

	ctrl.SetLogger(log.ComponentLogger("controller-runtime").Logr())

	log.Info().Msg("Starting PlatformMesh Operator")
	defer log.Info().Msg("Shutting down PlatformMesh Operator")

	ctx, _, shutdown := pmcontext.StartContext(log, operatorCfg, defaultCfg.ShutdownTimeout)
	defer shutdown()

	disableHTTP2 := func(c *tls.Config) {
		log.Info().Msg("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !defaultCfg.EnableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	var providerShutdown func(ctx context.Context) error
	if defaultCfg.Tracing.Enabled {
		providerShutdown, err = traces.InitProvider(ctx, defaultCfg.Tracing.Collector)
		if err != nil {
			log.Fatal().Err(err).Msg("unable to start gRPC-Sidecar TracerProvider")
		}
	} else {
		providerShutdown, err = traces.InitLocalProvider(ctx, defaultCfg.Tracing.Collector, false)
		if err != nil {
			log.Fatal().Err(err).Msg("unable to start local TracerProvider")
		}
	}

	defer func() {
		if err := providerShutdown(ctx); err != nil {
			log.Fatal().Err(err).Msg("failed to shutdown TracerProvider")
		}
	}()

	log.Info().Msg("Starting manager")

	restCfg := ctrl.GetConfigOrDie()
	runtimeClient, err := ctrlruntimeclient.New(restCfg, ctrlruntimeclient.Options{Scheme: subroutines.GetClientScheme()})
	if err != nil {
		setupLog.Error(err, "unable to create PlatformMesh client")
		os.Exit(1)
	}
	if operatorCfg.RemoteRuntime.IsEnabled() {
		setupLog.Info("Remote PlatformMesh reconciliation enabled, kubeconfig: " + operatorCfg.RemoteRuntime.Kubeconfig)
		var err error
		runtimeClient, restCfg, err = subroutines.GetClientAndRestConfig(operatorCfg.RemoteRuntime.Kubeconfig)
		if err != nil {
			setupLog.Error(err, "unable to create PlatformMesh client")
			os.Exit(1)
		}
	}
	setupLog.Info(fmt.Sprintf("PlatformMesh Host: %s", restCfg.Host))
	restCfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return otelhttp.NewTransport(rt)
	})

	var leaderCfg *rest.Config
	if defaultCfg.LeaderElectionEnabled {
		leaderCfg, err = rest.InClusterConfig()
		if err != nil {
			log.Fatal().Err(err).Msg("unable to get in-cluster config")
		}
	}

	// NOTE: We are using MC multi provider. When adding new controllers, remember to
	// set your WithEngageWithLocalCluster and/or WithEngageWithProviderClusters ForOption
	// in For() [mcbuilder.TypedBuilder] appropriately, so that your reconciler receives
	// only events for the cluster(s) it is supposed to.
	mgr, err := mcmanager.New(restCfg, mcmultiprovider.New(mcmultiprovider.Options{}), mcmanager.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   defaultCfg.Metrics.BindAddress,
			SecureServing: defaultCfg.Metrics.Secure,
			TLSOpts:       tlsOpts,
		},
		BaseContext:                   func() context.Context { return ctx },
		HealthProbeBindAddress:        defaultCfg.HealthProbeBindAddress,
		LeaderElection:                defaultCfg.LeaderElectionEnabled,
		LeaderElectionID:              "81924e50.platform-mesh.org",
		LeaderElectionConfig:          leaderCfg,
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	log.Info().Msg("Manager successfully created")

	restCfgInfra := ctrl.GetConfigOrDie()
	restCfgInfra.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return otelhttp.NewTransport(rt)
	})
	clientInfra, err := ctrlruntimeclient.New(restCfgInfra, ctrlruntimeclient.Options{Scheme: subroutines.GetClientScheme()})
	if err != nil {
		setupLog.Error(err, "unable to create Infra client")
		os.Exit(1)
	}
	if operatorCfg.RemoteInfra.IsEnabled() {
		var infraErr error
		clientInfra, _, infraErr = subroutines.GetClientAndRestConfig(operatorCfg.RemoteInfra.Kubeconfig)
		if infraErr != nil {
			setupLog.Error(infraErr, "unable to create Infra client")
			os.Exit(1)
		}
	}
	imageVersionStore := subroutines.NewImageVersionStore()

	pmReconciler, err := controller.NewPlatformMeshReconciler(mgr, &operatorCfg, defaultCfg, clientInfra, imageVersionStore)
	if err != nil {
		setupLog.Error(err, "unable to create PlatformMesh reconciler")
		os.Exit(1)
	}
	if err := pmReconciler.SetupWithManager(mgr, defaultCfg); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PlatformMesh")
		os.Exit(1)
	}

	resourceReconciler, err := controller.NewResourceReconciler(mgr, &operatorCfg, clientInfra, imageVersionStore)
	if err != nil {
		setupLog.Error(err, "unable to create Resource reconciler")
		os.Exit(1)
	}
	if err := resourceReconciler.SetupWithManager(mgr, defaultCfg); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Resource")
		os.Exit(1)
	}

	managedProvidersReconciler, err := providers.NewManagedProviderReconciler(mgr, &operatorCfg, defaultCfg)
	if err != nil {
		setupLog.Error(err, "unable to create ManagedProvider reconciler")
		os.Exit(1)
	}
	if err := managedProvidersReconciler.SetupWithManager(mgr, defaultCfg); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ManagedProvider")
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

	go startProvidersOperator(ctx, runtimeClient, mgr)

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Fatal().Err(err).Msg("problem running manager")
	}
}

func startProvidersOperator(ctx context.Context, runtimeCl ctrlruntimeclient.Client, mgr mcmanager.Manager) {
	multiProvider := mgr.GetProvider().(*mcmultiprovider.Provider)

	// Wait until we have kcp up, with its kubeconfig available.
	var err error
	var kcpCfg *rest.Config
	err = wait.PollUntilContextCancel(ctx, defaultWaitForKcpAdminKubeconfigPeriod, true, func(ctx context.Context) (bool, error) {
		kcpCfg, err = buildKcpAdminConfigForWorkspace(runtimeCl, operatorCfg.Providers.ProvidersAPIExportEndpointSliceWorkspace)
		if err != nil {
			setupLog.Error(err, "trying to retrieve kcp admin kubeconfig")
		}
		return err == nil, nil
	})
	if err != nil {
		setupLog.Error(err, "failed retrieve kcp admin config")
		os.Exit(1)
	}
	kcpCfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return otelhttp.NewTransport(rt)
	})

	// Set up the mgr with apiexport provider.

	setupLog.Info("Starting apiexport-providers-platform-mesh provider")

	var apiexportProvider *mcapiexportprovider.Provider
	err = wait.PollUntilContextCancel(ctx, defaultWaitForKcpAdminKubeconfigPeriod, true, func(ctx context.Context) (bool, error) {
		apiexportProvider, err = mcapiexportprovider.New(
			kcpCfg,
			operatorCfg.Providers.ProvidersAPIExportEndpointSliceName,
			mcapiexportprovider.Options{
				Scheme: scheme,
			},
		)
		if err != nil {
			setupLog.Error(err, "failed to create APIExport provider", "apiexportendpointslice", fmt.Sprintf("%s:%s", operatorCfg.Providers.ProvidersAPIExportEndpointSliceWorkspace, operatorCfg.Providers.ProvidersAPIExportEndpointSliceName))
		}
		return err == nil, nil
	})
	if err != nil {
		setupLog.Error(err, "failed retrieve kcp admin config")
		os.Exit(1)
	}

	setupLog.Info("Successfully started apiexport-providers-platform-mesh provider")

	// Setup the reconciler against the mgr.
	providersReconciler, err := providers.NewProviderReconciler(mgr, &operatorCfg, defaultCfg, runtimeCl)
	if err != nil {
		setupLog.Error(err, "failed to create Providers reconciler")
		os.Exit(1)
	}
	err = providersReconciler.SetupWithManager(mgr, defaultCfg)
	if err != nil {
		setupLog.Error(err, "unable to setup ProviderReconciler with manager")
		os.Exit(1)
	}

	if err := multiProvider.AddProvider("apiexport-providers-platform-mesh", apiexportProvider); err != nil {
		setupLog.Error(err, "failed to add apiexport-providers-platform-mesh provider")
		os.Exit(1)
	}
}

func buildKcpAdminConfigForWorkspace(cl ctrlruntimeclient.Client, wsPath string) (*rest.Config, error) {
	kcpUrl := operatorCfg.KCP.Url
	if kcpUrl == "" {
		kcpUrl = fmt.Sprintf("https://%s-front-proxy.%s:%s", operatorCfg.KCP.FrontProxyName, operatorCfg.KCP.Namespace, operatorCfg.KCP.FrontProxyPort)
	}
	kcpUrl += fmt.Sprintf("/clusters/%s", wsPath)
	return subroutines.BuildKubeconfigFromConfig(cl, &operatorCfg.KCP, kcpUrl)
}
