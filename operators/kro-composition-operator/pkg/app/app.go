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

// Package app wires the kro-composition-operator: the scheme, the per-workspace engine, the
// multicluster controller and the manager. cmd/ and the tests build the operator through it,
// so neither needs the engine's internals.
package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	krov1alpha1 "github.com/kubernetes-sigs/kro/api/v1alpha1"
	"github.com/kubernetes-sigs/kro/pkg/dynamiccontroller"
	krofeatures "github.com/kubernetes-sigs/kro/pkg/features"
	krometrics "github.com/kubernetes-sigs/kro/pkg/metrics"

	"go.platform-mesh.io/kro-composition-operator/internal/engine"
	"go.platform-mesh.io/kro-composition-operator/internal/workspace"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

// Config is what cmd/ reads from its flags.
type Config struct {
	// APIExportEndpointSlice serves the kro.run RGD API.
	APIExportEndpointSlice string
	// ProviderWorkspace holds the kro.run APIExport and its endpointslice.
	ProviderWorkspace string
	// HealthProbeBindAddress is where the health/readiness probes bind.
	HealthProbeBindAddress string
	// MetricsBindAddress is where metrics bind ("0" disables them).
	MetricsBindAddress string
	// LeaderElect enables leader election against the host cluster (in-cluster config).
	LeaderElect bool
	// DynamicController configures the kro DynamicController run per consumer workspace.
	DynamicController dynamiccontroller.Config
}

// DefaultConfig returns the operator's defaults, the same as cmd/'s flag defaults.
func DefaultConfig() Config {
	return Config{
		APIExportEndpointSlice: "kro.run",
		ProviderWorkspace:      "root:providers:kro-provider",
		HealthProbeBindAddress: ":8081",
		MetricsBindAddress:     ":9090",
		DynamicController:      DefaultDynamicControllerConfig(),
	}
}

// DefaultDynamicControllerConfig is kro's defaults, but applied per consumer workspace
// rather than per cluster, so these budgets are per tenant and scale with workspace count.
func DefaultDynamicControllerConfig() dynamiccontroller.Config {
	return dynamiccontroller.Config{
		Workers:         1,
		ResyncPeriod:    10 * time.Hour,
		QueueMaxRetries: 20,
		MinRetryDelay:   200 * time.Millisecond,
		MaxRetryDelay:   1000 * time.Second,
		RateLimit:       10,
		BurstLimit:      100,
	}
}

// NewScheme returns the scheme the operator's clients use.
func NewScheme() (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		clientgoscheme.AddToScheme,
		kcpapisv1alpha1.AddToScheme,
		kcpapisv1alpha2.AddToScheme,
		kcpcorev1alpha1.AddToScheme,
		kcptenancyv1alpha1.AddToScheme,
		krov1alpha1.AddToScheme,
		apiextensionsv1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			return nil, err
		}
	}
	return scheme, nil
}

// Engine reconciles ResourceGraphDefinitions in consumer workspaces: it publishes each
// composite type as a bound API and runs one kro DynamicController per workspace.
type Engine struct {
	eng *engine.Engine
}

// NewEngine returns an Engine that reaches every consumer workspace with base retargeted to
// /clusters/<name> (any /clusters suffix on base.Host is dropped). ctx must outlive all
// workspaces; the per-workspace controllers stop with it.
func NewEngine(ctx context.Context, base *rest.Config, scheme *runtime.Scheme, dc dynamiccontroller.Config) *Engine {
	writeBase := rest.CopyConfig(base)
	writeBase.Host = StripClusters(base.Host)
	return &Engine{eng: engine.New(ctx, workspace.NewProvider(writeBase, scheme), dc)}
}

// ReconcileRGD converges the RGD rgdName in the logical cluster clusterName. Errors for which
// IsTransient is true are expected and self-resolving; the caller requeues.
func (e *Engine) ReconcileRGD(ctx context.Context, clusterName, rgdName string) error {
	return e.eng.ReconcileRGD(ctx, clusterName, rgdName)
}

// IsTransient reports whether err is an expected, self-resolving requeue signal.
func IsTransient(err error) bool {
	return engine.IsTransient(err)
}

