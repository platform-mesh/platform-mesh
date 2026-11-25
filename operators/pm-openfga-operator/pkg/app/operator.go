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
	"go.platform-mesh.io/pm-openfga-operator/internal/features"
	"go.platform-mesh.io/pm-openfga-operator/internal/fga"
	"go.platform-mesh.io/pm-openfga-operator/internal/legacy"
	"go.platform-mesh.io/pm-openfga-operator/internal/predicates"
	"go.platform-mesh.io/pm-openfga-operator/pkg/config"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	multiprovider "sigs.k8s.io/multicluster-runtime/providers/multi"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	pathaware "github.com/kcp-dev/multicluster-provider/path-aware"
)

// operatorCoreProviderName is the name of the core.platform-mesh.io provider
// in the fga role.
const operatorCoreProviderName = "core-platform-mesh-io"

// RunOperator runs the fga role until ctx is done: the Store and
// AuthorizationModel controllers that write to OpenFGA, and the controllers
// that keep the organization Stores and the account tuples of initialized
// workspaces current.
//
// kcpCfg is a kcp rest config; cfg.KCP.Workspace names the workspace in it
// that holds the core, openfga and provider-openfga APIExportEndpointSlices
// (with LegacyAPIs also the fga and provider-fga ones) named in
// cfg.APIExportEndpointSlices (empty: the one kcpCfg points at). cfg.CoreModulePath
// must be readable unless cfg.Initializer.WorkspaceInitializerEnabled is off.
func RunOperator(ctx context.Context, kcpCfg *rest.Config, cfg *config.Config, deps Deps) error {
	if err := prepare(kcpCfg, cfg, &deps); err != nil {
		return err
	}
	kcpCfg, err := kcpconfig.ForWorkspace(kcpCfg, cfg.KCP.Workspace)
	if err != nil {
		return err
	}
	log := deps.Log
	commonCfg := deps.CommonConfig

	scheme := NewScheme()
	mgrOpts, err := managerOptions(ctx, scheme, operatorLeaderElectionID, deps)
	if err != nil {
		return err
	}

	coreProvider, err := pathaware.New(kcpCfg, cfg.APIExportEndpointSlices.CorePlatformMeshIO, apiexport.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("creating core apiexport provider: %w", err)
	}
	// Stores live under openfga.platform-mesh.io and AuthorizationModels under
	// provider-openfga.platform-mesh.io, so the Store and AuthorizationModel
	// controllers engage only with clusters visible through those exports.
	openfgaProvider, err := pathaware.New(kcpCfg, cfg.APIExportEndpointSlices.OpenfgaPlatformMeshIO, apiexport.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("creating openfga apiexport provider: %w", err)
	}
	providerOpenfgaProvider, err := pathaware.New(kcpCfg, cfg.APIExportEndpointSlices.ProviderOpenfgaPlatformMeshIO, apiexport.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("creating provider-openfga apiexport provider: %w", err)
	}

	multiProv := multiprovider.New(multiprovider.Options{Separator: config.ProviderSeparator})
	if err := multiProv.AddProvider(operatorCoreProviderName, coreProvider); err != nil {
		return fmt.Errorf("adding core provider: %w", err)
	}
	if err := multiProv.AddProvider(config.OpenfgaProviderName, openfgaProvider); err != nil {
		return fmt.Errorf("adding openfga provider: %w", err)
	}
	if err := multiProv.AddProvider(config.ProviderOpenfgaProviderName, providerOpenfgaProvider); err != nil {
		return fmt.Errorf("adding provider-openfga provider: %w", err)
	}

	authorizationModelLister := legacy.AuthorizationModelLister{
		Current: iclient.NewProviderLister(providerOpenfgaProvider.Provider.Provider),
	}
	kinds := []controller.Kinds{controller.CurrentKinds()}
	if cfg.FeatureEnabled(features.LegacyAPIs) {
		// The deprecated core.platform-mesh.io kinds are served through
		// fga.platform-mesh.io and provider-fga.platform-mesh.io.
		fgaProvider, err := pathaware.New(kcpCfg, cfg.APIExportEndpointSlices.FgaPlatformMeshIO, apiexport.Options{Scheme: scheme})
		if err != nil {
			return fmt.Errorf("creating fga apiexport provider: %w", err)
		}
		providerFgaProvider, err := pathaware.New(kcpCfg, cfg.APIExportEndpointSlices.ProviderFgaPlatformMeshIO, apiexport.Options{Scheme: scheme})
		if err != nil {
			return fmt.Errorf("creating provider-fga apiexport provider: %w", err)
		}
		if err := multiProv.AddProvider(config.FgaProviderName, fgaProvider); err != nil {
			return fmt.Errorf("adding fga provider: %w", err)
		}
		if err := multiProv.AddProvider(config.ProviderFgaProviderName, providerFgaProvider); err != nil {
			return fmt.Errorf("adding provider-fga provider: %w", err)
		}
		authorizationModelLister.Legacy = iclient.NewProviderLister(providerFgaProvider.Provider.Provider)
		kinds = append(kinds, controller.LegacyKinds())
	}

	mgr, err := mcmanager.New(kcpCfg, multiProv, mgrOpts)
	if err != nil {
		return fmt.Errorf("creating manager: %w", err)
	}

	fgaClient, closeFGAClient, err := openFGAClient(cfg, deps)
	if err != nil {
		return err
	}
	defer closeFGAClient()
	storeIDGetter := fga.NewCachingStoreIDGetter(ctx, fgaClient, cfg.FGA.StoreIDCacheTTL, log)

	for _, k := range kinds {
		if err := controller.NewStoreReconciler(ctx, log, fgaClient, mgr, cfg, authorizationModelLister, k).
			SetupWithManager(mgr, commonCfg); err != nil {
			return fmt.Errorf("setting up store%s controller: %w", k.Suffix, err)
		}
		if err := controller.NewAuthorizationModelReconciler(ctx, log, fgaClient, mgr, k).
			SetupWithManager(mgr, commonCfg); err != nil {
			return fmt.Errorf("setting up authorizationmodel%s controller: %w", k.Suffix, err)
		}
	}

	// AccountInfos and LogicalClusters are only served through core.platform-mesh.io.
	coreEngageOpts := controller.WithClustersFromNamedProvider(operatorCoreProviderName, config.ProviderSeparator)
	kcpClientGetter := iclient.NewConfigSchemeKCPClientGetter(kcpCfg, scheme)

	orgReconciler, err := controller.NewOrgLogicalClusterController(log, kcpClientGetter, *cfg, mgr, fgaClient, storeIDGetter, controller.ControllerOptions{
		Name: orgReconcilerName,
	})
	if err != nil {
		return fmt.Errorf("creating org logicalcluster reconciler: %w", err)
	}
	if err := orgReconciler.WithEngageOptions(coreEngageOpts).SetupWithManager(mgr, commonCfg,
		predicates.LogicalClusterIsAccountTypeOrg(),
		predicates.HasInitializerPredicate(cfg.InitializerName()),
	); err != nil {
		return fmt.Errorf("setting up org logicalcluster controller: %w", err)
	}

	accountReconciler, err := controller.NewAccountLogicalClusterController(log, *cfg, fgaClient, storeIDGetter, mgr, kcpClientGetter, controller.ControllerOptions{
		Name: accountReconcilerName,
	})
	if err != nil {
		return fmt.Errorf("creating account logicalcluster reconciler: %w", err)
	}
	if err := accountReconciler.WithEngageOptions(coreEngageOpts).SetupWithManager(mgr, commonCfg,
		predicate.Not(predicates.LogicalClusterIsAccountTypeOrg()),
		predicates.HasInitializerPredicate(cfg.InitializerName()),
	); err != nil {
		return fmt.Errorf("setting up account logicalcluster controller: %w", err)
	}
	if err := controller.NewAccountInfoReconciler(log, mgr).SetupWithManager(mgr, commonCfg, coreEngageOpts); err != nil {
		return fmt.Errorf("setting up accountinfo controller: %w", err)
	}

	return start(ctx, mgr, deps)
}
