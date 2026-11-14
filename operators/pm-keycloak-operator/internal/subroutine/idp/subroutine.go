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
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc"
	"golang.org/x/oauth2/clientcredentials"

	pmkeycloakv1alpha1 "go.platform-mesh.io/apis/keycloak/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	iclient "go.platform-mesh.io/pm-keycloak-operator/internal/client"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/clientreg"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/clientreg/keycloak"
	"go.platform-mesh.io/pm-keycloak-operator/pkg/config"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

// clusterNotReadyRequeue is how long to wait before retrying when the
// logical cluster has not yet been engaged by the multicluster provider. This
// is an expected transient condition during operator startup, before the
// provider has finished engaging all clusters.
const clusterNotReadyRequeue = 5 * time.Second

// pendingIfClusterNotReady converts a transient "cluster not yet engaged by
// the multicluster provider" error into a Pending result, so the object is
// retried without being marked as failed. It returns ok=false for any other
// error (or nil), in which case the caller should surface the original result.
func pendingIfClusterNotReady(ctx context.Context, err error) (subroutines.Result, bool) {
	if err == nil || !errors.Is(err, multicluster.ErrClusterNotFound) {
		return subroutines.OK(), false
	}
	logger.LoadLoggerFromContext(ctx).Info().Err(err).
		Msg("cluster not engaged by multicluster provider yet, requeuing")
	return subroutines.Pending(clusterNotReadyRequeue, "waiting for cluster to be engaged by the multicluster provider"), true
}

// LegacyAdopter hands a deprecated core.platform-mesh.io
// IdentityProviderConfiguration of the same name over to the one being
// reconciled, returning the managed clients to continue with.
// nil means there is nothing to adopt.
type LegacyAdopter func(ctx context.Context, engaged multicluster.ClusterName, name string) (map[string]pmkeycloakv1alpha1.ManagedClient, error)

// LegacyMirror copies status to a deprecated core.platform-mesh.io
// IdentityProviderConfiguration of the same name, if one exists.
type LegacyMirror func(ctx context.Context, engaged multicluster.ClusterName, name string, status pmkeycloakv1alpha1.IdentityProviderConfigurationStatus) error

type subroutine struct {
	keycloakBaseURL string
	adminClient     *http.Client
	mgr             mcmanager.Manager
	cfg             *config.Config
	kcpClientGetter iclient.KCPClientGetter
	adoptLegacy     LegacyAdopter
	mirrorLegacy    LegacyMirror
}

// WithLegacyAdopter makes the subroutine take over the Keycloak clients of a
// deprecated IdentityProviderConfiguration when it reconciles a successor
// that has no managed clients yet.
func (s *subroutine) WithLegacyAdopter(adopt LegacyAdopter) *subroutine {
	s.adoptLegacy = adopt
	return s
}

// WithLegacyMirror makes the subroutine keep the status of a deprecated twin
// in sync after each reconcile.
func (s *subroutine) WithLegacyMirror(mirror LegacyMirror) *subroutine {
	s.mirrorLegacy = mirror
	return s
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

	adminClient := cCfg.Client(ctx)

	return &subroutine{
		keycloakBaseURL: cfg.Keycloak.BaseURL,
		adminClient:     adminClient,
		mgr:             mgr,
		cfg:             cfg,
		kcpClientGetter: kcpClientGetter,
	}, nil
}