// SetupControllers registers the RGD controller on mgr, driving eng for every RGD the
// manager's provider serves.
func SetupControllers(mgr mcmanager.Manager, eng *Engine, log logr.Logger) error {
	rec := mcreconcile.Func(func(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
		err := eng.ReconcileRGD(ctx, string(req.ClusterName), req.Name)
		if err == nil {
			return ctrl.Result{}, nil
		}
		// Expected, self-resolving conditions requeue quietly; real failures are logged.
		if IsTransient(err) {
			log.V(1).Info("requeue (transient)", "cluster", string(req.ClusterName), "rgd", req.Name, "reason", err.Error())
		} else {
			log.Error(err, "reconcile failed", "cluster", string(req.ClusterName), "rgd", req.Name)
		}
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	})

	return mcbuilder.ControllerManagedBy(mgr).
		Named("kro-composition").
		For(&krov1alpha1.ResourceGraphDefinition{}).
		Complete(rec)
}

// Run builds the manager against base (the operator's kcp config), wires the controllers and
// blocks until ctx is cancelled or the manager fails.
func Run(ctx context.Context, base *rest.Config, cfg Config) error {
	log := logf.Log.WithName("setup")
	shardBase := StripClusters(base.Host)

	// Config pointing at the provider workspace (the APIExport + endpointslice live there).
	providerCfg := rest.CopyConfig(base)
	providerCfg.Host = shardBase + "/clusters/" + cfg.ProviderWorkspace

	scheme, err := NewScheme()
	if err != nil {
		return fmt.Errorf("scheme: %w", err)
	}

	// kro defines its collectors but registers them nowhere, so without this its metrics go
	// nowhere. No name overlap with controller-runtime's.
	krometrics.Register(crmetrics.Registry)

	// The lease namespace is this pod's, which exists in the host cluster and not in a kcp
	// workspace, so leader election has to point at the host cluster.
	var leaderCfg *rest.Config
	if cfg.LeaderElect {
		leaderCfg, err = rest.InClusterConfig()
		if err != nil {
			return fmt.Errorf("in-cluster config for leader election: %w", err)
		}
	}

	provider, err := apiexport.New(providerCfg, cfg.APIExportEndpointSlice, apiexport.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("apiexport provider: %w", err)
	}
	mgr, err := mcmanager.New(providerCfg, provider, manager.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: cfg.MetricsBindAddress},
		HealthProbeBindAddress: cfg.HealthProbeBindAddress,
		LeaderElection:         cfg.LeaderElect,
		LeaderElectionID:       "kro-composition-operator.platform-mesh.io",
		LeaderElectionConfig:   leaderCfg,
		// Give up the lease on graceful shutdown so a rolling update fails over in seconds
		// rather than waiting for the lease to expire.
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		return fmt.Errorf("mc manager: %w", err)
	}
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("healthz: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("readyz: %w", err)
	}

	eng := NewEngine(ctx, base, scheme, cfg.DynamicController)
	if err := SetupControllers(mgr, eng, logf.Log.WithName("reconcile")); err != nil {
		return fmt.Errorf("builder: %w", err)
	}

	log.Info("starting kro-composition-operator", "endpointslice", cfg.APIExportEndpointSlice,
		"providerWorkspace", cfg.ProviderWorkspace, "featureGates", EnabledFeatures())
	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("manager exited: %w", err)
	}
	return nil
}

// EnabledFeatures lists the kro feature gates that are on, so the startup log shows whether
// a --feature-gates value took effect.
func EnabledFeatures() []string {
	var on []string
	for name := range krofeatures.FeatureGate.GetAll() {
		if krofeatures.FeatureGate.Enabled(name) {
			on = append(on, string(name))
		}
	}
	sort.Strings(on)
	return on
}

// StripClusters returns the shard base URL (scheme://authority) from a kcp host
// that may carry a /clusters/<path> suffix.
func StripClusters(host string) string {
	if i := strings.Index(host, "/clusters/"); i >= 0 {
		return host[:i]
	}
	return strings.TrimSuffix(host, "/")
}
