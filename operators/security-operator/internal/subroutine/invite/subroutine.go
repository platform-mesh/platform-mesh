package invite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/coreos/go-oidc"
	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/logger"
	"golang.org/x/oauth2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/platform-mesh/security-operator/api/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/config"
)

const (
	RequiredActionVerifyEmail    string = "VERIFY_EMAIL"
	RequiredActionUpdatePassword string = "UPDATE_PASSWORD"
)

type subroutine struct {
	keycloakBaseURL string
	baseDomain      string
	keycloak        *http.Client
	mgr             mcmanager.Manager
}

type keycloakUser struct {
	ID              string   `json:"id,omitempty"`
	Email           string   `json:"email,omitempty"`
	RequiredActions []string `json:"requiredActions,omitempty"`
	Enabled         bool     `json:"enabled,omitempty"`
}

type keycloakClient struct {
	ID       string `json:"id,omitempty"`
	ClientID string `json:"clientId,omitempty"`
}

func New(ctx context.Context, cfg *config.Config, mgr mcmanager.Manager, pwd string) (*subroutine, error) {

	issuer := fmt.Sprintf("%s/realms/master", cfg.Invite.KeycloakBaseURL)
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}

	config := oauth2.Config{
		ClientID: cfg.Invite.KeycloakClientID,
		Endpoint: provider.Endpoint(),
	}

	token, err := config.PasswordCredentialsToken(ctx, cfg.Invite.KeycloakUser, pwd)
	if err != nil {
		return nil, err
	}

	return &subroutine{
		keycloakBaseURL: cfg.Invite.KeycloakBaseURL,
		baseDomain:      cfg.BaseDomain,
		mgr:             mgr,
		keycloak:        config.Client(ctx, token),
	}, nil
}

// Finalize implements subroutine.Subroutine.
func (s *subroutine) Finalize(ctx context.Context, instance runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	return ctrl.Result{}, nil
}

// Finalizers implements subroutine.Subroutine.
func (s *subroutine) Finalizers(_ runtimeobject.RuntimeObject) []string { return []string{} }

// GetName implements subroutine.Subroutine.
func (s *subroutine) GetName() string { return "Invite" }

// Process implements subroutine.Subroutine.
func (s *subroutine) Process(ctx context.Context, instance runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	invite := instance.(*v1alpha1.Invite)
	log := logger.LoadLoggerFromContext(ctx)

	log.Debug().Str("email", invite.Spec.Email).Msg("Processing invite")

	v := url.Values{
		"email":               {invite.Spec.Email},
		"max":                 {"1"},
		"briefRepresentation": {"true"},
	}

	clusterName, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to get cluster from context"), true, false)
	}

	cluster, err := s.mgr.GetCluster(ctx, clusterName)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to get cluster %q: %w", clusterName, err), true, false)
	}

	cl := cluster.GetClient()

	var accountInfo corev1alpha1.AccountInfo
	if err := cl.Get(ctx, client.ObjectKey{Name: "account"}, &accountInfo); err != nil {
		log.Err(err).Msg("Failed to get AccountInfo")
		return ctrl.Result{}, errors.NewOperatorError(err, true, false)
	}

	realm := accountInfo.Spec.Organization.Name
	if realm == "" {
		log.Error().Msg("Organization name is empty in AccountInfo")
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("organization name is empty in AccountInfo"), true, false)
	}

	res, err := s.keycloak.Get(fmt.Sprintf("%s/admin/realms/%s/users?%s", s.keycloakBaseURL, realm, v.Encode()))
	if err != nil { // coverage-ignore
		log.Err(err).Msg("Failed to query users")
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}
	defer res.Body.Close() //nolint:errcheck
	if res.StatusCode != http.StatusOK {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to query users: %s", res.Status), true, true)
	}

	var users []keycloakUser
	if err = json.NewDecoder(res.Body).Decode(&users); err != nil { // coverage-ignore
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	if len(users) != 0 {
		log.Info().Str("email", invite.Spec.Email).Msg("User already exists, skipping invite")
		return ctrl.Result{}, nil
	}

	log.Info().Str("email", invite.Spec.Email).Msg("User does not exist, creating user and sending invite")

	// Create user
	newUser := keycloakUser{
		Email:           invite.Spec.Email,
		RequiredActions: []string{RequiredActionUpdatePassword, RequiredActionVerifyEmail},
		Enabled:         true,
	}

	var buffer bytes.Buffer
	if err = json.NewEncoder(&buffer).Encode(&newUser); err != nil { // coverage-ignore
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	res, err = s.keycloak.Post(fmt.Sprintf("%s/admin/realms/%s/users", s.keycloakBaseURL, realm), "application/json", &buffer)
	if err != nil { // coverage-ignore
		log.Err(err).Msg("Failed to create user")
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}
	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode != http.StatusCreated {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to create user: %s", res.Status), true, true)
	}

	res, err = s.keycloak.Get(fmt.Sprintf("%s/admin/realms/%s/users?%s", s.keycloakBaseURL, realm, v.Encode()))
	if err != nil { // coverage-ignore
		log.Err(err).Msg("Failed to query users")
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}
	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode != http.StatusOK {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to query users: %s", res.Status), true, true)
	}

	if err = json.NewDecoder(res.Body).Decode(&users); err != nil { // coverage-ignore
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	newUser = users[0]

	log.Debug().Str("email", invite.Spec.Email).Str("id", newUser.ID).Msg("User created")

	clientQueryParams := url.Values{
		"clientId": {realm},
	}

	res, err = s.keycloak.Get(fmt.Sprintf("%s/admin/realms/%s/clients?%s", s.keycloakBaseURL, realm, clientQueryParams.Encode()))
	if err != nil {
		log.Err(err).Msg("Failed to verify client exists")
		return ctrl.Result{}, errors.NewOperatorError(err, true, false)
	}
	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode != http.StatusOK {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to verify client exists: %s", res.Status), true, false)
	}

	var clients []keycloakClient
	if err = json.NewDecoder(res.Body).Decode(&clients); err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, false)
	}

	if len(clients) == 0 {
		log.Info().Str("clientId", realm).Msg("Client does not exist yet, requeuing")
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("client %s does not exist yet", realm), true, false)
	}

	log.Debug().Str("clientId", realm).Msg("Client verified")

	queryParams := url.Values{
		"redirect_uri": {fmt.Sprintf("https://%s.%s/", realm, s.baseDomain)},
		"client_id":    {realm},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, fmt.Sprintf("%s/admin/realms/%s/users/%s/execute-actions-email?%s", s.keycloakBaseURL, realm, newUser.ID, queryParams.Encode()), http.NoBody)
	if err != nil { // coverage-ignore
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	res, err = s.keycloak.Do(req)
	if err != nil { // coverage-ignore
		log.Err(err).Msg("Failed to send invite email")
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}
	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode != http.StatusNoContent {
		return ctrl.Result{}, errors.NewOperatorError(fmt.Errorf("failed to send invite email: %s", res.Status), true, true)
	}

	log.Info().Str("email", invite.Spec.Email).Msg("User created and invite sent")

	return ctrl.Result{}, nil
}

var _ lifecyclesubroutine.Subroutine = &subroutine{}
