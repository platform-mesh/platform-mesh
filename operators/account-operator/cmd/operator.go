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
	"net/url"
	"strings"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	platformmeshcontext "github.com/platform-mesh/golang-commons/context"
	"github.com/platform-mesh/golang-commons/traces"
	"github.com/spf13/cobra"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/controller"
)

var operatorCmd = &cobra.Command{
	Use:   "operator",
	Short: "operator to reconcile Accounts",
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
			log.Fatal().Err(err).Msg("unable to start gRPC-Sidecar TracerProvider")
		}
	}
	defer func() {
		if err := traceShutdown(ctx); err != nil {
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

	var leaderCfg *rest.Config
	if defaultCfg.LeaderElection.Enabled {
		leaderCfg, err = rest.InClusterConfig()
		if err != nil {
			log.Fatal().Err(err).Msg("unable to get in-cluster config")
		}
	}
	provider, err := apiexport.New(restCfg, operatorCfg.Kcp.ApiExportEndpointSliceName, apiexport.Options{
		Log:    &ctrl.Log,
		Scheme: scheme,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("creating APIExport provider")
	}

	mgr, err := mcmanager.New(restCfg, provider, mcmanager.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   defaultCfg.Metrics.BindAddress,
			SecureServing: defaultCfg.Metrics.Secure,
			TLSOpts:       tlsOpts,
		},
		BaseContext:                   func() context.Context { return ctx },
		WebhookServer:                 webhookServer,
		HealthProbeBindAddress:        defaultCfg.HealthProbeBindAddress,
		LeaderElection:                defaultCfg.LeaderElection.Enabled,
		LeaderElectionID:              "8c290d9a.platform-mesh.io",
		LeaderElectionConfig:          leaderCfg,
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("unable to start manager")
	}

	orgsClient, err := buildOrgsClient(mgr.GetLocalManager())
	if err != nil {
		log.Fatal().Err(err).Msg("unable to create orgs client")
	}

	accountReconciler := controller.NewAccountReconciler(log, mgr, operatorCfg, orgsClient)
	if err := accountReconciler.SetupWithManager(mgr, defaultCfg, log); err != nil {
		log.Fatal().Err(err).Str("controller", "Account").Msg("unable to create controller")
	}

	if operatorCfg.Controllers.AccountInfo.Enabled {
		accountInfoReconciler := controller.NewAccountInfoReconciler(log, mgr, operatorCfg)
		if err := accountInfoReconciler.SetupWithManager(mgr, defaultCfg, log); err != nil {
			log.Fatal().Err(err).Str("controller", "AccountInfo").Msg("unable to create controller")
		}
	}

	if operatorCfg.Webhooks.Enabled {
		var denyList []string
		if operatorCfg.Webhooks.DenyList != "" {
			denyList = strings.Split(operatorCfg.Webhooks.DenyList, ",")
			for i, item := range denyList {
				denyList[i] = strings.TrimSpace(item)
			}
		}

		accountTypeAllowList := []v1alpha1.AccountType{v1alpha1.AccountTypeOrg}
		for _, additionalType := range operatorCfg.Webhooks.AdditionalAccountTypes {
			accountTypeAllowList = append(accountTypeAllowList, v1alpha1.AccountType(additionalType))
		}

		log.Info().Strs("deniedNames", denyList).Msg("webhooks are enabled")
		if err := v1alpha1.SetupAccountWebhookWithManager(mgr.GetLocalManager(), denyList, accountTypeAllowList); err != nil {
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

func buildOrgsClient(mgr ctrl.Manager) (client.Client, error) {
	cfg := rest.CopyConfig(mgr.GetConfig())

	parsed, err := url.Parse(cfg.Host)
	if err != nil {
		log.Error().Err(err).Msg("unable to parse host")
		return nil, err
	}

	parsed.Path = "/clusters/root:orgs"

	cfg.Host = parsed.String()

	return client.New(cfg, client.Options{
		Scheme: scheme,
	})
}