func (s *subroutine) newOIDCClient(realmName string) (clientreg.Client, *keycloak.AdminClient) {
	adminClient := keycloak.NewAdminClient(s.adminClient, s.keycloakBaseURL, realmName)

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

// registrationClientURI is the RFC 7592 client configuration endpoint of a
// client, as served under the configured Keycloak base URL.
func (s *subroutine) registrationClientURI(realmName, clientID string) string {
	return fmt.Sprintf("%s/realms/%s/clients-registrations/openid-connect/%s", strings.TrimSuffix(s.keycloakBaseURL, "/"), realmName, clientID)
}

// refreshRegistrationClientURIs rewrites status registration client URIs that
// do not match the configured Keycloak base URL, e.g. after the base URL flag
// changed. The stored URIs would otherwise point at the old Keycloak address
// forever: they are only refreshed after a successful update, which the old
// address prevents. It reports whether any URI changed.
func (s *subroutine) refreshRegistrationClientURIs(idpConfig *pmkeycloakv1alpha1.IdentityProviderConfiguration) bool {
	changed := false
	for clientName, managedClient := range idpConfig.Status.ManagedClients {
		if managedClient.ClientID == "" {
			continue
		}
		uri := s.registrationClientURI(idpConfig.Name, managedClient.ClientID)
		if managedClient.RegistrationClientURI == uri {
			continue
		}
		managedClient.RegistrationClientURI = uri
		idpConfig.Status.ManagedClients[clientName] = managedClient
		changed = true
	}
	return changed
}

func (s *subroutine) Finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	result, err := s.finalize(ctx, obj)
	if res, ok := pendingIfClusterNotReady(ctx, err); ok {
		return res, nil
	}
	return result, err
}

func (s *subroutine) finalize(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	idpToDelete := obj.(*pmkeycloakv1alpha1.IdentityProviderConfiguration)
	log := logger.LoadLoggerFromContext(ctx)
	realmName := idpToDelete.Name

	oidcClient, adminClient := s.newOIDCClient(realmName)

	s.refreshRegistrationClientURIs(idpToDelete)

	for _, clientToDelete := range idpToDelete.Spec.Clients {
		registrationAccessToken, err := s.readRegistrationAccessToken(ctx, clientToDelete.SecretRef)
		if err != nil {
			if apierrors.IsNotFound(err) {
				log.Info().Str("secretName", clientToDelete.SecretRef.Name).Msg("Secret not found, client was likely deleted")
				continue
			}
			return subroutines.OK(), fmt.Errorf("failed to get registration access token from secret: %w", err)
		}

		clientIDToDelete := idpToDelete.Status.ManagedClients[clientToDelete.ClientName].ClientID
		if clientIDToDelete == "" {
			log.Info().Str("clientName", clientToDelete.ClientName).Msg("Client ID not found in status, skipping client deletion")
			continue
		}

		registrationClientURIToDelete := idpToDelete.Status.ManagedClients[clientToDelete.ClientName].RegistrationClientURI
		if registrationClientURIToDelete == "" {
			log.Info().Str("clientName", clientToDelete.ClientName).Msg("Registration client URI not found in status, skipping client deletion")
			continue
		}

		err = oidcClient.Delete(ctx, clientIDToDelete, registrationClientURIToDelete, registrationAccessToken)
		if err != nil && !clientreg.IsNotFound(err) {
			return subroutines.OK(), fmt.Errorf("failed to delete oidc client: %w", err)
		}

		secretToDelete := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clientToDelete.SecretRef.Name,
				Namespace: clientToDelete.SecretRef.Namespace,
			},
		}

		orgsClient, err := s.kcpClientGetter.NewClientForLogicalCluster(ctx, string(config.MultiProviderName(config.KeycloakProviderName, config.OrgsClusterPath)))
		if err != nil {
			return subroutines.OK(), fmt.Errorf("getting orgs client: %w", err)
		}
		if err := orgsClient.Delete(ctx, secretToDelete); err != nil {
			return subroutines.OK(), fmt.Errorf("failed to delete client secret: %w", err)
		}
	}

	if err := adminClient.DeleteRealm(ctx, realmName); err != nil {
		return subroutines.OK(), fmt.Errorf("failed to delete realm: %w", err)
	}

	return subroutines.OK(), nil
}

func (s *subroutine) Finalizers(_ ctrlruntimeclient.Object) []string {
	return []string{"keycloak.platform-mesh.io/idp-finalizer"}
}

