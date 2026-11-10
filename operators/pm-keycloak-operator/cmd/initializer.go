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

	"github.com/spf13/cobra"

	iclient "go.platform-mesh.io/pm-keycloak-operator/internal/client"
	"go.platform-mesh.io/pm-keycloak-operator/internal/controller"
	"go.platform-mesh.io/pm-keycloak-operator/internal/predicates"

	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/kcp-dev/multicluster-provider/initializingworkspaces"
)

var initializerCmd = &cobra.Command{
	Use:   "initializer",
	Short: "Keycloak initializer for organization workspaces",
	RunE: func(cmd *cobra.Command, args []string) error {
		restCfg, err := getKubeconfigFromPath(initializerCfg.KCP.Kubeconfig)
		if err != nil {
			log.Error().Err(err).Msg("unable to get kcp kubeconfig")
			os.Exit(1)
		}

		mgrOpts := ctrl.Options{
			Scheme:                 scheme,
			LeaderElection:         defaultCfg.LeaderElectionEnabled,
			LeaderElectionID:       "pm-keycloak-operator-initializer.platform-mesh.io",
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

		provider, err := initializingworkspaces.New(restCfg, initializerCfg.WorkspaceTypeName,
			initializingworkspaces.Options{
				Scheme: mgrOpts.Scheme,
			},
		)
		if err != nil {
			log.Error().Err(err).Msg("unable to construct cluster provider")
			os.Exit(1)
		}

		mgr, err := mcmanager.New(restCfg, provider, mgrOpts)
		if err != nil {
			setupLog.Error(err, "Failed to create manager")
			os.Exit(1)
		}

		k8sCfg := ctrl.GetConfigOrDie()

		runtimeClient, err := ctrlruntimeclient.New(k8sCfg, ctrlruntimeclient.Options{Scheme: scheme})
		if err != nil {
			log.Error().Err(err).Msg("Failed to create in cluster client")
			os.Exit(1)
		}

		if initializerCfg.IDP.AdditionalRedirectURLs == nil {
			initializerCfg.IDP.AdditionalRedirectURLs = []string{}
		}

		kcpClientGetter := iclient.NewConfigSchemeKCPClientGetter(restCfg, scheme)
		orgReconciler, err := controller.NewOrgLogicalClusterController(log, kcpClientGetter, initializerCfg, runtimeClient, mgr, controller.ControllerOptions{
			Name:            "OrgLogicalClusterInitializer",
			InitializerName: initializerCfg.InitializerName(),
		})
		if err != nil {
			setupLog.Error(err, "unable to create LogicalCluster initializer")
			os.Exit(1)
		}
		if err := orgReconciler.SetupWithManager(mgr, defaultCfg, predicates.LogicalClusterIsAccountTypeOrg()); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "LogicalCluster")
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

		setupLog.Info("starting manager")

		return mgr.Start(ctrl.SetupSignalHandler())
	},
}
