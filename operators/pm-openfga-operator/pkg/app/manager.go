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
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	platformeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/pm-openfga-operator/pkg/config"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
)

// Leader election IDs of the roles. Each role runs on its own manager.
const (
	operatorLeaderElectionID       = "pm-openfga-operator.platform-mesh.io"
	modelGeneratorLeaderElectionID = "pm-openfga-operator-generator.platform-mesh.io"
	initializerLeaderElectionID    = "pm-openfga-operator-initializer.platform-mesh.io"
	terminatorLeaderElectionID     = "pm-openfga-operator-terminator.platform-mesh.io"
	systemLeaderElectionID         = "pm-openfga-operator-system.platform-mesh.io"
)

// Controller names of the LogicalCluster controllers. controller-runtime
// keeps the names of all controllers of a process and rejects a second
// controller of the same name, so the names of all roles are distinct:
//
//	fga:             store, authorizationmodel, accountinfo (with LegacyAPIs
//	                 also store-legacy, authorizationmodel-legacy) and the
//	                 two ...Reconciler names below
//	initializer:     the two ...Initializer names below
//	terminator:      the two ...Terminator names below
//	system:          apiexportpolicy (with LegacyAPIs also apiexportpolicy-legacy)
//	model-generator: apibinding
const (
	orgReconcilerName      = "OrgLogicalClusterReconciler"
	accountReconcilerName  = "AccountLogicalClusterReconciler"
	orgInitializerName     = "OrgLogicalClusterInitializer"
	accountInitializerName = "AccountLogicalClusterInitializer"
	orgTerminatorName      = "OrgLogicalClusterTerminator"
	accountTerminatorName  = "AccountLogicalClusterTerminator"
)

// prepare validates the arguments common to the Run functions and defaults deps.CommonConfig.
func prepare(kcpCfg *rest.Config, cfg *config.Config, deps *Deps) error {
	if kcpCfg == nil {
		return errors.New("kcp rest config must be set")
	}
	if cfg == nil {
		return errors.New("config must be set")
	}
	if deps.Log == nil {
		return errors.New("deps.Log must be set")
	}
	if deps.CommonConfig == nil {
		deps.CommonConfig = platformeshconfig.NewDefaultConfig()
	}
	return nil
}

// managerOptions returns the manager options of a role. The runtime cluster
// is only reached for leader election.
func managerOptions(ctx context.Context, scheme *runtime.Scheme, leaderElectionID string, deps Deps) (manager.Options, error) {
	log := deps.Log
	commonCfg := deps.CommonConfig
	opts := manager.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: commonCfg.Metrics.BindAddress,
			TLSOpts: []func(*tls.Config){
				func(c *tls.Config) {
					log.Info().Msg("disabling http/2")
					c.NextProtos = []string{"http/1.1"}
				},
			},
		},
		HealthProbeBindAddress: commonCfg.HealthProbeBindAddress,
		LeaderElection:         commonCfg.LeaderElectionEnabled,
		LeaderElectionID:       leaderElectionID,
		BaseContext:            func() context.Context { return ctx },
	}
	if deps.SkipNameValidation {
		opts.Controller.SkipNameValidation = ptr.To(true)
	}
	if commonCfg.LeaderElectionEnabled {
		inClusterCfg, err := rest.InClusterConfig()
		if err != nil {
			return opts, fmt.Errorf("creating in-cluster config: %w", err)
		}
		opts.LeaderElectionConfig = inClusterCfg
	}
	return opts, nil
}

// openFGAClient returns deps.FGAClient or, when that is nil, a client dialed
// to cfg.FGA.Target. The returned function closes the dialed connection.
func openFGAClient(cfg *config.Config, deps Deps) (openfgav1.OpenFGAServiceClient, func(), error) {
	if deps.FGAClient != nil {
		return deps.FGAClient, func() {}, nil
	}
	conn, err := grpc.NewClient(cfg.FGA.Target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, fmt.Errorf("creating openfga grpc client: %w", err)
	}
	return openfgav1.NewOpenFGAServiceClient(conn), func() { _ = conn.Close() }, nil
}

// start adds the health checks to mgr and runs it until ctx is done.
func start(ctx context.Context, mgr mcmanager.Manager, deps Deps) error {
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("setting up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("setting up ready check: %w", err)
	}

	deps.Log.Info().Msg("starting manager")
	return mgr.Start(ctx)
}
