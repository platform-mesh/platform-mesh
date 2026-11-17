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

// Package app wires the extension-manager-operator: the scheme, the controllers and the
// manager. cmd/ and the integration tests both go through it, so the tests run the
// operator the way the binary does without importing internal packages.
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	pmuiv1alpha1 "go.platform-mesh.io/apis/ui/v1alpha1"
	"go.platform-mesh.io/extension-manager-operator/internal/controller"
	"go.platform-mesh.io/extension-manager-operator/pkg/config"
	platformmeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

// Deps are the collaborators the controllers need that do not come from flags.
type Deps struct {
	// Log is the operator logger. Required.
	Log *logger.Logger
	// HTTPClient fetches remote ContentConfigurations. nil means http.DefaultClient.
	HTTPClient *http.Client
}

// NewScheme returns the scheme the operator and its server use: client-go, kcp tenancy,
// apis and core v1alpha1, and the ui.platform-mesh.io API.
func NewScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(kcptenancyv1alpha1.AddToScheme(s))
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(s))
	utilruntime.Must(kcpcorev1alpha1.AddToScheme(s))
	utilruntime.Must(pmuiv1alpha1.AddToScheme(s))
	return s
}

// SetupControllers registers the operator's controllers with mgr.
func SetupControllers(mgr mcmanager.Manager, cfg *config.Config, deps Deps) error {
	if deps.Log == nil {
		return errors.New("app: Deps.Log is required")
	}
	common, operator := defaults(cfg)

	rec := controller.NewContentConfigurationReconciler(deps.Log, mgr, *operator, deps.HTTPClient)
	if err := rec.SetupWithManager(mgr, common, deps.Log); err != nil {
		return fmt.Errorf("unable to create ContentConfiguration controller: %w", err)
	}
	return nil
}

// Run builds the manager the way the operator binary does and runs it until ctx is done.
// The reconcile config comes from KUBECONFIG if set, otherwise from ctrl.GetConfig().
// With Operator.KCPAPIExportEndpointSliceName set, the manager reconciles through an
// APIExport provider; otherwise it reconciles the local cluster.
func Run(ctx context.Context, cfg *config.Config, deps Deps) error { // coverage-ignore
	if deps.Log == nil {
		return errors.New("app: Deps.Log is required")
	}
	log := deps.Log
	common, operator := defaults(cfg)
	scheme := NewScheme()

	var reconcileCfg *rest.Config
	var err error
	if kubeconfigPath := os.Getenv("KUBECONFIG"); kubeconfigPath != "" {
		reconcileCfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return fmt.Errorf("unable to load kubeconfig from KUBECONFIG env var: %w", err)
		}
	} else {
		log.Info().Msg("KUBECONFIG not set, using ctrl.GetConfig()")
		reconcileCfg, err = ctrl.GetConfig()
		if err != nil {
			return fmt.Errorf("unable to get kubeconfig: %w", err)
		}
	}
	reconcileCfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return otelhttp.NewTransport(rt)
	})

	var leaderElectionCfg *rest.Config
	if common.LeaderElectionEnabled {
		leaderElectionCfg, err = rest.InClusterConfig()
		if err != nil {
			return fmt.Errorf("unable to get in-cluster config for leader election: %w", err)
		}
	}

	var mcProvider multicluster.Provider
	if operator.KCPAPIExportEndpointSliceName != "" {
		p, err := apiexport.New(reconcileCfg, operator.KCPAPIExportEndpointSliceName, apiexport.Options{
			Scheme: scheme,
		})
		if err != nil {
			return fmt.Errorf("unable to construct APIExportProvider: %w", err)
		}
		log.Info().Msgf("Using APIExportProvider with EndpointSlice %s", operator.KCPAPIExportEndpointSliceName)
		mcProvider = p
	}

	mgr, err := mcmanager.New(reconcileCfg, mcProvider, manager.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: common.Metrics.BindAddress,
			TLSOpts: []func(*tls.Config){
				func(c *tls.Config) { c.NextProtos = []string{"http/1.1"} },
			},
		},
		BaseContext:                   func() context.Context { return ctx },
		HealthProbeBindAddress:        common.HealthProbeBindAddress,
		LeaderElection:                common.LeaderElectionEnabled,
		LeaderElectionID:              "eengiex3.platform-mesh.io",
		LeaderElectionReleaseOnCancel: true,
		LeaderElectionConfig:          leaderElectionCfg,
	})
	if err != nil {
		return fmt.Errorf("unable to set up overall controller manager: %w", err)
	}

	if err := SetupControllers(mgr, &config.Config{Common: common, Operator: operator}, deps); err != nil {
		return err
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}

	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("problem running manager: %w", err)
	}
	return nil
}

func defaults(cfg *config.Config) (*platformmeshconfig.CommonServiceConfig, *config.OperatorConfig) {
	common := &platformmeshconfig.CommonServiceConfig{}
	operator := config.NewOperatorConfig()
	if cfg != nil {
		if cfg.Common != nil {
			common = cfg.Common
		}
		if cfg.Operator != nil {
			operator = cfg.Operator
		}
	}
	return common, operator
}
