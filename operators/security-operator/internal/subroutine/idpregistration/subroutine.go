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

package idpregistration

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc"
	"golang.org/x/oauth2/clientcredentials"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/util"
	"go.platform-mesh.io/security-operator/pkg/clientreg"
	"go.platform-mesh.io/security-operator/pkg/clientreg/keycloak"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

const (
	finalizerName                  = "system.platform-mesh.io/idpregistration-finalizer"
	clusterNotReadyRequeue         = 5 * time.Second
	idpRegistrationSuccessMessage  = "Configured in Keycloak"
)

type subroutine struct {
	keycloakBaseURL string
	adminHTTPClient *http.Client
	discoveryClient *http.Client
	mgr             mcmanager.Manager
	cfg             *config.Config
	kcpClientGetter iclient.KCPClientGetter
}

func New(ctx context.Context, cfg *config.Config, mgr mcmanager.Manager, kcpClientGetter iclient.KCPClientGetter) (*subroutine, error) {
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

	return &subroutine{
		keycloakBaseURL: cfg.Keycloak.BaseURL,
		adminHTTPClient: cCfg.Client(ctx),
		discoveryClient: &http.Client{Timeout: time.Duration(cfg.HttpClientTimeoutSeconds) * time.Second},
		mgr:             mgr,
		cfg:             cfg,
		kcpClientGetter: kcpClientGetter,
	}, nil
}

var _ subroutines.Subroutine = &subroutine{}

func (s *subroutine) GetName() string { return "IdPRegistration" }

func (s *subroutine) Finalizers(_ ctrlruntimeclient.Object) []string {
	return []string{finalizerName}
}

func (s *subroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	result, err := s.process(ctx, obj)
	if res, ok := pendingIfClusterNotReady(err); ok {
		return res, nil
	}
	return result, err
}

func (s *subroutine) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	result, err := s.finalize(ctx, obj)
	if res, ok := pendingIfClusterNotReady(err); ok {
		return res, nil
	}
	return result, err
}

func (s *subroutine) process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	reg := obj.(*pmcorev1alpha1.IdPRegistration)
	log := logger.LoadLoggerFromContext(ctx)

	realm, err := s.realmForCluster(ctx)
	if err != nil {
		return subroutines.OK(), err
	}

	if slices.Contains(s.cfg.IDP.RealmDenyList, realm) {
		err := fmt.Errorf("upstream IdP registration is not allowed for realm %q", realm)
		s.setStatus(reg, false, realm, "", nil, err.Error())
		return subroutines.OK(), err
	}

	_, adminClient := s.newAdminClient(realm)

	orgID, domains, err := s.reconcileBroker(ctx, adminClient, realm, reg)
	if err != nil {
		s.setStatus(reg, false, realm, reg.Status.OrganizationID, reg.Status.LinkedEmailDomains, err.Error())
		return subroutines.OK(), err
	}

	s.setStatus(reg, true, realm, orgID, domains, idpRegistrationSuccessMessage)
	log.Info().Str("alias", reg.Spec.Alias).Str("realm", realm).Msg("IdPRegistration reconciled")
	return subroutines.OK(), nil
}

func (s *subroutine) finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	reg := obj.(*pmcorev1alpha1.IdPRegistration)
	log := logger.LoadLoggerFromContext(ctx)

	realm, err := s.realmForCluster(ctx)
	if err != nil {
		return subroutines.OK(), err
	}

	_, adminClient := s.newAdminClient(realm)
	alias := strings.TrimSpace(reg.Spec.Alias)

	if alias != "" {
		if err := adminClient.DeleteIdentityProvider(ctx, alias); err != nil {
			return subroutines.OK(), err
		}
		if reg.Status.OrganizationID != "" {
			if err := adminClient.DeleteOrganization(ctx, reg.Status.OrganizationID); err != nil {
				log.Error().Err(err).Str("organizationId", reg.Status.OrganizationID).Msg("failed to delete keycloak organization")
			}
		}
	}

	if reg.Spec.OIDC != nil && reg.Spec.OIDC.ClientSecretRef.Name != "" {
		cluster, clusterErr := s.mgr.ClusterFromContext(ctx)
		if clusterErr != nil {
			return subroutines.OK(), clusterErr
		}
		if err := deleteManagedClientSecret(ctx, cluster.GetClient(), reg.Spec.OIDC.ClientSecretRef.Name, reg.Name); err != nil {
			log.Error().Err(err).Str("secret", reg.Spec.OIDC.ClientSecretRef.Name).Msg("failed to delete client secret")
		}
	}

	log.Info().Str("alias", alias).Str("realm", realm).Msg("IdPRegistration finalized")
	return subroutines.OK(), nil
}

