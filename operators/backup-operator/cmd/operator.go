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

	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	singleprovider "sigs.k8s.io/multicluster-runtime/providers/single"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
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

	hostClient, err := ctrlruntimeclient.New(restCfg, ctrlruntimeclient.Options{Scheme: scheme})
	if err != nil {
		log.Fatal().Err(err).Msg("creating host cluster client")
	}

	standaloneCluster, err := cluster.New(restCfg, func(o *cluster.Options) { o.Scheme = scheme })
	if err != nil {
		log.Fatal().Err(err).Msg("creating cluster")
	}
	provider := singleprovider.New(multicluster.ClusterName("standalone"), standaloneCluster)

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

	// The cluster's cache must be registered as a runnable so it starts
	// when the manager starts; without this informers never sync and no events arrive.
	if err := mgr.GetLocalManager().Add(standaloneCluster); err != nil {
		log.Fatal().Err(err).Msg("adding cluster to manager")
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

	// Ensure the topology schema ConfigMap exists. This is done as a Runnable so
	// the health probe endpoints are registered before the first API server call,
	// avoiding CrashLoopBackOff on transient API server unavailability at startup.
	if err := mgr.GetLocalManager().Add(&projectorRunnable{client: hostClient, namespace: operatorCfg.Namespace}); err != nil {
		log.Fatal().Err(err).Msg("unable to register topology schema projector")
	}

	log.Info().Msg("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Fatal().Err(err).Msg("problem running manager")
	}
}

// projectorRunnable wraps projector.EnsureConfigMap as a controller-runtime Runnable
// so it runs after the manager has started (health probes registered) rather than
// blocking the operator from starting entirely when the API server is temporarily unavailable.
type projectorRunnable struct {
	client    ctrlruntimeclient.Client
	namespace string
}

func (r *projectorRunnable) Start(ctx context.Context) error {
	if err := projector.New(r.client, r.namespace).EnsureConfigMap(ctx); err != nil {
		// Non-fatal: the schema ConfigMap is used by clients that read topology
		// documents; the operator's core backup/restore logic can proceed without it.
		log.Warn().Err(err).Msg("unable to ensure topology schema ConfigMap; proceeding without it")
	}
	return nil
}
