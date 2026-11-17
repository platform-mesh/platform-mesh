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

// Package app wires the pm-keycloak-operator controllers onto a manager. It
// is the public surface through which cmd/ and the integration tests set up
// the operator; the reconcilers themselves stay in internal/.
package app

import (
	"context"
	"errors"
	"fmt"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmkeycloakv1alpha1 "go.platform-mesh.io/apis/keycloak/v1alpha1"
	platformeshconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/pm-keycloak-operator/internal/client"
	"go.platform-mesh.io/pm-keycloak-operator/internal/controller"
	"go.platform-mesh.io/pm-keycloak-operator/internal/features"
	"go.platform-mesh.io/pm-keycloak-operator/internal/legacy"
	"go.platform-mesh.io/pm-keycloak-operator/internal/predicates"
	"go.platform-mesh.io/pm-keycloak-operator/internal/subroutine/idp"
	internalwebhook "go.platform-mesh.io/pm-keycloak-operator/internal/webhook"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/config"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/kcp-dev/multicluster-provider/pkg/provider"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

// NewScheme returns the scheme the pm-keycloak-operator managers use.
func NewScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(kcptenancyv1alpha1.AddToScheme(s))
	utilruntime.Must(pmcorev1alpha1.AddToScheme(s))
	utilruntime.Must(pmkeycloakv1alpha1.AddToScheme(s))
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(s))
	utilruntime.Must(kcpapisv1alpha2.AddToScheme(s))
	utilruntime.Must(kcpcorev1alpha1.AddToScheme(s))
	return s
}

// Deps are the dependencies of the operator controllers beyond the manager
// and the operator config.
type Deps struct {
	// Log is the operator logger.
	Log *logger.Logger
	// CommonConfig is the shared service config (concurrency, debug label).
	// NewDefaultConfig is used when nil.
	CommonConfig *platformeshconfig.CommonServiceConfig
	// KeycloakProvider is the provider behind the keycloak.platform-mesh.io
	// APIExport; secrets and organization resources are written through it.
	KeycloakProvider *provider.Provider
	// RuntimeClient reaches the cluster the operator runs in.
	RuntimeClient ctrlruntimeclient.Client
	// RestConfig reaches kcp as the operator itself, for the organization
	// workspaces the initializer writes to and the webhook reads from.
	RestConfig *rest.Config
}

// SetupOperatorControllers registers the operator's reconcilers and, when
// enabled, its validating webhook on mgr.
//
// mgr must run on a multi provider that holds the keycloak and core
// APIExport providers under config.KeycloakProviderName and
// config.CoreProviderName; with LegacyAPIs also the system one under
// config.SystemProviderName.
func SetupOperatorControllers(ctx context.Context, mgr mcmanager.Manager, cfg *config.Config, deps Deps) error {
	if deps.Log == nil {
		return errors.New("deps.Log must be set")
	}
	if deps.KeycloakProvider == nil {
		return errors.New("deps.KeycloakProvider must be set")
	}
	if deps.RestConfig == nil {
		return errors.New("deps.RestConfig must be set")
	}
	commonCfg := deps.CommonConfig
	if commonCfg == nil {
		commonCfg = platformeshconfig.NewDefaultConfig()
	}
	log := deps.Log
	scheme := mgr.GetLocalManager().GetScheme()

	kcpClientGetter := iclient.NewManagerKCPClientGetter(mgr, deps.KeycloakProvider)
	kcpClientGetterWithConfig := iclient.NewConfigSchemeKCPClientGetter(deps.RestConfig, scheme)
	keycloakEngageOpts := controller.WithClustersFromNamedProvider(config.KeycloakProviderName, config.ProviderSeparator)

	// With LegacyAPIs a keycloak IdentityProviderConfiguration created next to
	// its deprecated core twin takes over that twin's realm and clients and
	// keeps the twin's status current for consumers still reading it.
	legacyAPIs := cfg.FeatureEnabled(features.LegacyAPIs)
	var adopt idp.LegacyAdopter
	var mirror idp.LegacyMirror
	var legacyExists internalwebhook.LegacyExists
	if legacyAPIs {
		adopt = legacy.IdentityProviderConfigurationAdopter(kcpClientGetter)
		mirror = legacy.IdentityProviderConfigurationMirror(kcpClientGetter)
		legacyExists = legacy.IdentityProviderConfigurationExists(kcpClientGetterWithConfig)
	}

	idpReconciler, err := controller.NewIdentityProviderConfigurationReconciler(ctx, mgr, kcpClientGetter, cfg, log, adopt, mirror)
	if err != nil {
		return fmt.Errorf("creating identityprovider reconciler: %w", err)
	}
	if err := idpReconciler.SetupWithManager(mgr, commonCfg, log); err != nil {
		return fmt.Errorf("setting up identityprovider controller: %w", err)
	}
	inviteReconciler, err := controller.NewInviteReconciler(ctx, mgr, cfg, log, kcpClientGetter)
	if err != nil {
		return fmt.Errorf("creating invite reconciler: %w", err)
	}
	if err := inviteReconciler.SetupWithManager(mgr, commonCfg, log); err != nil {
		return fmt.Errorf("setting up invite controller: %w", err)
	}
	if legacyAPIs {
		legacyIDPReconciler, err := controller.NewLegacyIdentityProviderConfigurationReconciler(ctx, mgr, kcpClientGetter, cfg, log)
		if err != nil {
			return fmt.Errorf("creating identityprovider-legacy reconciler: %w", err)
		}
		if err := legacyIDPReconciler.SetupWithManager(mgr, commonCfg, log); err != nil {
			return fmt.Errorf("setting up identityprovider-legacy controller: %w", err)
		}
		legacyInviteReconciler, err := controller.NewLegacyInviteReconciler(ctx, mgr, cfg, log, kcpClientGetter)
		if err != nil {
			return fmt.Errorf("creating invite-legacy reconciler: %w", err)
		}
		if err := legacyInviteReconciler.SetupWithManager(mgr, commonCfg, log); err != nil {
			return fmt.Errorf("setting up invite-legacy controller: %w", err)
		}
	}

	orgReconciler, err := controller.NewOrgLogicalClusterController(log, kcpClientGetterWithConfig, *cfg, deps.RuntimeClient, mgr, controller.ControllerOptions{
		Name: "OrgLogicalClusterReconciler",
	})
	if err != nil {
		return fmt.Errorf("creating logicalcluster reconciler: %w", err)
	}
	if err := orgReconciler.WithEngageOptions(keycloakEngageOpts).SetupWithManager(mgr, commonCfg,
		predicates.LogicalClusterIsAccountTypeOrg(),
		predicates.HasInitializerPredicate(cfg.InitializerName()),
	); err != nil {
		return fmt.Errorf("setting up logicalcluster controller: %w", err)
	}

	if cfg.Webhooks.Enabled {
		log.Info().Msg("validating webhooks are enabled")
		if err := internalwebhook.SetupIdentityProviderConfigurationValidatingWebhookWithManager(ctx, mgr.GetLocalManager(), cfg, legacyExists); err != nil {
			return fmt.Errorf("setting up IdentityProviderConfiguration webhook: %w", err)
		}
	}
	return nil
}