func (s *subroutine) reconcileBroker(
	ctx context.Context,
	adminClient *keycloak.AdminClient,
	realmName string,
	reg *pmcorev1alpha1.IdPRegistration,
) (string, []string, error) {
	if reg.Spec.Type != pmcorev1alpha1.UpstreamIdentityProviderTypeOIDC {
		return "", nil, fmt.Errorf("unsupported provider type %q", reg.Spec.Type)
	}
	if reg.Spec.OIDC == nil {
		return "", nil, fmt.Errorf("oidc config is required")
	}

	clientSecret, err := s.readClientSecret(ctx, reg.Spec.OIDC.ClientSecretRef.Name)
	if err != nil {
		return "", nil, fmt.Errorf("reading client secret: %w", err)
	}

	var discovery *keycloak.OIDCDiscovery
	if reg.Spec.OIDC.DiscoveryURL != "" {
		d, err := keycloak.FetchOIDCDiscovery(ctx, s.discoveryClient, reg.Spec.OIDC.DiscoveryURL)
		if err != nil {
			return "", nil, fmt.Errorf("fetching discovery: %w", err)
		}
		discovery = &d
	}

	desired, err := keycloak.ToKeycloakIdentityProviderFromRegistration(*reg, clientSecret, discovery)
	if err != nil {
		return "", nil, err
	}

	current, err := adminClient.GetIdentityProvider(ctx, reg.Spec.Alias)
	if err != nil {
		return "", nil, fmt.Errorf("checking identity provider existence: %w", err)
	}

	if current == nil {
		if err := adminClient.CreateIdentityProvider(ctx, desired); err != nil {
			return "", nil, err
		}
		current, err = adminClient.GetIdentityProvider(ctx, reg.Spec.Alias)
		if err != nil || current == nil {
			return "", nil, fmt.Errorf("loading identity provider after create: %w", err)
		}
	}

	keycloak.SyncIdentityProviderSpec(current, desired)

	if err := adminClient.UpdateIdentityProvider(ctx, reg.Spec.Alias, *current); err != nil {
		return "", nil, fmt.Errorf("updating identity provider: %w", err)
	}

	if err := adminClient.SyncIdentityProviderMappers(ctx, reg.Spec.Alias); err != nil {
		return "", nil, fmt.Errorf("syncing identity provider mappers: %w", err)
	}

	current, err = adminClient.GetIdentityProvider(ctx, reg.Spec.Alias)
	if err != nil || current == nil {
		return "", nil, fmt.Errorf("loading identity provider after sync: %w", err)
	}

	upstream := keycloak.RegistrationToUpstream(*reg)
	var domains []string
	if reg.Spec.EmailDomainRouting != nil {
		domains = util.NormalizeEmailDomains(reg.Spec.EmailDomainRouting.Domains)
	}

	prevOrgID := reg.Status.OrganizationID
	if len(domains) == 0 {
		orgID := prevOrgID
		if orgID == "" && current.OrganizationID != "" {
			orgID = current.OrganizationID
		}
		keycloak.ClearOrganizationBrokerConfig(current)
		if err := adminClient.UpdateIdentityProvider(ctx, reg.Spec.Alias, *current); err != nil {
			return "", nil, fmt.Errorf("clearing organization linkage: %w", err)
		}
		if orgID != "" {
			if err := adminClient.DeleteOrganization(ctx, orgID); err != nil {
				return "", nil, fmt.Errorf("deleting keycloak organization: %w", err)
			}
		}
		return "", nil, nil
	}

	if err := adminClient.EnsureOrganizationsEnabled(ctx, realmName); err != nil {
		return "", nil, fmt.Errorf("enabling organizations for realm: %w", err)
	}

	org, _, err := adminClient.CreateOrUpdateOrganizationForDomains(
		ctx,
		organizationName(realmName, reg),
		organizationAlias(realmName, reg),
		domains,
	)
	if err != nil {
		return "", nil, fmt.Errorf("ensuring keycloak organization: %w", err)
	}

	keycloak.LinkIdentityProviderOrganization(current, org.ID, upstream)
	if err := adminClient.UpdateIdentityProvider(ctx, reg.Spec.Alias, *current); err != nil {
		return "", nil, fmt.Errorf("linking identity provider to organization: %w", err)
	}

	return org.ID, domains, nil
}

