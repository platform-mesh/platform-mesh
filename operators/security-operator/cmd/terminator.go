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
	"crypto/tls"
	"os"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/controller"
	"go.platform-mesh.io/security-operator/internal/fga"
	"go.platform-mesh.io/security-operator/internal/predicates"
	"go.platform-mesh.io/security-operator/internal/terminatingworkspaces"

	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
)

var terminatorCmd = &cobra.Command{
	Use:   "terminator",
	Short: "FGA terminator for account workspaces",
	RunE: func(cmd *cobra.Command, args []string) error {
		kcpCfg, err := getKubeconfigFromPath(terminatorCfg.KCP.Kubeconfig)
		if err != nil {
			log.Error().Err(err).Msg("unable to get KCP kubeconfig")
			os.Exit(1)
		}

		mgrOpts := ctrl.Options{
			Scheme:                 scheme,
			LeaderElection:         defaultCfg.LeaderElectionEnabled,
			LeaderElectionID:       "security-operator-terminator.platform-mesh.io",
			HealthProbeBindAddress: defaultCfg.HealthProbeBindAddress,
			Metrics: server.Options{
				BindAddress: defaultCfg.Metrics.BindAddress,
				TLSOpts: []func(*tls.Config){
					func(c *tls.Config) {
						log.Info().Msg("disabling http/2")
						c.NextProtos = []string{"http/1.1"}
					},
				},
			},
		}
		if defaultCfg.LeaderElectionEnabled {
			inClusterCfg, err := rest.InClusterConfig()
			if err != nil {
				log.Error().Err(err).Msg("unable to create in-cluster config")
				return err
			}
			mgrOpts.LeaderElectionConfig = inClusterCfg
		}

		provider, err := terminatingworkspaces.New(kcpCfg, terminatorCfg.WorkspaceTypeName,
			terminatingworkspaces.Options{
				Scheme: mgrOpts.Scheme,
			},
		)
		if err != nil {
			log.Error().Err(err).Msg("Failed to create terminatingworkspaces provider")
			os.Exit(1)
		}

		mgr, err := mcmanager.New(kcpCfg, provider, mgrOpts)
		if err != nil {
			log.Error().Err(err).Msg("Failed to create manager")
			os.Exit(1)
		}

		conn, err := grpc.NewClient(terminatorCfg.FGA.Target, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Error().Err(err).Msg("unable to create grpc client")
			os.Exit(1)
		}
		defer func() { _ = conn.Close() }()
		fgaClient := openfgav1.NewOpenFGAServiceClient(conn)
		storeIDGetter := fga.NewCachingStoreIDGetter(
			fgaClient,
			terminatorCfg.FGA.StoreIDCacheTTL,
			cmd.Context(),
			log,
		)
		kcpClientGetter := iclient.NewConfigSchemeKCPClientGetter(mgr.GetLocalManager().GetConfig(), mgr.GetLocalManager().GetScheme())

		alcReconciler, err := controller.NewAccountLogicalClusterController(log, terminatorCfg, fgaClient, storeIDGetter, mgr, kcpClientGetter, controller.ControllerOptions{
			Name:           "AccountLogicalClusterTerminator",
			TerminatorName: terminatorCfg.TerminatorName(),
		})
		if err != nil {
			log.Error().Err(err).Msg("unable to create AccountLogicalCluster reconciler")
			os.Exit(1)
		}
		if err := alcReconciler.SetupWithManager(mgr, defaultCfg,
			predicate.Not(predicates.LogicalClusterIsAccountTypeOrg()),
		); err != nil {
			log.Error().Err(err).Msg("Unable to create AccountLogicalClusterTerminator")
			os.Exit(1)
		}

		if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
			log.Error().Err(err).Msg("unable to set up health check")
			os.Exit(1)
		}
		if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
			log.Error().Err(err).Msg("unable to set up ready check")
			os.Exit(1)
		}

		setupLog.Info("starting manager")

		return mgr.Start(ctrl.SetupSignalHandler())
	},
}
