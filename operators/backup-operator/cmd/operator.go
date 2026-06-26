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

	"github.com/spf13/cobra"

	"go.platform-mesh.io/backup-operator/pkg/controller"
	"go.platform-mesh.io/backup-operator/pkg/topology/projector"
	platformmeshcontext "go.platform-mesh.io/golang-commons/context"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	singleprovider "sigs.k8s.io/multicluster-runtime/providers/single"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	pathaware "github.com/kcp-dev/multicluster-provider/path-aware"
)

var operatorCmd = &cobra.Command{
	Use:   "operator",
	Short: "run the backup-operator controller manager",
	Run:   RunController,
}

func RunController(_ *cobra.Command, _ []string) { // coverage-ignore
	ctrl.SetLogger(log.ComponentLogger("controller-runtime").Logr())

	if err := operatorCfg.Validate(); err != nil {
		log.Fatal().Err(err).Msg("invalid configuration")
	}

	ctx, _, shutdown := platformmeshcontext.StartContext(log, operatorCfg, defaultCfg.ShutdownTimeout)
	defer shutdown()

	restCfg := ctrl.GetConfigOrDie()

	var hostCfg *rest.Config
	if operatorCfg.Standalone {
		// In standalone mode KUBECONFIG (or in-cluster SA) points at the host cluster directly.
		hostCfg = restCfg
	} else {
		// In KCP mode KUBECONFIG points at the KCP workspace; the host cluster
		// is reached via the pod's service account.
		var err error
		hostCfg, err = rest.InClusterConfig()
		if err != nil {
			log.Fatal().Err(err).Msg("building in-cluster config for host client")
		}
	}

	hostClient, err := client.New(hostCfg, client.Options{Scheme: scheme})
	if err != nil {
		log.Fatal().Err(err).Msg("creating host cluster client")
	}

	var (
		standaloneCluster cluster.Cluster
		provider          multicluster.Provider
	)
	if operatorCfg.Standalone {
		standaloneCluster, err = cluster.New(restCfg, func(o *cluster.Options) { o.Scheme = scheme })
		if err != nil {
			log.Fatal().Err(err).Msg("creating standalone cluster")
		}
		provider = singleprovider.New(multicluster.ClusterName("standalone"), standaloneCluster)
	} else {
		provider, err = pathaware.New(restCfg, operatorCfg.Kcp.ApiExportEndpointSliceName, apiexport.Options{
			Log:    &ctrl.Log,
			Scheme: scheme,
		})
		if err != nil {
			log.Fatal().Err(err).Msg("creating APIExport provider")
		}
	}

	mgr, err := mcmanager.New(restCfg, provider, mcmanager.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   defaultCfg.Metrics.BindAddress,
			SecureServing: defaultCfg.Metrics.Secure,
		},
		BaseContext:                   func() context.Context { return ctx },
		HealthProbeBindAddress:        defaultCfg.HealthProbeBindAddress,
		LeaderElection:                defaultCfg.LeaderElectionEnabled,
		LeaderElectionID:              "backup-operator.platform-mesh.io",
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("unable to start manager")
	}

	// The standalone cluster's cache must be registered as a runnable so it starts
	// when the manager starts; without this informers never sync and no events arrive.
	// cluster.Cluster only implements controller-runtime's Runnable, not multicluster
	// Aware, so it must be added to the inner controller-runtime manager.
	if standaloneCluster != nil {
		if err := mgr.GetLocalManager().Add(standaloneCluster); err != nil {
			log.Fatal().Err(err).Msg("adding standalone cluster to manager")
		}
	}

	if err := projector.New(hostClient, operatorCfg.Namespace).EnsureConfigMap(ctx); err != nil {
		log.Fatal().Err(err).Msg("unable to ensure topology schema ConfigMap")
	}

	if err := controller.NewPlatformBackupReconciler(mgr, operatorCfg.Namespace).SetupWithManager(mgr); err != nil {
		log.Fatal().Err(err).Str("controller", "PlatformBackup").Msg("unable to create controller")
	}

	if err := controller.NewPlatformRestoreReconciler(mgr, operatorCfg.Namespace).SetupWithManager(mgr); err != nil {
		log.Fatal().Err(err).Str("controller", "PlatformRestore").Msg("unable to create controller")
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
