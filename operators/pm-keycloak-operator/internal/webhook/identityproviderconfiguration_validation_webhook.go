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
	"golang.org/x/oauth2/clientcredentials"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmkeycloakv1alpha1 "go.platform-mesh.io/apis/keycloak/v1alpha1"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/clientreg"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/clientreg/keycloak"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/config"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	mcruntime "sigs.k8s.io/multicluster-runtime"

	"github.com/kcp-dev/logicalcluster/v3"
)

// SetupIdentityProviderConfigurationValidatingWebhookWithManager registers a validating webhook that prevents
// creation of an `IdentityProviderConfiguration` if the corresponding Keycloak realm already exists.
// LegacyExists reports whether a deprecated core.platform-mesh.io
// IdentityProviderConfiguration named name exists in cluster.
type LegacyExists func(ctx context.Context, cluster logicalcluster.Name, name string) (bool, error)

// SetupIdentityProviderConfigurationValidatingWebhookWithManager registers the
// IdentityProviderConfiguration validation for keycloak.platform-mesh.io and,
// with legacyExists set, for the deprecated core.platform-mesh.io kind as well;
// the successor of a legacy object may then be created for an existing realm.
func SetupIdentityProviderConfigurationValidatingWebhookWithManager(ctx context.Context, mgr ctrl.Manager, cfg *config.Config, legacyExists LegacyExists) error {
	keycloakClient, err := newKeycloakAdminClient(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to create keycloak admin client for webhook: %w", err)
	}

	validator := &identityProviderConfigurationValidator{
		keycloakClient: keycloakClient,
		realmDenyList:  slices.Clone(cfg.IDP.RealmDenyList),
		legacyExists:   legacyExists,
	}

	if err := mcruntime.NewWebhookManagedBy(mgr, &pmkeycloakv1alpha1.IdentityProviderConfiguration{}).
		WithValidator(validator).
		Complete(); err != nil {
		return err
	}
	if legacyExists == nil {
		return nil
	}
	//nolint:staticcheck // legacy kind served on purpose
	return mcruntime.NewWebhookManagedBy(mgr, &pmcorev1alpha1.IdentityProviderConfiguration{}).
		WithValidator(&legacyIdentityProviderConfigurationValidator{validator}).
		Complete()
}

var _ admission.Validator[*pmkeycloakv1alpha1.IdentityProviderConfiguration] = (*identityProviderConfigurationValidator)(nil)

//nolint:staticcheck // legacy kind served on purpose
var _ admission.Validator[*pmcorev1alpha1.IdentityProviderConfiguration] = (*legacyIdentityProviderConfigurationValidator)(nil)
var _ realmChecker = (*keycloak.AdminClient)(nil)

type identityProviderConfigurationValidator struct {
	keycloakClient realmChecker
	realmDenyList  []string
	// legacyExists is nil when the deprecated kind is not served.
	legacyExists LegacyExists
}

// realmAdopterGroups may create an IdentityProviderConfiguration for a realm
// that already exists in Keycloak: platform administrators adopting a realm,
// e.g. the platform-mesh-operator migration from core.platform-mesh.io.
var realmAdopterGroups = []string{"system:kcp:admin", "system:masters"}

// mayAdoptRealm reports whether the admission request comes from a realm adopter.
func mayAdoptRealm(ctx context.Context) bool {
	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(req.UserInfo.Groups, func(group string) bool {
		return slices.Contains(realmAdopterGroups, group)
	})
}

type realmChecker interface {
	RealmExists(ctx context.Context, realmName string) (bool, error)
}

func (v *identityProviderConfigurationValidator) ValidateCreate(ctx context.Context, idp *pmkeycloakv1alpha1.IdentityProviderConfiguration) (admission.Warnings, error) {
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

	if mayAdoptRealm(ctx) {
		return nil, nil
	}
	exists, err := v.keycloakClient.RealmExists(ctx, realmName)
	if err != nil {
		return nil, fmt.Errorf("failed to check realm existence in keycloak: %w", err)
	}
	if !exists {
		return nil, nil
	}
	// The realm belongs to a deprecated IdentityProviderConfiguration being moved to this group.
	if v.legacyExists != nil {
		legacy, err := v.legacyExists(ctx, logicalcluster.From(idp), realmName)
		if err != nil {
			return nil, fmt.Errorf("failed to check for a legacy IdentityProviderConfiguration: %w", err)
		}
		if legacy {
			return nil, nil
		}
	}
	return nil, fmt.Errorf("keycloak realm %q already exists", realmName)
}

func (v *identityProviderConfigurationValidator) ValidateUpdate(ctx context.Context, oldObj, newObj *pmkeycloakv1alpha1.IdentityProviderConfiguration) (admission.Warnings, error) {
	// Intentionally allow updates to prevent deadlocks when reconcilers add status/finalizers.
	return nil, nil
}

func (v *identityProviderConfigurationValidator) ValidateDelete(ctx context.Context, obj *pmkeycloakv1alpha1.IdentityProviderConfiguration) (admission.Warnings, error) {
	return nil, nil
}

// legacyIdentityProviderConfigurationValidator applies the same rules to the
// deprecated core.platform-mesh.io kind; only the name is validated.
type legacyIdentityProviderConfigurationValidator struct {
	current *identityProviderConfigurationValidator
}

//nolint:staticcheck // legacy kind served on purpose
func (v *legacyIdentityProviderConfigurationValidator) ValidateCreate(ctx context.Context, idp *pmcorev1alpha1.IdentityProviderConfiguration) (admission.Warnings, error) {
	return v.current.ValidateCreate(ctx, &pmkeycloakv1alpha1.IdentityProviderConfiguration{ObjectMeta: idp.ObjectMeta})
}

//nolint:staticcheck // legacy kind served on purpose
func (v *legacyIdentityProviderConfigurationValidator) ValidateUpdate(_ context.Context, _, _ *pmcorev1alpha1.IdentityProviderConfiguration) (admission.Warnings, error) {
	return nil, nil
}

//nolint:staticcheck // legacy kind served on purpose
func (v *legacyIdentityProviderConfigurationValidator) ValidateDelete(_ context.Context, _ *pmcorev1alpha1.IdentityProviderConfiguration) (admission.Warnings, error) {
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
