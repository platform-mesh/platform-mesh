//go:build integration

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

package kcpmigration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"go.platform-mesh.io/golang-commons/logger"
	migrationcrds "go.platform-mesh.io/kcp-migration-operator/config/crd"
	"go.platform-mesh.io/kcp-migration-operator/pkg/app"
	"go.platform-mesh.io/tests/framework/fixtures"
	"go.platform-mesh.io/tests/framework/kcp"
	"go.platform-mesh.io/tests/framework/scheme"
	"go.platform-mesh.io/tests/framework/wait"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/kcp-dev/multicluster-provider/envtest"
	"github.com/kcp-dev/sdk/apis/core"
)

const (
	timeout  = 10 * time.Second
	interval = 250 * time.Millisecond

	kcpMigrationCRD = "kcpmigrations.migration.platform-mesh.io"
)

// sourceWorkspace creates the workspace the operator watches (it stands in for
// the kube-apiserver the operator runs against), installs the operator CRDs
// into it and returns its rest config. A kcp workspace serves ConfigMaps and
// Namespaces, so the plain controller-runtime managers work unchanged.
func sourceWorkspace(t testing.TB, env *kcp.Env, s *runtime.Scheme) (*rest.Config, logicalcluster.Path) {
	t.Helper()

	_, path := env.Workspace(t, core.RootCluster.Path(), envtest.WithNamePrefix("kcpmigration"))
	cl := env.Client(t, path, s)
	fixtures.ApplyFS(t, cl, migrationcrds.Resources, "*.yaml")
	wait.CRDEstablished(t, cl, kcpMigrationCRD)
	return env.RESTConfig(path), path
}

// targetWorkspaces creates the kcp tree the sync mode writes into: a parent
// workspace with a child named after the source namespace ("default"), so the
// sync expression "<parent>:{{ .Source.metadata.namespace }}" resolves to it.
func targetWorkspaces(t testing.TB, env *kcp.Env) (parent, target logicalcluster.Path) {
	t.Helper()

	_, parent = env.Workspace(t, core.RootCluster.Path(), envtest.WithNamePrefix("kcpmigration-target"))
	_, target = env.Workspace(t, parent, envtest.WithName(sourceNamespace))
	return parent, target
}

// startManager builds a manager against cfg, lets setup register controllers,
// starts it and waits for its cache to sync. The manager is stopped in t.Cleanup.
func startManager(t testing.TB, cfg *rest.Config, s *runtime.Scheme, setup func(ctrl.Manager) error) {
	t.Helper()

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:  s,
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	require.NoError(t, err)
	require.NoError(t, setup(mgr))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- mgr.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("manager exited with error: %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Errorf("manager did not stop")
		}
	})

	syncCtx, syncCancel := context.WithTimeout(ctx, timeout)
	defer syncCancel()
	require.True(t, mgr.GetCache().WaitForCacheSync(syncCtx), "manager cache did not sync")
}

// IntegrationSuite runs the operator mode (KCPMigration controller) and the
// sync mode (a ConfigMap sync controller writing into kcp) against one source
// workspace.
type IntegrationSuite struct {
	suite.Suite

	ctx context.Context
	// client reaches the source workspace.
	client ctrlruntimeclient.Client
	// targetClient reaches the kcp workspace the sync mode writes into.
	targetClient ctrlruntimeclient.Client
}

func TestIntegration(t *testing.T) {
	suite.Run(t, new(IntegrationSuite))
}

func (s *IntegrationSuite) SetupSuite() {
	t := s.T()
	s.ctx = t.Context()
	env := kcp.Server(t)

	// The test clients also need apiextensions (CRD install); the managers run
	// with the operator's own scheme, like cmd/ does.
	testScheme := scheme.New(t, app.AddToScheme)
	opScheme, err := app.NewScheme()
	s.Require().NoError(err)

	cfg, path := sourceWorkspace(t, env, testScheme)
	s.client = env.Client(t, path, testScheme)

	targetParent, targetPath := targetWorkspaces(t, env)
	s.targetClient = env.Client(t, targetPath, testScheme)

	logCfg := logger.DefaultConfig()
	logCfg.Level = "debug"
	log, err := logger.New(logCfg)
	s.Require().NoError(err)

	startManager(t, cfg, opScheme, func(mgr ctrl.Manager) error {
		return app.SetupOperatorControllers(mgr, operatorConfig(), app.Deps{Log: log})
	})
	// The sync mode writes through the front-proxy into the workspace its
	// expression names; KCPConfig is the unscoped admin config, the factory
	// appends /clusters/<path>.
	startManager(t, cfg, opScheme, func(mgr ctrl.Manager) error {
		return app.SetupSyncControllers(mgr, syncConfigs(targetParent), app.Deps{Log: log, KCPConfig: env.Config()})
	})
}
