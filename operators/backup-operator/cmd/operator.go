/*
Copyright 2026.

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

	"github.com/kcp-dev/multicluster-provider/apiexport"
	pathaware "github.com/kcp-dev/multicluster-provider/path-aware"
	"github.com/spf13/cobra"
	platformmeshcontext "go.platform-mesh.io/golang-commons/context"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"go.platform-mesh.io/backup-operator/pkg/controller"
	"go.platform-mesh.io/backup-operator/pkg/topology/projector"
)

var operatorCmd = &cobra.Command{
	Use:   "operator",
	Short: "run the backup-operator controller manager",
	Run:   RunController,
}

func RunController(_ *cobra.Command, _ []string) { // coverage-ignore
	ctrl.SetLogger(log.ComponentLogger("controller-runtime").Logr())

	ctx, _, shutdown := platformmeshcontext.StartContext(log, operatorCfg, defaultCfg.ShutdownTimeout)
	defer shutdown()

	restCfg := ctrl.GetConfigOrDie()

	provider, err := pathaware.New(restCfg, operatorCfg.Kcp.ApiExportEndpointSliceName, apiexport.Options{
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

	if err := projector.New(mgr.GetLocalManager().GetClient(), operatorCfg.Namespace).EnsureConfigMap(ctx); err != nil {
		log.Fatal().Err(err).Msg("unable to ensure topology schema ConfigMap")
	}

	if err := controller.NewPlatformBackupReconciler(mgr).SetupWithManager(mgr); err != nil {
		log.Fatal().Err(err).Str("controller", "PlatformBackup").Msg("unable to create controller")
	}

	if err := controller.NewPlatformRestoreReconciler(mgr).SetupWithManager(mgr); err != nil {
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
