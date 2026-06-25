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

package invite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/coreos/go-oidc"
	"golang.org/x/oauth2/clientcredentials"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/controller/lifecycle/ratelimiter"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/subroutines"

	"k8s.io/client-go/util/workqueue"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
)

const (
	RequiredActionVerifyEmail    string = "VERIFY_EMAIL"
	RequiredActionUpdatePassword string = "UPDATE_PASSWORD"
	UserDefaultPasswordType      string = "password"
	UserDefaultPasswordValue     string = "password"
)

type subroutine struct {
	keycloakBaseURL    string
	baseDomain         string
	keycloak           *http.Client
	kcpClientGetter    client.KCPClientGetter
	setDefaultPassword bool
	limiter            workqueue.TypedRateLimiter[*pmcorev1alpha1.Invite]
}

type keycloakUser struct {
	ID              string               `json:"id,omitempty"`
	Email           string               `json:"email,omitempty"`
	RequiredActions []string             `json:"requiredActions,omitempty"`
	Enabled         bool                 `json:"enabled,omitempty"`
	Credentials     []keycloakCredential `json:"credentials,omitempty"`
}

type keycloakCredential struct {
	Type      string `json:"type"`
	Value     string `json:"value"`
	Temporary bool   `json:"temporary"`
}

type keycloakClient struct {
	ID       string `json:"id,omitempty"`
	ClientID string `json:"clientId,omitempty"`
}

func New(ctx context.Context, cfg *config.Config, kcpClientGetter client.KCPClientGetter) (*subroutine, error) {
	issuer := fmt.Sprintf("%s/realms/master", cfg.Keycloak.BaseURL)
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("creating OIDC provider: %w", err)
	}

	cCfg := clientcredentials.Config{
		ClientID:     cfg.Keycloak.ClientID,
		ClientSecret: cfg.Keycloak.ClientSecret,
		TokenURL:     provider.Endpoint().TokenURL,
	}

	httpClient := cCfg.Client(ctx)

	lim, err := ratelimiter.NewStaticThenExponentialRateLimiter[*pmcorev1alpha1.Invite](
		ratelimiter.NewConfig())
	if err != nil {
		return nil, fmt.Errorf("creating RateLimiter: %w", err)
	}

	return &subroutine{
		keycloakBaseURL:    cfg.Keycloak.BaseURL,
		baseDomain:         cfg.BaseDomain,
		keycloak:           httpClient,
		kcpClientGetter:    kcpClientGetter,
		setDefaultPassword: cfg.SetDefaultPassword,
		limiter:            lim,
	}, nil
}

var _ subroutines.Processor = &subroutine{}

func (s *subroutine) GetName() string { return "Invite" }

