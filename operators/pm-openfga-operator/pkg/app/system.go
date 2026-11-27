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
	"go.platform-mesh.io/pm-openfga-operator/pkg/config"

	"k8s.io/client-go/rest"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	multiprovider "sigs.k8s.io/multicluster-runtime/providers/multi"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	pathaware "github.com/kcp-dev/multicluster-provider/path-aware"
)

// RunSystem runs the system role until ctx is done: the APIExportPolicy
// controllers that write the bind tuples of the policies to OpenFGA.
//
// kcpCfg is a kcp rest config; cfg.KCP.Workspace names the workspace in it
// that holds the openfga and core APIExportEndpointSlices (with LegacyAPIs
// also the system one) named in cfg.APIExportEndpointSlices (empty: the one
// kcpCfg points at).
func RunSystem(ctx context.Context, kcpCfg *rest.Config, cfg *config.Config, deps Deps) error {
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
	mgrOpts, err := managerOptions(ctx, scheme, systemLeaderElectionID, deps)
	if err != nil {
		return err
	}

	openfgaProvider, err := pathaware.New(kcpCfg, cfg.APIExportEndpointSlices.OpenfgaPlatformMeshIO, apiexport.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("creating openfga apiexport provider: %w", err)
	}
	coreProvider, err := pathaware.New(kcpCfg, cfg.APIExportEndpointSlices.CorePlatformMeshIO, apiexport.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("creating core apiexport provider: %w", err)
	}

	multiProv := multiprovider.New(multiprovider.Options{Separator: config.ProviderSeparator})
	if err := multiProv.AddProvider(config.OpenfgaProviderName, openfgaProvider); err != nil {
		return fmt.Errorf("adding openfga provider: %w", err)
	}
	if err := multiProv.AddProvider(config.CoreProviderName, coreProvider); err != nil {
		return fmt.Errorf("adding core provider: %w", err)
	}
	kinds := []controller.Kinds{controller.CurrentKinds()}
	if cfg.FeatureEnabled(features.LegacyAPIs) {
		// Deprecated core.platform-mesh.io APIExportPolicies are served through system.platform-mesh.io.
		systemProvider, err := pathaware.New(kcpCfg, cfg.APIExportEndpointSlices.SystemPlatformMeshIO, apiexport.Options{Scheme: scheme})
		if err != nil {
			return fmt.Errorf("creating system apiexport provider: %w", err)
		}
		if err := multiProv.AddProvider(config.SystemProviderName, systemProvider); err != nil {
			return fmt.Errorf("adding system provider: %w", err)
		}
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

	kcpClientGetter := iclient.NewManagerKCPClientGetter(mgr, coreProvider.Provider.Provider)
	providerLister := iclient.NewProviderLister(coreProvider.Provider.Provider)

	for _, k := range kinds {
		if err := controller.NewAPIExportPolicyReconciler(log, fgaClient, mgr, providerLister, cfg, storeIDGetter, kcpClientGetter, k).
			SetupWithManager(mgr, commonCfg); err != nil {
			return fmt.Errorf("setting up apiexportpolicy%s controller: %w", k.Suffix, err)
		}
	}

	return start(ctx, mgr, deps)
}