func (s *subroutine) GetName() string { return "IdentityProviderConfiguration" }

func (s *subroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	result, err := s.process(ctx, obj)
	if res, ok := pendingIfClusterNotReady(ctx, err); ok {
		return res, nil
	}
	return result, err
}

func (s *subroutine) process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	idpConfig := obj.(*pmkeycloakv1alpha1.IdentityProviderConfiguration)
	log := logger.LoadLoggerFromContext(ctx)

	realmName := idpConfig.Name
	oidcClient, adminClient := s.newOIDCClient(realmName)

	if err := s.ensureRealm(ctx, adminClient, realmName, idpConfig.Spec.RegistrationAllowed, log); err != nil {
		return subroutines.OK(), err
	}

	if len(idpConfig.Status.ManagedClients) == 0 && s.adoptLegacy != nil {
		engaged, ok := mccontext.ClusterFrom(ctx)
		if !ok {
			return subroutines.OK(), fmt.Errorf("failed to get cluster from context")
		}
		adopted, err := s.adoptLegacy(ctx, engaged, idpConfig.Name)
		if err != nil {
			return subroutines.OK(), fmt.Errorf("adopting legacy IdentityProviderConfiguration: %w", err)
		}
		if len(adopted) > 0 {
			log.Info().Int("clients", len(adopted)).Msg("adopted the Keycloak clients of the deprecated core.platform-mesh.io IdentityProviderConfiguration")
			idpConfig.Status.ManagedClients = adopted
		}
	}

	// Status changes are persisted by the lifecycle after the subroutine returns,
	// also for the legacy kind the legacy adapter maps this object back to.
	if s.refreshRegistrationClientURIs(idpConfig) {
		log.Info().Str("keycloakBaseURL", s.keycloakBaseURL).Msg("Keycloak base URL changed, updating registration client URIs in status")
	}

	if err := s.deleteRemovedClients(ctx, idpConfig, oidcClient, log); err != nil {
		return subroutines.OK(), err
	}

	managedClients := make(map[string]pmkeycloakv1alpha1.ManagedClient)
	for i := range idpConfig.Spec.Clients {
		clientConfig := &idpConfig.Spec.Clients[i]

		clientInfo, err := s.registerOrUpdateClient(ctx, idpConfig, clientConfig, realmName, oidcClient, adminClient)
		if err != nil {
			return subroutines.OK(), fmt.Errorf("failed to process client %s: %w", clientConfig.ClientName, err)
		}

		if err := s.createOrUpdateSecret(ctx, clientConfig, clientInfo, idpConfig.Name); err != nil {
			return subroutines.OK(), fmt.Errorf("failed to create or update kubernetes secret: %w", err)
		}

		// Store the URI derived from the configured base URL rather than the
		// one Keycloak returns, which carries Keycloak's own hostname and would
		// be rewritten by refreshRegistrationClientURIs on every reconcile.
		managedClients[clientConfig.ClientName] = pmkeycloakv1alpha1.ManagedClient{
			ClientID:              clientInfo.ClientID,
			RegistrationClientURI: s.registrationClientURI(realmName, clientInfo.ClientID),
			SecretRef:             clientConfig.SecretRef,
		}
	}

	idpConfig.Status.ManagedClients = managedClients

	if s.mirrorLegacy != nil {
		engaged, ok := mccontext.ClusterFrom(ctx)
		if !ok {
			return subroutines.OK(), fmt.Errorf("failed to get cluster from context")
		}
		if err := s.mirrorLegacy(ctx, engaged, idpConfig.Name, idpConfig.Status); err != nil {
			return subroutines.OK(), fmt.Errorf("mirroring status to the legacy IdentityProviderConfiguration: %w", err)
		}
	}

	return subroutines.OK(), nil
}

