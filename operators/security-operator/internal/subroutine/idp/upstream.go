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

package idp

import (
	"context"
	"fmt"
	"strings"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/util"
	"go.platform-mesh.io/security-operator/pkg/clientreg/keycloak"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func (s *subroutine) reconcileUpstreamIdentityProviders(
	ctx context.Context,
	idpConfig *pmcorev1alpha1.IdentityProviderConfiguration,
	adminClient *keycloak.AdminClient,
	log *logger.Logger,
) map[string]pmcorev1alpha1.UpstreamIdentityProviderStatus {
	realmName := idpConfig.Name
	desiredAliases := make(map[string]struct{}, len(idpConfig.Spec.UpstreamIdentityProviders))
	managed := make(map[string]pmcorev1alpha1.UpstreamIdentityProviderStatus)

	for i := range idpConfig.Spec.UpstreamIdentityProviders {
		upstream := &idpConfig.Spec.UpstreamIdentityProviders[i]
		alias := strings.TrimSpace(upstream.Alias)
		if alias == "" {
			continue
		}
		desiredAliases[alias] = struct{}{}

		status := pmcorev1alpha1.UpstreamIdentityProviderStatus{
			Alias: alias,
		}

		prevOrgID := idpConfig.Status.ManagedUpstreamIdentityProviders[alias].OrganizationID
		if orgID, domains, err := s.reconcileUpstreamIdentityProvider(ctx, adminClient, realmName, upstream, prevOrgID, log); err != nil {
			status.Ready = false
			status.Message = err.Error()
			log.Error().Err(err).Str("alias", alias).Msg("failed to reconcile upstream identity provider")
		} else {
			status.Ready = true
			status.Message = ""
			status.OrganizationID = orgID
			status.LinkedEmailDomains = domains
			log.Info().Str("alias", alias).Msg("upstream identity provider reconciled")
		}

		status.LastSyncTime = upstreamStatusLastSyncTimeIfChanged(idpConfig.Status.ManagedUpstreamIdentityProviders, status)
		managed[alias] = status
	}

	for alias := range idpConfig.Status.ManagedUpstreamIdentityProviders {
		if _, ok := desiredAliases[alias]; ok {
			continue
		}

		log.Info().Str("alias", alias).Msg("deleting upstream identity provider removed from spec")
		prevStatus := idpConfig.Status.ManagedUpstreamIdentityProviders[alias]
		if err := adminClient.DeleteIdentityProvider(ctx, alias); err != nil {
			deleteStatus := pmcorev1alpha1.UpstreamIdentityProviderStatus{
				Alias:   alias,
				Ready:   false,
				Message: fmt.Sprintf("failed to delete: %v", err),
			}
			deleteStatus.LastSyncTime = upstreamStatusLastSyncTimeIfChanged(idpConfig.Status.ManagedUpstreamIdentityProviders, deleteStatus)
			managed[alias] = deleteStatus
			log.Error().Err(err).Str("alias", alias).Msg("failed to delete upstream identity provider")
			continue
		}
		if prevStatus.OrganizationID != "" {
			if err := adminClient.DeleteOrganization(ctx, prevStatus.OrganizationID); err != nil {
				log.Error().
					Err(err).
					Str("alias", alias).
					Str("organizationId", prevStatus.OrganizationID).
					Msg("failed to delete keycloak organization for removed upstream identity provider")
			}
		}
	}

	return managed
}

// upstreamStatusLastSyncTimeIfChanged keeps the previous LastSyncTime when the meaningful
// status fields are unchanged so the resource status converges (and the
// controller stops reconciling); it only advances to now on an actual change.
func upstreamStatusLastSyncTimeIfChanged(
	previous map[string]pmcorev1alpha1.UpstreamIdentityProviderStatus,
	next pmcorev1alpha1.UpstreamIdentityProviderStatus,
) metav1.Time {
	if existing, ok := previous[next.Alias]; ok {
		candidate := next
		candidate.LastSyncTime = existing.LastSyncTime
		if equality.Semantic.DeepEqual(existing, candidate) {
			return existing.LastSyncTime
		}
	}
	return metav1.Now()
}

func (s *subroutine) reconcileUpstreamIdentityProvider(
	ctx context.Context,
	adminClient *keycloak.AdminClient,
	realmName string,
	upstream *pmcorev1alpha1.UpstreamIdentityProvider,
	prevOrgID string,
	log *logger.Logger,
) (string, []string, error) {
	if upstream.Type != pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC {
		return "", nil, fmt.Errorf("unsupported upstream identity provider type %q", upstream.Type)
	}
	if upstream.OIDC == nil {
		return "", nil, fmt.Errorf("oidc config is required for provider %q", upstream.Alias)
	}

	clientSecret, err := s.readClientSecret(ctx, upstream.OIDC.ClientSecretRef)
	if err != nil {
		return "", nil, fmt.Errorf("reading client secret: %w", err)
	}

	desired, err := keycloak.ToKeycloakIdentityProvider(*upstream, clientSecret)
	if err != nil {
		return "", nil, err
	}

	if upstream.OIDC.DiscoveryURL != "" {
		imported, err := adminClient.ImportIdentityProviderConfig(
			ctx,
			string(upstream.Type),
			upstream.OIDC.DiscoveryURL,
		)
		if err != nil {
			return "", nil, fmt.Errorf("importing oidc discovery config: %w", err)
		}
		keycloak.MergeImportedOIDCConfig(
			&desired,
			imported,
			"clientId",
			"clientSecret",
			"clientAuthMethod",
			"clientAssertionSigningAlg",
			"clientAssertionAudience",
			"defaultScope",
			"prompt",
			"forwardParameters",
		)
	}

	current, err := adminClient.GetIdentityProvider(ctx, upstream.Alias)
	if err != nil {
		return "", nil, fmt.Errorf("checking identity provider existence: %w", err)
	}

	if current == nil {
		if err := adminClient.CreateIdentityProvider(ctx, desired); err != nil {
			return "", nil, err
		}
		current, err = adminClient.GetIdentityProvider(ctx, upstream.Alias)
		if err != nil {
			return "", nil, fmt.Errorf("loading identity provider after create: %w", err)
		}
		if current == nil {
			return "", nil, fmt.Errorf("identity provider %q not found after create", upstream.Alias)
		}
	}

	keycloak.MergeIdentityProviderSpec(current, desired)

	var domains []string
	if upstream.EmailDomainRouting != nil {
		domains = util.NormalizeEmailDomains(upstream.EmailDomainRouting.Domains)
	}
	if len(domains) == 0 {
		// Prefer the organization ID tracked in our status: Keycloak's GET on the
		// identity provider does not always populate organizationId, so relying on
		// it alone can orphan the organization when a provider drops its domains.
		orgID := prevOrgID
		if orgID == "" {
			orgID = current.OrganizationID
		}
		keycloak.ClearOrganizationBrokerConfig(current)
		if err := adminClient.UpdateIdentityProvider(ctx, upstream.Alias, *current); err != nil {
			return "", nil, fmt.Errorf("clearing organization linkage: %w", err)
		}
		if orgID != "" {
			if err := adminClient.DeleteOrganization(ctx, orgID); err != nil {
				return "", nil, fmt.Errorf("deleting keycloak organization: %w", err)
			}
		}
		return "", nil, nil
	}

	org, reusedExistingOrg, err := adminClient.CreateOrUpdateOrganizationForDomains(
		ctx,
		keycloakOrganizationNameForUpstream(realmName, upstream),
		keycloakOrganizationAliasForUpstream(realmName, upstream),
		domains,
	)
	if err != nil {
		return "", nil, fmt.Errorf("ensuring keycloak organization: %w", err)
	}
	if reusedExistingOrg {
		log.Warn().
			Str("alias", upstream.Alias).
			Str("organizationId", org.ID).
			Str("organizationName", org.Name).
			Msg("reusing existing Keycloak organization for email domain; updating name, alias, and domains to match upstream spec")
	}

	keycloak.LinkIdentityProviderOrganization(current, org.ID, *upstream)
	if err := adminClient.UpdateIdentityProvider(ctx, upstream.Alias, *current); err != nil {
		return "", nil, fmt.Errorf("linking identity provider to organization: %w", err)
	}

	return org.ID, domains, nil
}

func keycloakOrganizationNameForUpstream(realmName string, upstream *pmcorev1alpha1.UpstreamIdentityProvider) string {
	if name := strings.TrimSpace(upstream.DisplayName); name != "" {
		return name
	}
	return fmt.Sprintf("%s upstream SSO", realmName)
}

func keycloakOrganizationAliasForUpstream(realmName string, upstream *pmcorev1alpha1.UpstreamIdentityProvider) string {
	alias := strings.TrimSpace(upstream.Alias)
	if alias == "" {
		alias = "upstream"
	}
	return fmt.Sprintf("%s-%s-domains", realmName, alias)
}

func (s *subroutine) readClientSecret(ctx context.Context, secretRef corev1.SecretReference) (string, error) {
	if secretRef.Name == "" {
		return "", nil
	}

	namespace := secretRef.Namespace
	if namespace == "" {
		namespace = "default"
	}

	secret := &corev1.Secret{}
	key := ctrlruntimeclient.ObjectKey{Name: secretRef.Name, Namespace: namespace}
	orgsClient, err := s.kcpClientGetter.NewClientForLogicalCluster(
		ctx,
		string(config.MultiProviderName(config.CoreProviderName, config.OrgsClusterPath)),
	)
	if err != nil {
		return "", fmt.Errorf("getting orgs client: %w", err)
	}
	if err := orgsClient.Get(ctx, key, secret); err != nil {
		return "", err
	}

	if v, ok := secret.Data["client_secret"]; ok {
		return string(v), nil
	}
	if v, ok := secret.Data["clientSecret"]; ok {
		return string(v), nil
	}

	return "", fmt.Errorf("secret %q does not contain client_secret", secretRef.Name)
}
