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
	"strings"

	apisv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	platformmeshcontext "github.com/platform-mesh/golang-commons/context"
	"github.com/platform-mesh/golang-commons/traces"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/kcp"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/controller"
)

var operatorCmd = &cobra.Command{
	Use:   "operator",
	Short: "operator to reconcile Accounts",
	Run:   RunController,
}

var (
	enableLeaderElection bool
	secureMetrics        bool
	enableHTTP2          bool
)

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
	if !enableHTTP2 {
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

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
		CertDir: operatorCfg.Webhooks.CertDir,
		Port:    operatorCfg.Webhooks.Port,
	})
	restCfg := ctrl.GetConfigOrDie()
	restCfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return otelhttp.NewTransport(rt)
	})
	opts := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   defaultCfg.Metrics.BindAddress,
			SecureServing: secureMetrics,
			TLSOpts:       tlsOpts,
		},
		BaseContext:                   func() context.Context { return ctx },
		WebhookServer:                 webhookServer,
		HealthProbeBindAddress:        defaultCfg.HealthProbeBindAddress,
		LeaderElection:                enableLeaderElection,
		LeaderElectionID:              "8c290d9a.platform-mesh.io",
		LeaderElectionConfig:          restCfg,
		LeaderElectionReleaseOnCancel: true,
	}
	var mgr ctrl.Manager
	mgrConfig := rest.CopyConfig(restCfg)
	if len(operatorCfg.Kcp.ApiExportEndpointSliceName) > 0 {
		// Lookup API Endpointslice
		kclient, err := client.New(restCfg, client.Options{
			Scheme: scheme,
		})
		if err != nil {
			log.Fatal().Err(err).Msg("unable to create client")
		}
		es := &apisv1alpha1.APIExportEndpointSlice{}
		err = kclient.Get(ctx, client.ObjectKey{Name: operatorCfg.Kcp.ApiExportEndpointSliceName}, es)
		if err != nil {
			log.Fatal().Err(err).Msg("unable to create client")
		}
		if len(es.Status.APIExportEndpoints) == 0 {
			log.Fatal().Msg("no APIExportEndpoints found")
		}
		log.Info().Str("host", es.Status.APIExportEndpoints[0].URL).Msg("using host")
		mgrConfig.Host = es.Status.APIExportEndpoints[0].URL
	}
	mgr, err = kcp.NewClusterAwareManager(mgrConfig, opts)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to start manager")
	}

	var fgaClient openfgav1.OpenFGAServiceClient
	if operatorCfg.Subroutines.FGA.Enabled {
		log.Debug().Str("GrpcAddr", operatorCfg.Subroutines.FGA.GrpcAddr).Msg("Creating FGA Client")
		conn, err := grpc.NewClient(operatorCfg.Subroutines.FGA.GrpcAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		)
		if err != nil {

			log.Fatal().Err(err).Msg("error when creating the grpc client")
		}
		log.Debug().Msg("FGA client created")

		fgaClient = openfgav1.NewOpenFGAServiceClient(conn)
	}

	accountReconciler := controller.NewAccountReconciler(log, mgr, operatorCfg, fgaClient)
	if err := accountReconciler.SetupWithManager(mgr, defaultCfg, log); err != nil {
		log.Fatal().Err(err).Str("controller", "Account").Msg("unable to create controller")
	}

	if operatorCfg.Webhooks.Enabled {
		var denyList []string
		if operatorCfg.Webhooks.DenyList != "" {
			denyList = strings.Split(operatorCfg.Webhooks.DenyList, ",")
			for i, item := range denyList {
				denyList[i] = strings.TrimSpace(item)
			}
		}
		log.Info().Strs("deniedNames", denyList).Msg("webhooks are enabled")
		if err := v1alpha1.SetupAccountWebhookWithManager(mgr, denyList); err != nil {
			log.Fatal().Err(err).Str("webhook", "Account").Msg("unable to create webhook")
		}
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
