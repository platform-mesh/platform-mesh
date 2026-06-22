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

package webhook

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/coreos/go-oidc"
	corev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/pkg/clientreg"
	"go.platform-mesh.io/security-operator/pkg/clientreg/keycloak"
	"golang.org/x/oauth2/clientcredentials"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	mcruntime "sigs.k8s.io/multicluster-runtime"

	"k8s.io/apimachinery/pkg/runtime"
)

// SetupIdentityProviderConfigurationValidatingWebhookWithManager registers a validating webhook that prevents
// creation of an `IdentityProviderConfiguration` if the corresponding Keycloak realm already exists.
func SetupIdentityProviderConfigurationValidatingWebhookWithManager(ctx context.Context, mgr ctrl.Manager, cfg *config.Config) error {
	keycloakClient, err := newKeycloakAdminClient(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to create keycloak admin client for webhook: %w", err)
	}

	realmDenyList := slices.Clone(cfg.IDP.RealmDenyList)

	return mcruntime.NewWebhookManagedBy(mgr).
		For(&corev1alpha1.IdentityProviderConfiguration{}).
		WithValidator(&identityProviderConfigurationValidator{keycloakClient: keycloakClient, realmDenyList: realmDenyList}).
		Complete()
}

var _ webhook.CustomValidator = (*identityProviderConfigurationValidator)(nil) // nolint:staticcheck
var _ realmChecker = (*keycloak.AdminClient)(nil)

type identityProviderConfigurationValidator struct {
	keycloakClient realmChecker
	realmDenyList  []string
}

type realmChecker interface {
	RealmExists(ctx context.Context, realmName string) (bool, error)
}

func (v *identityProviderConfigurationValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	idp := obj.(*corev1alpha1.IdentityProviderConfiguration)

	realmName := strings.TrimSpace(idp.GetName())
	if realmName == "" {
		return nil, fmt.Errorf("realm name must not be empty")
	}
	if realmName == "master" {
		return nil, fmt.Errorf("creation of IdentityProviderConfiguration for realm 'master' is not allowed")
	}
	if slices.Contains(v.realmDenyList, realmName) {
		return nil, fmt.Errorf("creation of IdentityProviderConfiguration for realm %q is not allowed", realmName)
	}

	exists, err := v.keycloakClient.RealmExists(ctx, realmName)
	if err != nil {
		return nil, fmt.Errorf("failed to check realm existence in keycloak: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("keycloak realm %q already exists", realmName)
	}

	return nil, nil
}

func (v *identityProviderConfigurationValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	// Intentionally allow updates to prevent deadlocks when reconcilers add status/finalizers.
	return nil, nil
}

func (v *identityProviderConfigurationValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func newKeycloakAdminClient(ctx context.Context, cfg *config.Config) (*keycloak.AdminClient, error) {
	issuer := fmt.Sprintf("%s/realms/master", cfg.Keycloak.BaseURL)
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}

	cCfg := clientcredentials.Config{
		ClientID:     cfg.Keycloak.ClientID,
		ClientSecret: cfg.Keycloak.ClientSecret,
		TokenURL:     provider.Endpoint().TokenURL,
	}

	adminHTTPClient := cCfg.Client(ctx)

	// Use the master realm for admin endpoint access.
	adminClient := keycloak.NewAdminClient(adminHTTPClient, cfg.Keycloak.BaseURL, "master")
	adminHTTPClient.Transport = clientreg.NewRetryTransport(adminHTTPClient.Transport, adminClient)

	return adminClient, nil
}
