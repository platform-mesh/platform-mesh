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
	"fmt"

	"go.platform-mesh.io/golang-commons/kcpconfig"
	iclient "go.platform-mesh.io/pm-openfga-operator/internal/client"
	"go.platform-mesh.io/pm-openfga-operator/internal/controller"
	"go.platform-mesh.io/pm-openfga-operator/internal/fga"
	"go.platform-mesh.io/pm-openfga-operator/internal/predicates"
	"go.platform-mesh.io/pm-openfga-operator/internal/terminatingworkspaces"
	"go.platform-mesh.io/pm-openfga-operator/pkg/config"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	"github.com/kcp-dev/multicluster-provider/initializingworkspaces"
)

// RunInitializer runs the initializer role until ctx is done: it initializes
// the organization and account workspaces that carry the initializer of the
// cfg.WorkspaceTypeName WorkspaceType and removes the initializer from them.
//
// kcpCfg is a kcp rest config; the WorkspaceType is looked up in the workspace
// named by cfg.KCP.Workspace (empty: the one kcpCfg points at). cfg.CoreModulePath must be readable unless
// cfg.Initializer.WorkspaceInitializerEnabled is off.
func RunInitializer(ctx context.Context, kcpCfg *rest.Config, cfg *config.Config, deps Deps) error {
	if err := prepare(kcpCfg, cfg, &deps); err != nil {
		return err
	}
	kcpCfg, err := kcpconfig.ForWorkspace(kcpCfg, cfg.KCP.Workspace)
	if err != nil {
		return err
	}

	scheme := NewScheme()
	provider, err := initializingworkspaces.New(kcpCfg, cfg.WorkspaceTypeName, initializingworkspaces.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("creating initializingworkspaces provider: %w", err)
	}

	return runLogicalClusterControllers(ctx, kcpCfg, cfg, deps, provider, initializerLeaderElectionID,
		controller.ControllerOptions{
			Name:            orgInitializerName,
			InitializerName: cfg.InitializerName(),
		},
		controller.ControllerOptions{
			Name:            accountInitializerName,
			InitializerName: cfg.InitializerName(),
			TerminatorName:  cfg.TerminatorName(),
		},
	)
}

// RunTerminator runs the terminator role until ctx is done: it cleans up
// after the organization and account workspaces in deletion that carry the
// terminator of the cfg.WorkspaceTypeName WorkspaceType and removes the
// terminator from them.
//
// kcpCfg is a kcp rest config; the WorkspaceType is looked up in the workspace
// named by cfg.KCP.Workspace (empty: the one kcpCfg points at). cfg.CoreModulePath must be readable unless
// cfg.Initializer.WorkspaceInitializerEnabled is off.
func RunTerminator(ctx context.Context, kcpCfg *rest.Config, cfg *config.Config, deps Deps) error {
	if err := prepare(kcpCfg, cfg, &deps); err != nil {
		return err
	}
	kcpCfg, err := kcpconfig.ForWorkspace(kcpCfg, cfg.KCP.Workspace)
	if err != nil {
		return err
	}

	scheme := NewScheme()
	provider, err := terminatingworkspaces.New(kcpCfg, cfg.WorkspaceTypeName, terminatingworkspaces.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("creating terminatingworkspaces provider: %w", err)
	}

	return runLogicalClusterControllers(ctx, kcpCfg, cfg, deps, provider, terminatorLeaderElectionID,
		controller.ControllerOptions{
			Name:           orgTerminatorName,
			TerminatorName: cfg.TerminatorName(),
		},
		controller.ControllerOptions{
			Name:           accountTerminatorName,
			TerminatorName: cfg.TerminatorName(),
		},
	)
}

// runLogicalClusterControllers runs the organization and account
// LogicalCluster controllers on the workspaces of provider.
func runLogicalClusterControllers(
	ctx context.Context,
	kcpCfg *rest.Config,
	cfg *config.Config,
	deps Deps,
	provider multicluster.Provider,
	leaderElectionID string,
	orgOpts, accountOpts controller.ControllerOptions,
) error {
	log := deps.Log
	commonCfg := deps.CommonConfig

	scheme := NewScheme()
	mgrOpts, err := managerOptions(ctx, scheme, leaderElectionID, deps)
	if err != nil {
		return err
	}
	mgr, err := mcmanager.New(kcpCfg, provider, mgrOpts)
	if err != nil {
		return fmt.Errorf("creating manager: %w", err)
	}

	fgaClient, closeFGAClient, err := openFGAClient(cfg, deps)
	if err != nil {
		return err
	}
	defer closeFGAClient()
	storeIDGetter := fga.NewCachingStoreIDGetter(ctx, fgaClient, cfg.FGA.StoreIDCacheTTL, log)
	kcpClientGetter := iclient.NewConfigSchemeKCPClientGetter(kcpCfg, scheme)

	orgReconciler, err := controller.NewOrgLogicalClusterController(log, kcpClientGetter, *cfg, mgr, fgaClient, storeIDGetter, orgOpts)
	if err != nil {
		return fmt.Errorf("creating %s: %w", orgOpts.Name, err)
	}
	if err := orgReconciler.SetupWithManager(mgr, commonCfg, predicates.LogicalClusterIsAccountTypeOrg()); err != nil {
		return fmt.Errorf("setting up %s: %w", orgOpts.Name, err)
	}

	accountReconciler, err := controller.NewAccountLogicalClusterController(log, *cfg, fgaClient, storeIDGetter, mgr, kcpClientGetter, accountOpts)
	if err != nil {
		return fmt.Errorf("creating %s: %w", accountOpts.Name, err)
	}
	if err := accountReconciler.SetupWithManager(mgr, commonCfg, predicate.Not(predicates.LogicalClusterIsAccountTypeOrg())); err != nil {
		return fmt.Errorf("setting up %s: %w", accountOpts.Name, err)
	}

	return start(ctx, mgr, deps)
}
