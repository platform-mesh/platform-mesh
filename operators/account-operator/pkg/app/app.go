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

// Package app wires the account-operator: the scheme, the controllers, the
// webhooks and the manager. cmd/ and the integration tests both go through
// it, so there is a single wiring path. The reconcilers themselves stay in
// internal/.
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"go.platform-mesh.io/account-operator/internal/controller"
	"go.platform-mesh.io/account-operator/pkg/config"
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	platformmeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	pathaware "github.com/kcp-dev/multicluster-provider/path-aware"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

// LeaderElectionID is the lease name the operator uses for leader election.
const LeaderElectionID = "8c290d9a.platform-mesh.io"

// Deps carries what the wiring needs besides the operator configuration.
type Deps struct {
	// Log is the operator logger. Required.
	Log *logger.Logger
	// Common is the shared service configuration (concurrency, debug label,
	// metrics, leader election, ...). nil means the zero value.
	Common *platformmeshconfig.CommonServiceConfig
}

func (d Deps) common() *platformmeshconfig.CommonServiceConfig {
	if d.Common == nil {
		return &platformmeshconfig.CommonServiceConfig{}
	}
	return d.Common
}

// AddToScheme registers every API group the operator reads or writes.
func AddToScheme(s *runtime.Scheme) error {
	for _, add := range []func(*runtime.Scheme) error{
		pmcorev1alpha1.AddToScheme,
		kcpapisv1alpha1.AddToScheme,
		kcpcorev1alpha1.AddToScheme,
		kcptenancyv1alpha1.AddToScheme,
	} {
		if err := add(s); err != nil {
			return err
		}
	}
	return nil
}

// NewScheme returns a scheme with AddToScheme applied.
func NewScheme() (*runtime.Scheme, error) {
	s := runtime.NewScheme()
	if err := AddToScheme(s); err != nil {
		return nil, err
	}
	return s, nil
}

// SetupControllers registers the Account controller and, when
// cfg.Controllers.AccountInfo.Enabled, the AccountInfo controller with mgr.
func SetupControllers(mgr mcmanager.Manager, cfg config.OperatorConfig, deps Deps) error {
	if deps.Log == nil {
		return errors.New("app.Deps.Log is required")
	}
	common := deps.common()

	accountReconciler, err := controller.NewAccountReconciler(deps.Log, mgr, cfg)
	if err != nil {
		return fmt.Errorf("creating account reconciler: %w", err)
	}
	if err := accountReconciler.SetupWithManager(mgr, common, deps.Log); err != nil {
		return fmt.Errorf("setting up Account controller: %w", err)
	}

	if cfg.Controllers.AccountInfo.Enabled {
		accountInfoReconciler, err := controller.NewAccountInfoReconciler(deps.Log, mgr, cfg)
		if err != nil {
			return fmt.Errorf("creating account info reconciler: %w", err)
		}
		if err := accountInfoReconciler.SetupWithManager(mgr, common, deps.Log); err != nil {
			return fmt.Errorf("setting up AccountInfo controller: %w", err)
		}
	}
	return nil
}

// SetupWebhooks registers the Account validating webhook when
// cfg.Webhooks.Enabled; otherwise it does nothing.
func SetupWebhooks(mgr mcmanager.Manager, cfg config.OperatorConfig, deps Deps) error {
	if !cfg.Webhooks.Enabled {
		return nil
	}

	var denyList []string
	if cfg.Webhooks.DenyList != "" {
		denyList = strings.Split(cfg.Webhooks.DenyList, ",")
		for i, item := range denyList {
			denyList[i] = strings.TrimSpace(item)
		}
	}

	accountTypeAllowList := make([]pmcorev1alpha1.AccountType, 0, len(cfg.Webhooks.AdditionalAccountTypes)+1)
	accountTypeAllowList = append(accountTypeAllowList, pmcorev1alpha1.AccountTypeOrg)
	for _, additionalType := range cfg.Webhooks.AdditionalAccountTypes {
		accountTypeAllowList = append(accountTypeAllowList, pmcorev1alpha1.AccountType(additionalType))
	}

	if deps.Log != nil {
		deps.Log.Info().Strs("deniedNames", denyList).Msg("webhooks are enabled")
	}
	if err := pmcorev1alpha1.SetupAccountWebhookWithManager(mgr.GetLocalManager(), denyList, accountTypeAllowList); err != nil {
		return fmt.Errorf("setting up Account webhook: %w", err)
	}
	return nil
}

// Run builds the provider and the manager from the in-cluster/kubeconfig rest
// config, wires controllers, webhooks and health checks, and runs the manager
// until ctx is cancelled.
func Run(ctx context.Context, cfg config.OperatorConfig, deps Deps) error { // coverage-ignore
	if deps.Log == nil {
		return errors.New("app.Deps.Log is required")
	}
	log := deps.Log
	common := deps.common()

	scheme, err := NewScheme()
	if err != nil {
		return fmt.Errorf("building scheme: %w", err)
	}

	var tlsOpts []func(*tls.Config)
	if !common.EnableHTTP2 {
		tlsOpts = append(tlsOpts, func(c *tls.Config) {
			log.Info().Msg("disabling http/2")
			c.NextProtos = []string{"http/1.1"}
		})
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
		CertDir: cfg.Webhooks.CertDir,
		Port:    cfg.Webhooks.Port,
	})

	restCfg, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("getting rest config: %w", err)
	}
	restCfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return otelhttp.NewTransport(rt)
	})

	var leaderCfg *rest.Config
	if common.LeaderElectionEnabled {
		leaderCfg, err = rest.InClusterConfig()
		if err != nil {
			return fmt.Errorf("getting in-cluster config: %w", err)
		}
	}

	provider, err := pathaware.New(restCfg, cfg.Kcp.ApiExportEndpointSliceName, apiexport.Options{
		Log:    &ctrl.Log,
		Scheme: scheme,
	})
	if err != nil {
		return fmt.Errorf("creating APIExport provider: %w", err)
	}

	mgr, err := mcmanager.New(restCfg, provider, mcmanager.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   common.Metrics.BindAddress,
			SecureServing: common.Metrics.Secure,
			TLSOpts:       tlsOpts,
		},
		BaseContext:                   func() context.Context { return ctx },
		WebhookServer:                 webhookServer,
		HealthProbeBindAddress:        common.HealthProbeBindAddress,
		LeaderElection:                common.LeaderElectionEnabled,
		LeaderElectionID:              LeaderElectionID,
		LeaderElectionConfig:          leaderCfg,
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		return fmt.Errorf("creating manager: %w", err)
	}

	if err := SetupControllers(mgr, cfg, deps); err != nil {
		return err
	}
	if err := SetupWebhooks(mgr, cfg, deps); err != nil {
		return err
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("setting up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("setting up ready check: %w", err)
	}

	log.Info().Msg("starting manager")
	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("running manager: %w", err)
	}
	return nil
}
