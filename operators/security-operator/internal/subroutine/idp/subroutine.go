package idp

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/coreos/go-oidc"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/platform-mesh/security-operator/api/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/config"
	"golang.org/x/oauth2/clientcredentials"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
)

type subroutine struct {
	keycloakBaseURL string
	keycloak        *http.Client
	oidc            *http.Client
	orgsClient      client.Client
	mgr             mcmanager.Manager
	cfg             *config.Config
}

func New(ctx context.Context, cfg *config.Config, orgsClient client.Client, mgr mcmanager.Manager) (*subroutine, error) {
	issuer := fmt.Sprintf("%s/realms/master", cfg.Invite.KeycloakBaseURL)
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}

	cCfg := clientcredentials.Config{
		ClientID:     cfg.Invite.KeycloakClientID,
		ClientSecret: cfg.Invite.KeycloakClientSecret,
		TokenURL:     provider.Endpoint().TokenURL,
	}

	httpClient := cCfg.Client(ctx)

	oidcClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	return &subroutine{
		keycloakBaseURL: cfg.Invite.KeycloakBaseURL,
		keycloak:        httpClient,
		oidc:            oidcClient,
		orgsClient:      orgsClient,
		mgr:             mgr,
		cfg:             cfg,
	}, nil
}

// Finalize implements subroutine.Subroutine.
func (s *subroutine) Finalize(ctx context.Context, instance runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	idpToDelete := instance.(*v1alpha1.IdentityProviderConfiguration)
	log := logger.LoadLoggerFromContext(ctx)

	for _, clientToDelete := range idpToDelete.Spec.Clients {
		registrationAccessToken, err := s.readRegistrationAccessTokenFromSecret(ctx, clientToDelete.SecretRef)
		if err != nil {
			if kerrors.IsNotFound(err) {
				log.Info().Str("secretName", clientToDelete.SecretRef.Name).Msg("Secret not found, client was likely deleted")
				continue
			}
			return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to get registration access token from secret: %w", err), true, true)
		}

		err = s.deleteClient(ctx, idpToDelete.Name, clientToDelete.ClientID, clientToDelete.RegistrationClientURI, registrationAccessToken)
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to delete oidc client %w", err), true, false)
		}

		secretToDelete := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clientToDelete.SecretRef.Name,
				Namespace: clientToDelete.SecretRef.Namespace,
			},
		}
		if err := s.orgsClient.Delete(ctx, secretToDelete); err != nil {
			return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to delete client secret %w", err), true, false)
		}
	}

	err := s.deleteRealm(ctx, idpToDelete.Name)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to delete realm %w", err), true, false)
	}

	return ctrl.Result{}, nil
}

// Finalizers implements subroutine.Subroutine.
func (s *subroutine) Finalizers(_ runtimeobject.RuntimeObject) []string {
	return []string{"core.platform-mesh.io/idp-finalizer"}
}

// GetName implements subroutine.Subroutine.
func (s *subroutine) GetName() string { return "IdentityProviderConfiguration" }