func (s *subroutine) newAdminClient(realmName string) (clientreg.Client, *keycloak.AdminClient) {
	adminClient := keycloak.NewAdminClient(s.adminHTTPClient, s.keycloakBaseURL, realmName)
	httpClient := &http.Client{
		Timeout:   time.Duration(s.cfg.HttpClientTimeoutSeconds) * time.Second,
		Transport: clientreg.NewRetryTransport(nil, adminClient),
	}
	oidcClient := clientreg.NewClient(
		clientreg.WithHTTPClient(httpClient),
		clientreg.WithTokenProvider(adminClient),
	)
	return oidcClient, adminClient
}

func (s *subroutine) realmForCluster(ctx context.Context) (string, error) {
	clusterName, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return "", fmt.Errorf("cluster name missing from context")
	}

	cl, err := s.kcpClientGetter.NewClientForLogicalCluster(ctx, string(clusterName))
	if err != nil {
		return "", fmt.Errorf("getting client for cluster %q: %w", clusterName, err)
	}

	var accountInfo pmcorev1alpha1.AccountInfo
	if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: "account"}, &accountInfo); err != nil {
		return "", fmt.Errorf("reading AccountInfo: %w", err)
	}

	realm := strings.TrimSpace(accountInfo.Spec.Organization.Name)
	if realm == "" {
		return "", fmt.Errorf("organization name is empty in AccountInfo")
	}
	return realm, nil
}

func (s *subroutine) readClientSecret(ctx context.Context, secretName string) (string, error) {
	if secretName == "" {
		return "", fmt.Errorf("clientSecretRef.name is required")
	}

	cluster, err := s.mgr.ClusterFromContext(ctx)
	if err != nil {
		return "", fmt.Errorf("getting cluster from context: %w", err)
	}

	secret := &corev1.Secret{}
	if err := cluster.GetClient().Get(ctx, ctrlruntimeclient.ObjectKey{Name: secretName, Namespace: "default"}, secret); err != nil {
		return "", err
	}

	if v, ok := secret.Data["client_secret"]; ok {
		return string(v), nil
	}
	if v, ok := secret.Data["clientSecret"]; ok {
		return string(v), nil
	}
	return "", fmt.Errorf("secret %q does not contain client_secret", secretName)
}

func (s *subroutine) setStatus(reg *pmcorev1alpha1.IdPRegistration, ready bool, realm, orgID string, domains []string, message string) {
	reg.Status.Ready = ready
	reg.Status.OrganizationID = orgID
	reg.Status.LinkedEmailDomains = domains
	reg.Status.Message = message
	if ready {
		reg.Status.RedirectURI = keycloak.BrokerRedirectURI(s.cfg.BaseDomain, realm, reg.Spec.Alias)
	} else {
		reg.Status.RedirectURI = ""
	}
}

func organizationName(realmName string, reg *pmcorev1alpha1.IdPRegistration) string {
	if name := strings.TrimSpace(reg.Spec.DisplayName); name != "" {
		return name
	}
	return fmt.Sprintf("%s upstream SSO", realmName)
}

func organizationAlias(realmName string, reg *pmcorev1alpha1.IdPRegistration) string {
	alias := strings.TrimSpace(reg.Spec.Alias)
	if alias == "" {
		alias = "upstream"
	}
	return fmt.Sprintf("%s-%s-domains", realmName, alias)
}

func pendingIfClusterNotReady(err error) (subroutines.Result, bool) {
	if err == nil || !errors.Is(err, multicluster.ErrClusterNotFound) {
		return subroutines.OK(), false
	}
	return subroutines.Pending(clusterNotReadyRequeue, "waiting for cluster to be engaged by the multicluster provider"), true
}
