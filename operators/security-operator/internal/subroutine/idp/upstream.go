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
	"go.platform-mesh.io/security-operator/pkg/clientreg/keycloak"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func (s *subroutine) reconcileUpstreamIdentityProviders(
	ctx context.Context,
	idpConfig *pmcorev1alpha1.IdentityProviderConfiguration,
	adminClient *keycloak.AdminClient,
	log *logger.Logger,
) map[string]pmcorev1alpha1.UpstreamIdentityProviderStatus {
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
			Alias:        alias,
			LastSyncTime: metav1.Now(),
		}

		if err := s.reconcileUpstreamProvider(ctx, adminClient, upstream); err != nil {
			status.Ready = false
			status.Message = err.Error()
			log.Error().Err(err).Str("alias", alias).Msg("failed to reconcile upstream identity provider")
		} else {
			status.Ready = true
			status.Message = ""
			log.Info().Str("alias", alias).Msg("upstream identity provider reconciled")
		}

		managed[alias] = status
	}

	for alias := range idpConfig.Status.ManagedUpstreamIdentityProviders {
		if _, ok := desiredAliases[alias]; ok {
			continue
		}

		log.Info().Str("alias", alias).Msg("deleting upstream identity provider removed from spec")
		if err := adminClient.DeleteIdentityProvider(ctx, alias); err != nil {
			managed[alias] = pmcorev1alpha1.UpstreamIdentityProviderStatus{
				Alias:        alias,
				Ready:        false,
				Message:      fmt.Sprintf("failed to delete: %v", err),
				LastSyncTime: metav1.Now(),
			}
			log.Error().Err(err).Str("alias", alias).Msg("failed to delete upstream identity provider")
		}
	}

	return managed
}

func (s *subroutine) reconcileUpstreamProvider(
	ctx context.Context,
	adminClient *keycloak.AdminClient,
	upstream *pmcorev1alpha1.UpstreamIdentityProvider,
) error {
	if upstream.Type != pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC {
		return fmt.Errorf("unsupported upstream identity provider type %q", upstream.Type)
	}
	if upstream.OIDC == nil {
		return fmt.Errorf("oidc config is required for provider %q", upstream.Alias)
	}

	clientSecret, err := s.readClientSecret(ctx, upstream.OIDC.ClientSecretRef)
	if err != nil {
		return fmt.Errorf("reading client secret: %w", err)
	}

	rep, err := keycloak.ToKeycloakIdentityProvider(*upstream, clientSecret)
	if err != nil {
		return err
	}

	if upstream.OIDC.DiscoveryURL != "" {
		imported, err := adminClient.ImportIdentityProviderConfig(
			ctx,
			string(upstream.Type),
			upstream.OIDC.DiscoveryURL,
		)
		if err != nil {
			return fmt.Errorf("importing oidc discovery config: %w", err)
		}
		keycloak.MergeImportedOIDCConfig(
			&rep,
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

	existing, err := adminClient.GetIdentityProvider(ctx, upstream.Alias)
	if err != nil {
		return fmt.Errorf("checking identity provider existence: %w", err)
	}

	if existing == nil {
		return adminClient.CreateIdentityProvider(ctx, rep)
	}

	return adminClient.UpdateIdentityProvider(ctx, upstream.Alias, rep)
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
