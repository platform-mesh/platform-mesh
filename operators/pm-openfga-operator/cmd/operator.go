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

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	platformeshcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/sentry"
	iclient "go.platform-mesh.io/pm-openfga-operator/internal/client"
	"go.platform-mesh.io/pm-openfga-operator/internal/controller"
	fga2 "go.platform-mesh.io/pm-openfga-operator/internal/fga"
	"go.platform-mesh.io/pm-openfga-operator/internal/predicates"
	"go.platform-mesh.io/pm-openfga-operator/pkg/app"
	"go.platform-mesh.io/pm-openfga-operator/pkg/config"

	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcmultiprovider "sigs.k8s.io/multicluster-runtime/providers/multi"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	pathaware "github.com/kcp-dev/multicluster-provider/path-aware"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var (
	scheme = app.NewScheme()
)

const (
	coreProviderName = "core-platform-mesh-io"
	// providerSeparator must match mcmultiprovider.Options.Separator below.
	providerSeparator = "#"
)

var operatorCmd = &cobra.Command{
	Use: "fga",
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
			LeaderElection:         defaultCfg.LeaderElectionEnabled,
			LeaderElectionID:       "pm-openfga-operator.platform-mesh.io",
			BaseContext:            func() context.Context { return ctx },
		}
		if defaultCfg.LeaderElectionEnabled {
			inClusterCfg, err := rest.InClusterConfig()
			if err != nil {
				log.Error().Err(err).Msg("unable to create in-cluster config")
				return err
			}
			mgrOpts.LeaderElectionConfig = inClusterCfg
		}

		if mgrOpts.Scheme == nil {
			log.Error().Err(fmt.Errorf("scheme should not be nil")).Msg("scheme should not be nil")
			return fmt.Errorf("scheme should not be nil")
		}

		provider, err := pathaware.New(restCfg, operatorCfg.APIExportEndpointSlices.CorePlatformMeshIO, apiexport.Options{
			Scheme: mgrOpts.Scheme,
		})
		if err != nil {
			setupLog.Error(err, "unable to construct core cluster provider")
			return err
		}

		// Stores live under fga.platform-mesh.io and AuthorizationModels under provider-fga.platform-mesh.io,
		// so the Store and AuthorizationModel controllers engage only with clusters visible through those exports.
		fgaProvider, err := pathaware.New(restCfg, operatorCfg.APIExportEndpointSlices.FgaPlatformMeshIO, apiexport.Options{
			Scheme: mgrOpts.Scheme,
		})
		if err != nil {
			setupLog.Error(err, "unable to construct fga cluster provider")
			return err
		}
		providerFgaProvider, err := pathaware.New(restCfg, operatorCfg.APIExportEndpointSlices.ProviderFgaPlatformMeshIO, apiexport.Options{
			Scheme: mgrOpts.Scheme,
		})
		if err != nil {
			setupLog.Error(err, "unable to construct provider-fga cluster provider")
			return err
		}

		multiProvider := mcmultiprovider.New(mcmultiprovider.Options{Separator: providerSeparator})

		mgr, err := mcmanager.New(restCfg, multiProvider, mgrOpts)
		if err != nil {
			setupLog.Error(err, "Failed to create manager")
			return err
		}

		if err := multiProvider.AddProvider(coreProviderName, provider); err != nil {
			setupLog.Error(err, "unable to add core cluster provider")
			return err
		}
		if err := multiProvider.AddProvider(config.FgaProviderName, fgaProvider); err != nil {
			setupLog.Error(err, "unable to add fga cluster provider")
			return err
		}
		if err := multiProvider.AddProvider(config.ProviderFgaProviderName, providerFgaProvider); err != nil {
			setupLog.Error(err, "unable to add provider-fga cluster provider")
			return err
		}

		conn, err := grpc.NewClient(operatorCfg.FGA.Target, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Error().Err(err).Msg("unable to create grpc client")
			return err
		}
		defer func() { _ = conn.Close() }()

		fga := openfgav1.NewOpenFGAServiceClient(conn)
		storeIDGetter := fga2.NewCachingStoreIDGetter(
			ctx,
			fga,
			operatorCfg.FGA.StoreIDCacheTTL,
			log,
		)

		authorizationModelLister := iclient.NewProviderLister(providerFgaProvider.Provider.Provider)
		fgaEngageOpts := controller.WithClustersFromNamedProvider(config.FgaProviderName, providerSeparator)
		providerFgaEngageOpts := controller.WithClustersFromNamedProvider(config.ProviderFgaProviderName, providerSeparator)
		// Invites, AccountInfos and LogicalClusters are only served through core.platform-mesh.io.
		coreEngageOpts := controller.WithClustersFromNamedProvider(coreProviderName, providerSeparator)

		if err = controller.NewStoreReconciler(ctx, log, fga, mgr, &operatorCfg, authorizationModelLister, fgaEngageOpts, providerFgaEngageOpts).
			SetupWithManager(mgr, defaultCfg); err != nil {
			log.Error().Err(err).Str("controller", "store").Msg("unable to create controller")
			return err
		}
		if err = controller.
			NewAuthorizationModelReconciler(ctx, log, fga, mgr, providerFgaEngageOpts).
			SetupWithManager(mgr, defaultCfg); err != nil {
			log.Error().Err(err).Str("controller", "authorizationmodel").Msg("unable to create controller")
			return err
		}

		kcpClientGetterWithConfig := iclient.NewConfigSchemeKCPClientGetter(restCfg, scheme)

		orgReconciler, err := controller.NewOrgLogicalClusterController(log, kcpClientGetterWithConfig, operatorCfg, mgr, fga, storeIDGetter, controller.ControllerOptions{
			Name: "OrgLogicalClusterReconciler",
		})
		if err != nil {
			log.Error().Err(err).Str("controller", "logicalcluster").Msg("unable to create initializer")
			return err
		}
		if err = orgReconciler.WithEngageOptions(coreEngageOpts).SetupWithManager(mgr, defaultCfg,
			predicates.LogicalClusterIsAccountTypeOrg(),
			predicates.HasInitializerPredicate(operatorCfg.InitializerName()),
		); err != nil {
			log.Error().Err(err).Str("controller", "logicalcluster").Msg("unable to create controller")
			return err
		}

		alcReconciler, err := controller.NewAccountLogicalClusterController(log, operatorCfg, fga, storeIDGetter, mgr, kcpClientGetterWithConfig, controller.ControllerOptions{
			Name: "AccountLogicalClusterReconciler",
		})
		if err != nil {
			log.Error().Err(err).Str("controller", "accounttypelogicalcluster").Msg("unable to create reconciler")
			return err
		}
		if err = alcReconciler.WithEngageOptions(coreEngageOpts).SetupWithManager(mgr, defaultCfg,
			predicate.Not(predicates.LogicalClusterIsAccountTypeOrg()),
			predicates.HasInitializerPredicate(operatorCfg.InitializerName()),
		); err != nil {
			log.Error().Err(err).Str("controller", "accounttypelogicalcluster").Msg("unable to create controller")
			return err
		}
		if err = controller.NewAccountInfoReconciler(log, mgr).SetupWithManager(mgr, defaultCfg, coreEngageOpts); err != nil {
			log.Error().Err(err).Str("controller", "accountinfo").Msg("unable to create controller")
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