func (s *subroutine) Process(ctx context.Context, obj ctrlruntimeclient.Object) (subroutines.Result, error) {
	invite := obj.(*pmcorev1alpha1.Invite)
	log := logger.LoadLoggerFromContext(ctx)

	log.Debug().Str("email", invite.Spec.Email).Msg("Processing invite")

	v := url.Values{
		"email":               {invite.Spec.Email},
		"max":                 {"1"},
		"briefRepresentation": {"true"},
	}

	clusterName, ok := mccontext.ClusterFrom(ctx)
	if !ok {
		return subroutines.OK(), fmt.Errorf("failed to get cluster from context")
	}

	cl, err := s.kcpClientGetter.NewClientForLogicalCluster(ctx, string(clusterName))
	if err != nil {
		return subroutines.OK(), fmt.Errorf("failed to get client for cluster %q: %w", clusterName, err)
	}

	var accountInfo pmcorev1alpha1.AccountInfo
	if err := cl.Get(ctx, ctrlruntimeclient.ObjectKey{Name: "account"}, &accountInfo); err != nil {
		log.Err(err).Msg("Failed to get AccountInfo")
		return subroutines.OK(), err
	}

	realm := accountInfo.Spec.Organization.Name
	if realm == "" {
		log.Error().Msg("Organization name is empty in AccountInfo")
		return subroutines.OK(), fmt.Errorf("organization name is empty in AccountInfo")
	}

	res, err := s.keycloak.Get(fmt.Sprintf("%s/admin/realms/%s/users?%s", s.keycloakBaseURL, realm, v.Encode()))
	if err != nil {
		log.Err(err).Msg("Failed to query users")
		return subroutines.OK(), err
	}
	defer res.Body.Close() //nolint:errcheck
	if res.StatusCode != http.StatusOK {
		return subroutines.OK(), fmt.Errorf("failed to query users: %s", res.Status)
	}

	var users []keycloakUser
	if err = json.NewDecoder(res.Body).Decode(&users); err != nil {
		return subroutines.OK(), err
	}

	if len(users) != 0 {
		log.Info().Str("email", invite.Spec.Email).Msg("User already exists, skipping invite")
		s.limiter.Forget(invite)
		return subroutines.OK(), nil
	}

	log.Info().Str("email", invite.Spec.Email).Msg("User does not exist, creating user and sending invite")

	if accountInfo.Spec.OIDC == nil {
		return subroutines.OK(), fmt.Errorf("AccountInfo OIDC is not configured yet")
	}

	oidcClient, ok := accountInfo.Spec.OIDC.Clients[realm]
	if !ok {
		return subroutines.OK(), fmt.Errorf("failed to get oidc client for organization %s", realm)
	}

	clientQueryParams := url.Values{
		"clientId": {oidcClient.ClientID},
	}

	res, err = s.keycloak.Get(fmt.Sprintf("%s/admin/realms/%s/clients?%s", s.keycloakBaseURL, realm, clientQueryParams.Encode()))
	if err != nil {
		log.Err(err).Msg("Failed to verify client exists")
		return subroutines.OK(), err
	}
	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode != http.StatusOK {
		return subroutines.OK(), fmt.Errorf("failed to verify client exists: %s", res.Status)
	}

	var clients []keycloakClient
	if err = json.NewDecoder(res.Body).Decode(&clients); err != nil {
		return subroutines.OK(), err
	}

	if len(clients) == 0 {
		log.Info().Str("clientId", oidcClient.ClientID).Msg("Client does not exist yet, requeuing")
		return subroutines.StopWithRequeue(s.limiter.When(invite), "client does not exist yet"), nil
	}

	log.Debug().Str("clientId", oidcClient.ClientID).Msg("Client verified")

	// Create user
	newUser := keycloakUser{
		Email:           invite.Spec.Email,
		RequiredActions: []string{RequiredActionUpdatePassword, RequiredActionVerifyEmail},
		Enabled:         true,
	}

	if s.setDefaultPassword {
		newUser.RequiredActions = []string{RequiredActionUpdatePassword}
		newUser.Credentials = []keycloakCredential{
			{
				Type:      UserDefaultPasswordType,
				Value:     UserDefaultPasswordValue,
				Temporary: true,
			},
		}
	}

	var buffer bytes.Buffer
	if err = json.NewEncoder(&buffer).Encode(&newUser); err != nil {
		return subroutines.OK(), err
	}

	res, err = s.keycloak.Post(fmt.Sprintf("%s/admin/realms/%s/users", s.keycloakBaseURL, realm), "application/json", &buffer)
	if err != nil {
		return subroutines.OK(), fmt.Errorf("posting to Keycloak to create user: %w", err)
	}
	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode != http.StatusCreated {
		return subroutines.OK(), fmt.Errorf("keycloak returned non-201 status code: %s", res.Status)
	}

	res, err = s.keycloak.Get(fmt.Sprintf("%s/admin/realms/%s/users?%s", s.keycloakBaseURL, realm, v.Encode()))
	if err != nil {
		log.Err(err).Msg("Failed to query users")
		return subroutines.OK(), err
	}
	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode != http.StatusOK {
		return subroutines.OK(), fmt.Errorf("failed to query users: %s", res.Status)
	}

	if err = json.NewDecoder(res.Body).Decode(&users); err != nil {
		return subroutines.OK(), err
	}

	newUser = users[0]

	log.Debug().Str("email", invite.Spec.Email).Str("id", newUser.ID).Msg("User created")

	queryParams := url.Values{
		"redirect_uri": {fmt.Sprintf("https://%s.%s/", realm, s.baseDomain)},
		"client_id":    {oidcClient.ClientID},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, fmt.Sprintf("%s/admin/realms/%s/users/%s/execute-actions-email?%s", s.keycloakBaseURL, realm, newUser.ID, queryParams.Encode()), http.NoBody)
	if err != nil {
		return subroutines.OK(), err
	}

	res, err = s.keycloak.Do(req)
	if err != nil {
		log.Err(err).Msg("Failed to send invite email")
		return subroutines.OK(), err
	}
	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode != http.StatusNoContent {
		return subroutines.OK(), fmt.Errorf("failed to send invite email: %s", res.Status)
	}

	log.Info().Str("email", invite.Spec.Email).Msg("User created and invite sent")

	s.limiter.Forget(invite)
	return subroutines.OK(), nil
}

var _ subroutines.Subroutine = &subroutine{}
