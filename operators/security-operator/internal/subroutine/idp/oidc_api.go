package idp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/platform-mesh/security-operator/api/v1alpha1"
)

type ClientRegistrationRequest struct {
	ClientID                string   `json:"client_id,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	PostLogoutRedirectURIs  []string `json:"post_logout_redirect_uris,omitempty"`
}

type clientInfo struct {
	Secret                  string `json:"client_secret,omitempty"`
	RegistrationAccessToken string `json:"registration_access_token,omitempty"`
	RegistrationClientURI   string `json:"registration_client_uri,omitempty"`
	ClientID                string `json:"client_id,omitempty"`
}

func (s *subroutine) registerClient(ctx context.Context, clientConfig v1alpha1.IdentityProviderClientConfig, realmName string, initialAccessToken string) (clientInfo, error) {
	payload := ClientRegistrationRequest{
		ClientName:             clientConfig.ClientName,
		RedirectURIs:           clientConfig.RedirectURIs,
		GrantTypes:             []string{"authorization_code", "refresh_token"},
		PostLogoutRedirectURIs: clientConfig.PostLogoutRedirectURIs,
	}

	payload.TokenEndpointAuthMethod = "client_secret_basic"

	if clientConfig.ClientType == v1alpha1.IdentityProviderClientTypePublic {
		payload.TokenEndpointAuthMethod = "none"
	}

	body, err := json.Marshal(payload)
	if err != nil { // coverage-ignore
		return clientInfo{}, fmt.Errorf("failed to marshal client registration request payload: %w", err)
	}

	url := fmt.Sprintf("%s/realms/%s/clients-registrations/openid-connect", s.keycloakBaseURL, realmName)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil { // coverage-ignore
		return clientInfo{}, fmt.Errorf("failed to build client registration request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", initialAccessToken))

	res, err := s.oidc.Do(req)
	if err != nil { // coverage-ignore
		return clientInfo{}, fmt.Errorf("failed to register oidc client: %w", err)
	}
	defer res.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(res.Body)
	if err != nil { // coverage-ignore
		return clientInfo{}, fmt.Errorf("failed to read response")
	}

	if res.StatusCode != http.StatusCreated {
		return clientInfo{}, fmt.Errorf("failed to register oidc client: status %d body: %s", res.StatusCode, respBody)
	}

	var resp clientInfo
	if err := json.Unmarshal(respBody, &resp); err != nil { // coverage-ignore
		return clientInfo{}, fmt.Errorf("failed to parse client registration response: %w body: %s", err, respBody)
	}

	return resp, nil
}

func (s *subroutine) executeUpdateRequest(ctx context.Context, registrationClientURI, token string, body []byte) (clientInfo, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, registrationClientURI, bytes.NewBuffer(body))
	if err != nil { // coverage-ignore
		return clientInfo{}, 0, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}

	res, err := s.oidc.Do(req)
	if err != nil { // coverage-ignore
		return clientInfo{}, 0, fmt.Errorf("failed to update client: %w", err)
	}
	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode != http.StatusOK {
		return clientInfo{}, res.StatusCode, fmt.Errorf("failed to update client: status %d", res.StatusCode)
	}

	respBody, err := io.ReadAll(res.Body)
	if err != nil { // coverage-ignore
		return clientInfo{}, res.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}

	var resp clientInfo
	if err := json.Unmarshal(respBody, &resp); err != nil { // coverage-ignore
		return clientInfo{}, res.StatusCode, fmt.Errorf("failed to parse response: %w body: %s", err, respBody)
	}

	return resp, res.StatusCode, nil
}

func (s *subroutine) updateClient(ctx context.Context, clientConfig v1alpha1.IdentityProviderClientConfig, realmName, registrationAccessToken string) (clientInfo, error) {
	payload := ClientRegistrationRequest{
		ClientID:                clientConfig.ClientID,
		ClientName:              clientConfig.ClientName,
		RedirectURIs:            clientConfig.RedirectURIs,
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		TokenEndpointAuthMethod: "client_secret_basic",
		PostLogoutRedirectURIs:  clientConfig.PostLogoutRedirectURIs,
	}

	if clientConfig.ClientType == v1alpha1.IdentityProviderClientTypePublic {
		payload.TokenEndpointAuthMethod = "none"
	}

	body, err := json.Marshal(payload)
	if err != nil { // coverage-ignore
		return clientInfo{}, fmt.Errorf("failed to marshal client update payload: %w", err)
	}

	info, statusCode, err := s.executeUpdateRequest(ctx, clientConfig.RegistrationClientURI, registrationAccessToken, body)
	if err == nil {
		return info, nil
	}

	if statusCode == http.StatusUnauthorized {
		newToken, err := s.regenerateRegistrationAccessToken(ctx, realmName, clientConfig.ClientID)
		if err != nil {
			return clientInfo{}, fmt.Errorf("failed to regenerate token after 401: %w", err)
		}

		info, _, err = s.executeUpdateRequest(ctx, clientConfig.RegistrationClientURI, newToken, body)
		if err != nil {
			return clientInfo{}, fmt.Errorf("failed to retry update after token regeneration: %w", err)
		}

		return info, nil
	}

	return clientInfo{}, err
}

func (s *subroutine) deleteClient(ctx context.Context, realmName, clientID, registrationClientURI, registrationAccessToken string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, registrationClientURI, nil)
	if err != nil { // coverage-ignore
		return fmt.Errorf("failed to build delete request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", registrationAccessToken))

	res, err := s.oidc.Do(req)
	if err != nil { // coverage-ignore
		return fmt.Errorf("client delete call failed: %w", err)
	}
	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("client delete failed with status %d", res.StatusCode)
	}

	if res.StatusCode == http.StatusUnauthorized {
		newToken, err := s.regenerateRegistrationAccessToken(ctx, realmName, clientID)
		if err != nil {
			return fmt.Errorf("failed to regenerate token after 401: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, registrationClientURI, nil)
		if err != nil { // coverage-ignore
			return fmt.Errorf("failed to build retry delete request: %w", err)
		}
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", newToken))

		res, err = s.oidc.Do(req)
		if err != nil { // coverage-ignore
			return fmt.Errorf("client delete retry call failed: %w", err)
		}
		defer res.Body.Close() //nolint:errcheck
	}

	return nil
}
