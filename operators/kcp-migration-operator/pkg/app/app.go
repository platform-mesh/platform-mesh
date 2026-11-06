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

// Package app wires the kcp-migration-operator controllers. It is the public
// entry point used by cmd/ and by black-box integration tests; the reconcilers
// themselves stay in internal/.
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"

	pmmigrationv1alpha1 "go.platform-mesh.io/apis/migration/v1alpha1"
	pmconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/kcp-migration-operator/internal/controller"
	"go.platform-mesh.io/kcp-migration-operator/internal/kcp"
	"go.platform-mesh.io/kcp-migration-operator/pkg/config"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// LeaderElectionID is the leader election lease name of the operator mode.
const LeaderElectionID = "kcp-migration-operator.platform-mesh.io"

// Deps carries the dependencies injected into the controllers.
type Deps struct {
	// Log is the operator logger. Required.
	Log *logger.Logger
	// KCPConfig is the rest config of the kcp the sync controllers write to.
	// When nil, syncing to kcp is disabled and sources are only reconciled.
	KCPConfig *rest.Config
}

func (d Deps) validate() error {
	if d.Log == nil {
		return errors.New("deps.Log is required")
	}
	return nil
}

// AddToScheme registers every API type the operator works with.
func AddToScheme(s *runtime.Scheme) error {
	if err := clientgoscheme.AddToScheme(s); err != nil {
		return err
	}
	return pmmigrationv1alpha1.AddToScheme(s)
}

// NewScheme returns a scheme populated by AddToScheme.
func NewScheme() (*runtime.Scheme, error) {
	s := runtime.NewScheme()
	if err := AddToScheme(s); err != nil {
		return nil, err
	}
	return s, nil
}

// SetupOperatorControllers registers the controllers of the operator mode
// (the KCPMigration reconciler) with mgr.
func SetupOperatorControllers(mgr ctrl.Manager, cfg *config.OperatorConfig, deps Deps) error {
	if err := deps.validate(); err != nil {
		return err
	}
	if cfg == nil {
		return errors.New("operator config is required")
	}
	r := controller.NewKCPMigrationReconciler(mgr.GetClient(), mgr.GetScheme(), deps.Log, cfg)
	if err := r.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create KCPMigration controller: %w", err)
	}
	return nil
}

// SetupSyncControllers registers one sync controller per configuration with mgr.
// Each controller watches the configured source kind and, when deps.KCPConfig is
// set, writes the transformed resource into the target kcp workspace.
func SetupSyncControllers(mgr ctrl.Manager, cfgs []*config.SyncConfig, deps Deps) error {
	if err := deps.validate(); err != nil {
		return err
	}
	if len(cfgs) == 0 {
		return errors.New("at least one sync config is required")
	}

	var workspaceFactory kcp.WorkspaceClientFactory
	if deps.KCPConfig != nil {
		var err error
		workspaceFactory, err = kcp.NewWorkspaceClientFactoryFromConfig(deps.KCPConfig, mgr.GetScheme())
		if err != nil {
			return fmt.Errorf("failed to create kcp workspace client factory: %w", err)
		}
	} else {
		deps.Log.Warn().Msg("no kcp config provided, sync to kcp will be disabled")
	}

	for _, cfg := range cfgs {
		deps.Log.Info().
			Str("name", cfg.MigrationName).
			Str("sourceAPIVersion", cfg.Source.APIVersion).
			Str("sourceKind", cfg.Source.Kind).
			Str("sourceNamespace", cfg.Source.Namespace).
			Strs("sourceLabelSelectors", cfg.Source.LabelSelectors).
			Int("maxWorkers", cfg.Performance.MaxWorkers).
			Msg("setting up sync controller for resource")

		c := controller.NewSyncController(mgr.GetClient(), deps.Log, cfg, workspaceFactory)
		if err := c.SetupWithManager(mgr); err != nil {
			return fmt.Errorf("unable to setup sync controller %q: %w", cfg.MigrationName, err)
		}
	}
	return nil
}

func addHealthChecks(mgr ctrl.Manager) error {
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}
	return nil
}

// RunOperator builds a manager against restCfg, registers the operator-mode
// controllers and runs it until ctx is cancelled.
func RunOperator(ctx context.Context, restCfg *rest.Config, cfg *config.OperatorConfig, common *pmconfig.CommonServiceConfig, deps Deps) error { // coverage-ignore
	if err := deps.validate(); err != nil {
		return err
	}
	s, err := NewScheme()
	if err != nil {
		return err
	}

	var tlsOpts []func(*tls.Config)
	if !common.EnableHTTP2 {
		tlsOpts = append(tlsOpts, func(c *tls.Config) {
			deps.Log.Info().Msg("disabling http/2")
			c.NextProtos = []string{"http/1.1"}
		})
	}

	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme: s,
		Metrics: metricsserver.Options{
			BindAddress:   common.Metrics.BindAddress,
			SecureServing: common.Metrics.Secure,
			TLSOpts:       tlsOpts,
		},
		HealthProbeBindAddress:        common.HealthProbeBindAddress,
		LeaderElection:                common.LeaderElectionEnabled,
		LeaderElectionID:              LeaderElectionID,
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		return fmt.Errorf("unable to create manager: %w", err)
	}
	if err := SetupOperatorControllers(mgr, cfg, deps); err != nil {
		return err
	}
	if err := addHealthChecks(mgr); err != nil {
		return err
	}

	deps.Log.Info().Msg("starting manager")
	return mgr.Start(ctx)
}

// RunSync builds a manager watching the source cluster at restCfg, registers a
// sync controller per configuration and runs it until ctx is cancelled.
func RunSync(ctx context.Context, restCfg *rest.Config, cfgs []*config.SyncConfig, deps Deps) error { // coverage-ignore
	if err := deps.validate(); err != nil {
		return err
	}
	s, err := NewScheme()
	if err != nil {
		return err
	}

	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme: s,
		Metrics: metricsserver.Options{
			BindAddress: ":9090",
		},
		HealthProbeBindAddress: ":8090",
		Logger:                 deps.Log.ComponentLogger("sync-controller-runtime").Logr(),
	})
	if err != nil {
		return fmt.Errorf("unable to create manager: %w", err)
	}
	if err := SetupSyncControllers(mgr, cfgs, deps); err != nil {
		return err
	}
	if err := addHealthChecks(mgr); err != nil {
		return err
	}

	deps.Log.Info().Int("resourceCount", len(cfgs)).Msg("starting sync manager")
	return mgr.Start(ctx)
}
