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

	platformeshcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/sentry"
	"go.platform-mesh.io/pm-keycloak-operator/internal/features"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/app"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/config"

	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcmultiprovider "sigs.k8s.io/multicluster-runtime/providers/multi"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	pathaware "github.com/kcp-dev/multicluster-provider/path-aware"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var scheme = app.NewScheme()

var operatorCmd = &cobra.Command{
	Use:   "operator",
	Short: "Reconciles IdentityProviderConfigurations, Invites and the organization identity setup against Keycloak",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctrl.SetLogger(log.ComponentLogger("controller-runtime").Logr())

		ctx, _, shutdown := platformeshcontext.StartContext(log, defaultCfg, defaultCfg.ShutdownTimeout)
		defer shutdown()

		restCfg, err := getKubeconfigFromPath(operatorCfg.KCP.Kubeconfig)
		if err != nil {
			log.Error().Err(err).Msg("unable to get kcp kubeconfig")
			return err
		}

		if defaultCfg.Sentry.Dsn != "" {
			err := sentry.Start(ctx,
				defaultCfg.Sentry.Dsn, defaultCfg.Environment, defaultCfg.Region,
				defaultCfg.Image.Name, defaultCfg.Image.Tag,
			)
			if err != nil {
				log.Fatal().Err(err).Msg("Sentry init failed")
			}

			defer platformeshcontext.Recover(log)
		}

		disableHTTP2 := func(c *tls.Config) {
			log.Info().Msg("disabling http/2")
			c.NextProtos = []string{"http/1.1"}
		}
		mgrOpts := ctrl.Options{
			Scheme: scheme,
			Metrics: metricsserver.Options{
				BindAddress: defaultCfg.Metrics.BindAddress,
				TLSOpts:     []func(*tls.Config){disableHTTP2},
			},
			HealthProbeBindAddress: defaultCfg.HealthProbeBindAddress,
			LeaderElection:         defaultCfg.LeaderElectionEnabled,
			LeaderElectionID:       "pm-keycloak-operator.platform-mesh.io",
			BaseContext:            func() context.Context { return ctx },
			WebhookServer: webhook.NewServer(webhook.Options{
				TLSOpts: []func(*tls.Config){disableHTTP2},
				CertDir: operatorCfg.Webhooks.CertDir,
				Port:    operatorCfg.Webhooks.Port,
			}),
		}
		if defaultCfg.LeaderElectionEnabled {
			inClusterCfg, err := rest.InClusterConfig()
			if err != nil {
				log.Error().Err(err).Msg("unable to create in-cluster config")
				return err
			}
			mgrOpts.LeaderElectionConfig = inClusterCfg
		}

		legacyAPIs := operatorCfg.FeatureEnabled(features.LegacyAPIs)

		// IdentityProviderConfigurations, Invites and LogicalClusters are served
		// through keycloak.platform-mesh.io, AccountInfos (and the legacy Invites)
		// through core.platform-mesh.io, the legacy IdentityProviderConfigurations
		// through system.platform-mesh.io.
		providers := map[string]string{
			config.KeycloakProviderName: operatorCfg.APIExportEndpointSlices.KeycloakPlatformMeshIO,
			config.CoreProviderName:     operatorCfg.APIExportEndpointSlices.CorePlatformMeshIO,
		}
		if legacyAPIs {
			providers[config.SystemProviderName] = operatorCfg.APIExportEndpointSlices.SystemPlatformMeshIO
		}
		multiProvider := mcmultiprovider.New(mcmultiprovider.Options{Separator: config.ProviderSeparator})

		mgr, err := mcmanager.New(restCfg, multiProvider, mgrOpts)
		if err != nil {
			setupLog.Error(err, "Failed to create manager")
			return err
		}
		var keycloakProvider *pathaware.Provider
		for name, slice := range providers {
			provider, err := pathaware.New(restCfg, slice, apiexport.Options{Scheme: scheme})
			if err != nil {
				setupLog.Error(err, "unable to construct cluster provider", "provider", name)
				return err
			}
			if err := multiProvider.AddProvider(name, provider); err != nil {
				setupLog.Error(err, "unable to add cluster provider", "provider", name)
				return err
			}
			if name == config.KeycloakProviderName {
				keycloakProvider = provider
			}
		}

		k8sCfg := ctrl.GetConfigOrDie()
		runtimeClient, err := ctrlruntimeclient.New(k8sCfg, ctrlruntimeclient.Options{Scheme: scheme})
		if err != nil {
			log.Error().Err(err).Msg("Failed to create in cluster client")
			return err
		}

		if err := app.SetupOperatorControllers(ctx, mgr, &operatorCfg, app.Deps{
			Log:              log,
			CommonConfig:     defaultCfg,
			KeycloakProvider: keycloakProvider.Provider.Provider,
			RuntimeClient:    runtimeClient,
			RestConfig:       restCfg,
		}); err != nil {
			log.Error().Err(err).Msg("unable to set up controllers")
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
			log.Error().Err(err).Msg("problem running manager")
			return err
		}
		return nil
	},
}
