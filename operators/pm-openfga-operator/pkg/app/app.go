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

// Package app wires the pm-openfga-operator controllers onto a manager. It is
// the public surface through which cmd/ and the integration tests set up the
// operator; the reconcilers themselves stay in internal/.
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	platformeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/pm-openfga-operator/internal/client"
	"go.platform-mesh.io/pm-openfga-operator/internal/controller"
	"go.platform-mesh.io/pm-openfga-operator/pkg/config"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	multiprovider "sigs.k8s.io/multicluster-runtime/providers/multi"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	pathaware "github.com/kcp-dev/multicluster-provider/path-aware"
	"github.com/kcp-dev/multicluster-provider/pkg/provider"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

// NewScheme returns the scheme the pm-openfga-operator managers use.
func NewScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(kcptenancyv1alpha1.AddToScheme(s))
	utilruntime.Must(pmcorev1alpha1.AddToScheme(s))
	utilruntime.Must(pmprovidersv1alpha1.AddToScheme(s))
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(s))
	utilruntime.Must(kcpapisv1alpha2.AddToScheme(s))
	utilruntime.Must(kcpcorev1alpha1.AddToScheme(s))
	return s
}

// Deps are the dependencies of the model-generator controllers beyond the
// manager and the operator config.
type Deps struct {
	// Log is the operator logger.
	Log *logger.Logger
	// CommonConfig is the shared service config (concurrency, debug label).
	// NewDefaultConfig is used when nil.
	CommonConfig *platformeshconfig.CommonServiceConfig
	// CoreProvider is the provider behind the core.platform-mesh.io APIExport.
	// Its lister finds the APIBindings affected by a ProviderPermissions change.
	CoreProvider *provider.Provider
}

// SetupModelGeneratorControllers registers the model-generator controllers
// (the APIBinding reconciler that generates AuthorizationModels) on mgr.
//
// mgr must run on a multi provider that holds the core, providers, fga and
// provider-fga APIExport providers under config.CoreProviderName,
// config.ProvidersProviderName, config.FgaProviderName and
// config.ProviderFgaProviderName.
func SetupModelGeneratorControllers(mgr mcmanager.Manager, cfg *config.Config, deps Deps) error {
	if deps.Log == nil {
		return errors.New("deps.Log must be set")
	}
	if deps.CoreProvider == nil {
		return errors.New("deps.CoreProvider must be set")
	}
	commonCfg := deps.CommonConfig
	if commonCfg == nil {
		commonCfg = platformeshconfig.NewDefaultConfig()
	}

	providerLister := iclient.NewProviderLister(deps.CoreProvider)
	if err := controller.NewAPIBindingReconciler(deps.Log, mgr, providerLister, cfg).SetupWithManager(mgr, commonCfg); err != nil {
		return fmt.Errorf("setting up apibinding controller: %w", err)
	}
	return nil
}

// RunModelGenerator builds the model-generator manager against the kcp behind
// restCfg, sets up its controllers and runs it until ctx is done.
func RunModelGenerator(ctx context.Context, restCfg *rest.Config, cfg *config.Config, deps Deps) error {
	commonCfg := deps.CommonConfig
	if commonCfg == nil {
		commonCfg = platformeshconfig.NewDefaultConfig()
		deps.CommonConfig = commonCfg
	}
	log := deps.Log
	if log == nil {
		return errors.New("deps.Log must be set")
	}

	scheme := NewScheme()
	mgrOpts := manager.Options{
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
		LeaderElectionID:       "pm-openfga-operator-generator.platform-mesh.io",
		BaseContext:            func() context.Context { return ctx },
	}
	if commonCfg.LeaderElectionEnabled {
		inClusterCfg, err := rest.InClusterConfig()
		if err != nil {
			return fmt.Errorf("creating in-cluster config: %w", err)
		}
		mgrOpts.LeaderElectionConfig = inClusterCfg
	}

	multiProv := multiprovider.New(multiprovider.Options{})
	var coreProvider *pathaware.Provider
	for _, p := range []struct {
		name, endpointSlice string
	}{
		{config.ProvidersProviderName, cfg.APIExportEndpointSlices.ProvidersPlatformMeshIO},
		{config.CoreProviderName, cfg.APIExportEndpointSlices.CorePlatformMeshIO},
		{config.FgaProviderName, cfg.APIExportEndpointSlices.FgaPlatformMeshIO},
		{config.ProviderFgaProviderName, cfg.APIExportEndpointSlices.ProviderFgaPlatformMeshIO},
	} {
		prov, err := pathaware.New(restCfg, p.endpointSlice, apiexport.Options{Scheme: scheme})
		if err != nil {
			return fmt.Errorf("creating %s apiexport provider: %w", p.name, err)
		}
		if err := multiProv.AddProvider(p.name, prov); err != nil {
			return fmt.Errorf("adding %s provider: %w", p.name, err)
		}
		if p.name == config.CoreProviderName {
			coreProvider = prov
		}
	}

	mgr, err := mcmanager.New(restCfg, multiProv, mgrOpts)
	if err != nil {
		return fmt.Errorf("creating manager: %w", err)
	}

	deps.CoreProvider = coreProvider.Provider.Provider
	if err := SetupModelGeneratorControllers(mgr, cfg, deps); err != nil {
		return err
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("setting up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("setting up ready check: %w", err)
	}

	log.Info().Msg("starting manager")
	return mgr.Start(ctx)
}