func (s *subroutine) ensureRealm(ctx context.Context, adminClient *keycloak.AdminClient, realmName string, registrationAllowed bool, log *logger.Logger) error {
	realmConfig := keycloak.RealmConfig{
		Realm:                       realmName,
		DisplayName:                 realmName,
		Enabled:                     true,
		LoginWithEmailAllowed:       true,
		RegistrationEmailAsUsername: true,
		RegistrationAllowed:         registrationAllowed,
		SSOSessionIdleTimeout:       s.cfg.IDP.AccessTokenLifespan,
		AccessTokenLifespan:         s.cfg.IDP.AccessTokenLifespan,
	}

	if s.cfg.IDP.SMTPServer != "" {
		smtpConfig := &keycloak.SMTPConfig{
			Host:     s.cfg.IDP.SMTPServer,
			Port:     fmt.Sprintf("%d", s.cfg.IDP.SMTPPort),
			From:     s.cfg.IDP.FromAddress,
			SSL:      s.cfg.IDP.SSL,
			StartTLS: s.cfg.IDP.StartTLS,
		}

		if s.cfg.IDP.SMTPUser != "" {
			smtpConfig.Auth = true
			smtpConfig.User = s.cfg.IDP.SMTPUser
			smtpConfig.Password = s.cfg.IDP.SMTPPassword
		}

		realmConfig.SMTPServer = smtpConfig
	}

	created, err := adminClient.CreateOrUpdateRealm(ctx, realmConfig)
	if err != nil {
		return fmt.Errorf("failed to create or update realm: %w", err)
	}

	if created {
		log.Info().Str("realm", realmName).Msg("Realm created")
	} else {
		log.Info().Str("realm", realmName).Msg("Realm updated")
	}

	return nil
}

func (s *subroutine) deleteRemovedClients(ctx context.Context, idpConfig *pmkeycloakv1alpha1.IdentityProviderConfiguration, oidcClient clientreg.Client, log *logger.Logger) error {
	for clientName, managedClient := range idpConfig.Status.ManagedClients {
		exists := slices.ContainsFunc(idpConfig.Spec.Clients,
			func(c pmkeycloakv1alpha1.IdentityProviderClientConfig) bool {
				return c.ClientName == clientName
			},
		)
		if exists {
			continue
		}

		log.Info().Str("clientName", clientName).Msg("Deleting client that is no longer in spec")

		registrationAccessToken, err := s.readRegistrationAccessToken(ctx, managedClient.SecretRef)
		if err != nil {
			if apierrors.IsNotFound(err) {
				log.Info().Str("secretName", managedClient.SecretRef.Name).Msg("Secret not found, client was likely deleted")
				continue
			}
			return fmt.Errorf("failed to get registration access token from secret: %w", err)
		}

		if err := oidcClient.Delete(ctx, managedClient.ClientID, managedClient.RegistrationClientURI, registrationAccessToken); err != nil && !clientreg.IsNotFound(err) {
			return fmt.Errorf("failed to delete client %s: %w", clientName, err)
		}

		secretToDelete := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      managedClient.SecretRef.Name,
				Namespace: managedClient.SecretRef.Namespace,
			},
		}
		orgsClient, err := s.kcpClientGetter.NewClientForLogicalCluster(ctx, string(config.MultiProviderName(config.KeycloakProviderName, config.OrgsClusterPath)))
		if err != nil {
			return fmt.Errorf("getting orgs client: %w", err)
		}
		if err := orgsClient.Delete(ctx, secretToDelete); err != nil {
			return fmt.Errorf("failed to delete client secret %s: %w", managedClient.SecretRef.Name, err)
		}
	}

	return nil
}