// Process implements subroutine.Subroutine.
func (s *subroutine) Process(ctx context.Context, instance runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	IdentityProviderConfiguration := instance.(*v1alpha1.IdentityProviderConfiguration)
	log := logger.LoadLoggerFromContext(ctx)

	cl, err := s.mgr.ClusterFromContext(ctx)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to get cluster from context %w", err), true, true)
	}

	realmName := IdentityProviderConfiguration.Name
	realm := realm{
		Realm:                       realmName,
		DisplayName:                 realmName,
		Enabled:                     true,
		LoginWithEmailAllowed:       true,
		RegistrationEmailAsUsername: true,
	}

	if s.cfg.IDP.SMTPServer != "" {
		smtpConfig := &smtpServer{
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

		realm.SMTPServer = smtpConfig
	}

	if err := s.createOrUpdateRealm(ctx, realm, log); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, false)
	}

	for clientName, managedClient := range IdentityProviderConfiguration.Status.ManagedClients {
		exists := slices.ContainsFunc(IdentityProviderConfiguration.Spec.Clients,
			func(c v1alpha1.IdentityProviderClientConfig) bool {
				return c.ClientName == clientName
			},
		)

		if !exists {
			log.Info().Str("clientName", clientName).Msg("Deleting client that is no longer in spec")

			registrationAccessToken, err := s.readRegistrationAccessTokenFromSecret(ctx, managedClient.SecretRef)
			if err != nil {
				if kerrors.IsNotFound(err) {
					log.Info().Str("secretName", managedClient.SecretRef.Name).Msg("Secret not found, client was likely deleted")
					continue
				}
			}

			if err := s.deleteClient(ctx, realmName, managedClient.ClientID, managedClient.RegistrationClientURI, registrationAccessToken); err != nil {
				return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to delete client %s: %w", clientName, err), true, false)
			}

			secretToDelete := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      managedClient.SecretRef.Name,
					Namespace: managedClient.SecretRef.Namespace,
				},
			}
			if err := s.orgsClient.Delete(ctx, secretToDelete); err != nil {
				return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to delete client secret %s: %w", managedClient.SecretRef.Name, err), true, false)
			}
		}
	}

	managedClients := make(map[string]v1alpha1.ManagedClient)
	for _, clientConfig := range IdentityProviderConfiguration.Spec.Clients {

		clientID, err := s.getClientID(ctx, realmName, clientConfig.ClientName)
		if err != nil {
			return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to get client id from keycloak: %w", err), true, true)
		}

		var clientInfo clientInfo
		if clientID != "" {
			registrationAccessToken, err := s.readRegistrationAccessTokenFromSecret(ctx, clientConfig.SecretRef)
			if err != nil {
				return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to get registration access token from secret: %w", err), true, true)
			}

			if clientConfig.RegistrationClientURI == "" {
				clientConfig.RegistrationClientURI = fmt.Sprintf("%s/realms/%s/clients-registrations/openid-connect/%s", s.keycloakBaseURL, realmName, clientID)
			}

			if clientConfig.ClientID == "" {
				clientConfig.ClientID = clientID
			}

			clientInfo, err = s.updateClient(ctx, clientConfig, realmName, registrationAccessToken)
			if err != nil {
				return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to update client: %w", err), true, true)
			}
		} else {
			iat, err := s.getInitialAccessToken(ctx, realmName)
			if err != nil {
				return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to get Initial Access Token: %w", err), true, true)
			}

			clientInfo, err = s.registerClient(ctx, clientConfig, realmName, iat)
			if err != nil {
				return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to register client: %w", err), true, true)
			}
		}

		if err := s.patchIdentityProviderConfiguration(ctx, cl.GetClient(), IdentityProviderConfiguration, clientConfig.ClientName, clientInfo.ClientID, clientInfo.RegistrationClientURI); err != nil {
			return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to set ClientID and RegistrationClientURI in IDP resource: %w", err), true, true)
		}

		if err := s.createOrUpdateSecret(ctx, clientConfig, clientInfo, IdentityProviderConfiguration.Name); err != nil {
			return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to create or update kubernetes secret: %w", err), true, true)
		}

		managedClients[clientConfig.ClientName] = v1alpha1.ManagedClient{
			ClientID:              clientInfo.ClientID,
			RegistrationClientURI: clientInfo.RegistrationClientURI,
			SecretRef:             clientConfig.SecretRef,
		}
	}

	if err := s.patchIdentityProviderConfigurationStatus(ctx, cl.GetClient(), IdentityProviderConfiguration, managedClients); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to update status with managed clients: %w", err), true, true)
	}

	return ctrl.Result{}, nil
}

func (s *subroutine) readRegistrationAccessTokenFromSecret(ctx context.Context, secretRef corev1.SecretReference) (string, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretRef.Name,
			Namespace: secretRef.Namespace,
		},
	}

	if err := s.orgsClient.Get(ctx, client.ObjectKeyFromObject(secret), secret); err != nil {
		return "", fmt.Errorf("failed to get secret: %w", err)
	}

	return string(secret.Data["registration_access_token"]), nil
}

func (s *subroutine) createOrUpdateSecret(ctx context.Context, clientConfig v1alpha1.IdentityProviderClientConfig, clientInfo clientInfo, idpName string) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clientConfig.SecretRef.Name,
			Namespace: clientConfig.SecretRef.Namespace,
			Labels: map[string]string{
				"core.platform-mesh.io/idp-name":    idpName,
				"core.platform-mesh.io/client-name": clientConfig.ClientName,
			},
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, s.orgsClient, secret, func() error {
		if secret.Data == nil {
			secret.Data = make(map[string][]byte)
		}
		if clientInfo.Secret != "" {
			secret.Data["client_secret"] = []byte(clientInfo.Secret)
		}
		if clientInfo.RegistrationAccessToken != "" {
			secret.Data["registration_access_token"] = []byte(clientInfo.RegistrationAccessToken)
		}
		secret.Type = corev1.SecretTypeOpaque
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create or update kubernetes secret: %w", err)
	}
	return nil
}

func (s *subroutine) patchIdentityProviderConfiguration(ctx context.Context, kcpClient client.Client, idpConfig *v1alpha1.IdentityProviderConfiguration, clientName, clientID, registrationClientURI string) error {
	for i := range idpConfig.Spec.Clients {
		c := &idpConfig.Spec.Clients[i]
		if c.ClientName != clientName {
			continue
		}

		original := idpConfig.DeepCopy()
		c.ClientID = clientID

		if registrationClientURI != "" {
			c.RegistrationClientURI = registrationClientURI
		}

		if err := kcpClient.Patch(ctx, idpConfig, client.MergeFrom(original)); err != nil {
			return fmt.Errorf("failed to patch IdentityProviderConfiguration: %w", err)
		}
		return nil
	}

	return fmt.Errorf("client %s not found in IdentityProviderConfiguration spec", clientName)
}

func (s *subroutine) patchIdentityProviderConfigurationStatus(ctx context.Context, kcpClient client.Client, idpConfig *v1alpha1.IdentityProviderConfiguration, managedClients map[string]v1alpha1.ManagedClient) error {
	original := idpConfig.DeepCopy()
	idpConfig.Status.ManagedClients = managedClients

	if err := kcpClient.Status().Patch(ctx, idpConfig, client.MergeFrom(original)); err != nil { // coverage-ignore
		return fmt.Errorf("failed to patch IdentityProviderConfiguration status: %w", err) // coverage-ignore
	}
	return nil
}
