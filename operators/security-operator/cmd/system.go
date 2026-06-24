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

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	platformeshcontext "go.platform-mesh.io/golang-commons/context"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/controller"
	"go.platform-mesh.io/security-operator/internal/fga"

	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	multiprovider "sigs.k8s.io/multicluster-runtime/providers/multi"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	pathaware "github.com/kcp-dev/multicluster-provider/path-aware"
)

var systemCmd = &cobra.Command{
	Use:   "system",
	Short: "System controllers for system.platform-mesh.io apiexport resources",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctrl.SetLogger(log.ComponentLogger("controller-runtime").Logr())

		ctx, _, shutdown := platformeshcontext.StartContext(log, defaultCfg, defaultCfg.ShutdownTimeout)
		defer shutdown()

		restCfg, err := getKubeconfigFromPath(systemCfg.KCP.Kubeconfig)
		if err != nil {
			log.Error().Err(err).Msg("unable to get KCP kubeconfig")
			return err
		}

		opts := ctrl.Options{
			Scheme: scheme,
			Metrics: metricsserver.Options{
				BindAddress: defaultCfg.Metrics.BindAddress,
				TLSOpts: []func(*tls.Config){
					func(c *tls.Config) {
						c.NextProtos = []string{"http/1.1"}
					},
				},
			},
			HealthProbeBindAddress: defaultCfg.HealthProbeBindAddress,
			LeaderElection:         defaultCfg.LeaderElectionEnabled,
			LeaderElectionID:       "security-operator-system.platform-mesh.io",
			BaseContext:            func() context.Context { return ctx },
		}

		if defaultCfg.LeaderElectionEnabled {
			inClusterCfg, err := rest.InClusterConfig()
			if err != nil {
				setupLog.Error(err, "unable to get in-cluster config for leader election")
				return err
			}
			opts.LeaderElectionConfig = inClusterCfg
		}

		systemProvider, err := pathaware.New(restCfg, systemCfg.APIExportEndpointSlices.SystemPlatformMeshIO, apiexport.Options{
			Scheme: scheme,
		})
		if err != nil {
			setupLog.Error(err, "unable to create apiexport provider")
			return err
		}

		coreProvider, err := pathaware.New(restCfg, systemCfg.APIExportEndpointSlices.CorePlatformMeshIO, apiexport.Options{
			Scheme: scheme,
		})
		if err != nil {
			setupLog.Error(err, "unable to create core apiexport provider")
			return err
		}

		multiProv := multiprovider.New(multiprovider.Options{})
		if err := multiProv.AddProvider(config.SystemProviderName, systemProvider); err != nil {
			return err
		}
		if err := multiProv.AddProvider(config.CoreProviderName, coreProvider); err != nil {
			return err
		}

		mgr, err := mcmanager.New(restCfg, multiProv, opts)
		if err != nil {
			setupLog.Error(err, "unable to create manager")
			return err
		}

		conn, err := grpc.NewClient(systemCfg.FGA.Target, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Error().Err(err).Msg("unable to create grpc client")
			return err
		}

		fgaClient := openfgav1.NewOpenFGAServiceClient(conn)

		storeIDGetter := fga.NewCachingStoreIDGetter(
			fgaClient,
			systemCfg.FGA.StoreIDCacheTTL,
			ctx,
			log,
		)

		kcpClientGetter := iclient.NewManagerKCPClientGetter(mgr, coreProvider.Provider.Provider)

		idpReconciler, err := controller.NewIdentityProviderConfigurationReconciler(ctx, mgr, kcpClientGetter, &systemCfg, log)
		if err != nil {
			log.Error().Err(err).Str("controller", "identityprovider").Msg("unable to create reconciler")
			return err
		}
		if err := idpReconciler.SetupWithManager(mgr, defaultCfg, log); err != nil {
			log.Error().Err(err).Str("controller", "identityprovider").Msg("unable to create controller")
			return err
		}

		providerLister := iclient.NewProviderLister(coreProvider.Provider.Provider)

		if err = controller.NewAPIExportPolicyReconciler(log, fgaClient, mgr, providerLister, &systemCfg, storeIDGetter, kcpClientGetter).SetupWithManager(mgr, defaultCfg); err != nil {
			log.Error().Err(err).Str("controller", "apiexportpolicy").Msg("unable to create controller")
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

		setupLog.Info("starting system manager")

		return mgr.Start(ctx)
	},
}