func (s *subroutine) registerOrUpdateClient(ctx context.Context, ipc *pmkeycloakv1alpha1.IdentityProviderConfiguration, clientConfig *pmkeycloakv1alpha1.IdentityProviderClientConfig, realmName string, oidcClient clientreg.Client, adminClient *keycloak.AdminClient) (clientreg.ClientInformation, error) {
	existingClient, err := adminClient.GetClientByName(ctx, clientConfig.ClientName)
	if err != nil {
		return clientreg.ClientInformation{}, fmt.Errorf("failed to check if client exists: %w", err)
	}

	authMethod := clientreg.TokenEndpointAuthMethodClientSecretBasic
	if clientConfig.ClientType == pmkeycloakv1alpha1.IdentityProviderClientTypePublic {
		authMethod = clientreg.TokenEndpointAuthMethodNone
	}

	metadata := clientreg.ClientMetadata{
		ClientName:              clientConfig.ClientName,
		RedirectURIs:            clientConfig.RedirectURIs,
		GrantTypes:              []string{clientreg.GrantTypeAuthorizationCode, clientreg.GrantTypeRefreshToken},
		TokenEndpointAuthMethod: authMethod,
		PostLogoutRedirectURIs:  clientConfig.PostLogoutRedirectURIs,
	}

	if existingClient == nil {
		return oidcClient.Register(ctx, adminClient.RegistrationEndpoint(), metadata)
	}

	// Client exists, update it
	registrationAccessToken, err := s.readRegistrationAccessToken(ctx, clientConfig.SecretRef)
	if err != nil && !apierrors.IsNotFound(err) {
		return clientreg.ClientInformation{}, fmt.Errorf("failed to get registration access token from secret: %w", err)
	}

	registrationClientURI := ipc.Status.ManagedClients[clientConfig.ClientName].RegistrationClientURI
	if registrationClientURI == "" {
		registrationClientURI = s.registrationClientURI(realmName, existingClient.ClientID)
	}

	metadata.ClientID = ipc.Status.ManagedClients[clientConfig.ClientName].ClientID
	if metadata.ClientID == "" {
		metadata.ClientID = existingClient.ClientID
	}

	return oidcClient.Update(ctx, registrationClientURI, registrationAccessToken, metadata)
}

func (s *subroutine) readRegistrationAccessToken(ctx context.Context, secretRef corev1.SecretReference) (string, error) {
	secret := &corev1.Secret{}
	key := ctrlruntimeclient.ObjectKey{Name: secretRef.Name, Namespace: secretRef.Namespace}
	orgsClient, err := s.kcpClientGetter.NewClientForLogicalCluster(ctx, string(config.MultiProviderName(config.KeycloakProviderName, config.OrgsClusterPath)))
	if err != nil {
		return "", fmt.Errorf("getting orgs client: %w", err)
	}
	if err := orgsClient.Get(ctx, key, secret); err != nil {
		return "", err
	}
	return string(secret.Data["registration_access_token"]), nil
}

func (s *subroutine) createOrUpdateSecret(ctx context.Context, clientConfig *pmkeycloakv1alpha1.IdentityProviderClientConfig, clientInfo clientreg.ClientInformation, idpName string) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clientConfig.SecretRef.Name,
			Namespace: clientConfig.SecretRef.Namespace,
			Labels: map[string]string{
				"keycloak.platform-mesh.io/idp-name":    idpName,
				"keycloak.platform-mesh.io/client-name": clientConfig.ClientName,
			},
		},
	}

	orgsClient, err := s.kcpClientGetter.NewClientForLogicalCluster(ctx, string(config.MultiProviderName(config.KeycloakProviderName, config.OrgsClusterPath)))
	if err != nil {
		return fmt.Errorf("getting orgs client: %w", err)
	}
	_, err = controllerutil.CreateOrUpdate(ctx, orgsClient, secret, func() error {
		if secret.Data == nil {
			secret.Data = make(map[string][]byte)
		}
		if clientInfo.ClientSecret != "" {
			secret.Data["client_secret"] = []byte(clientInfo.ClientSecret)
		}
		if clientInfo.RegistrationAccessToken != "" {
			secret.Data["registration_access_token"] = []byte(clientInfo.RegistrationAccessToken)
		}
		secret.Type = corev1.SecretTypeOpaque
		return nil
	})

	return err
}
