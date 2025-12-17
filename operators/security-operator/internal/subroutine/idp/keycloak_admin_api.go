package idp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/platform-mesh/golang-commons/logger"
)

type realm struct {
	Realm                       string      `json:"realm"`
	DisplayName                 string      `json:"displayName,omitempty"`
	Enabled                     bool        `json:"enabled"`
	LoginWithEmailAllowed       bool        `json:"loginWithEmailAllowed,omitempty"`
	RegistrationEmailAsUsername bool        `json:"registrationEmailAsUsername,omitempty"`
	SMTPServer                  *smtpServer `json:"smtpServer,omitempty"`
}

type smtpServer struct {
	Host     string `json:"host,omitempty"`
	Port     string `json:"port,omitempty"`
	From     string `json:"from,omitempty"`
	SSL      bool   `json:"ssl,omitempty"`
	StartTLS bool   `json:"starttls,omitempty"`
	Auth     bool   `json:"auth,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
}

func (s *subroutine) createOrUpdateRealm(ctx context.Context, realm realm, log *logger.Logger) error {
	realmJSON, err := json.Marshal(realm)
	if err != nil { // coverage-ignore
		return fmt.Errorf("failed to marshal realm data: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/admin/realms", s.keycloakBaseURL), bytes.NewBuffer(realmJSON))
	if err != nil { // coverage-ignore
		return fmt.Errorf("failed to create realm creation request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := s.keycloak.Do(req)
	if err != nil { // coverage-ignore
		return fmt.Errorf("failed to create realm: %w", err)
	}
	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusConflict {
		return fmt.Errorf("failed to create realm: status %d", res.StatusCode)
	}

	if res.StatusCode == http.StatusCreated {
		log.Info().Str("realm", realm.Realm).Msg("Realm created")
		return nil
	}

	if res.StatusCode == http.StatusConflict {
		updateURL := fmt.Sprintf("%s/admin/realms/%s", s.keycloakBaseURL, realm.Realm)
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, updateURL, bytes.NewBuffer(realmJSON))
		if err != nil { // coverage-ignore
			return fmt.Errorf("failed to create realm update request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		res, err := s.keycloak.Do(req)
		if err != nil { // coverage-ignore
			return fmt.Errorf("failed to update realm: %w", err)
		}
		defer res.Body.Close() //nolint:errcheck

		if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to update realm: status %d", res.StatusCode)
		}
		log.Info().Str("realm", realm.Realm).Msg("Realm updated")
		return nil
	}

	return nil
}

type realmClient struct {
	ClientID   string `json:"clientId"`
	ClientName string `json:"name"`
}

func (s *subroutine) getClientID(ctx context.Context, realmName, clientName string) (string, error) {
	url := fmt.Sprintf("%s/admin/realms/%s/clients", s.keycloakBaseURL, realmName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil { // coverage-ignore
		return "", fmt.Errorf("failed to create get clients request: %w", err)
	}

	res, err := s.keycloak.Do(req)
	if err != nil { // coverage-ignore
		return "", fmt.Errorf("failed to get clients: %w", err)
	}
	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get clients: status %d", res.StatusCode)
	}

	respBody, err := io.ReadAll(res.Body)
	if err != nil { // coverage-ignore
		return "", fmt.Errorf("failed to read clients response: %w", err)
	}

	var clients []realmClient
	if err := json.Unmarshal(respBody, &clients); err != nil { // coverage-ignore
		return "", fmt.Errorf("failed to parse clients response: %w body: %s", err, respBody)
	}

	for _, client := range clients {
		if client.ClientName == clientName {
			return client.ClientID, nil
		}
	}

	return "", nil
}

func (s *subroutine) deleteRealm(ctx context.Context, realmName string) error {
	url := fmt.Sprintf("%s/admin/realms/%s", s.keycloakBaseURL, realmName)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil { // coverage-ignore
		return fmt.Errorf("failed to create realm delete request: %w", err)
	}

	res, err := s.keycloak.Do(req)
	if err != nil { // coverage-ignore
		return fmt.Errorf("failed to delete realm: %w", err)
	}
	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNotFound {
		return fmt.Errorf("failed to delete realm: status %d", res.StatusCode)
	}
	return nil
}

func (s *subroutine) regenerateRegistrationAccessToken(ctx context.Context, realmName, clientUUID string) (string, error) {
	tokenURL := fmt.Sprintf("%s/admin/realms/%s/clients/%s/registration-access-token", s.keycloakBaseURL, realmName, clientUUID)
	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, nil)
	if err != nil { // coverage-ignore
		return "", fmt.Errorf("failed to create regenerate registration token request: %w", err)
	}

	tokenRes, err := s.keycloak.Do(tokenReq)
	if err != nil { // coverage-ignore
		return "", fmt.Errorf("failed to regenerate registration token: %w", err)
	}
	defer tokenRes.Body.Close() //nolint:errcheck

	if tokenRes.StatusCode != http.StatusOK && tokenRes.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(tokenRes.Body)
		return "", fmt.Errorf("failed to regenerate registration token: status %d body: %s", tokenRes.StatusCode, respBody)
	}

	tokenBody, err := io.ReadAll(tokenRes.Body)
	if err != nil { // coverage-ignore
		return "", fmt.Errorf("failed to read registration token response: %w", err)
	}

	var tokenResponse struct {
		RegistrationAccessToken string `json:"registrationAccessToken"`
	}
	if err := json.Unmarshal(tokenBody, &tokenResponse); err != nil { // coverage-ignore
		return "", fmt.Errorf("failed to parse registration token response: %w body: %s", err, tokenBody)
	}

	return tokenResponse.RegistrationAccessToken, nil
}

func (s *subroutine) getInitialAccessToken(ctx context.Context, realmName string) (string, error) {
	url := fmt.Sprintf("%s/admin/realms/%s/clients-initial-access", s.keycloakBaseURL, realmName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("{}"))
	if err != nil { // coverage-ignore
		return "", fmt.Errorf("failed to create IAT request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := s.keycloak.Do(req)
	if err != nil { // coverage-ignore
		return "", fmt.Errorf("failed to create IAT: %w", err)
	}
	defer res.Body.Close() //nolint:errcheck

	respBody, _ := io.ReadAll(res.Body)

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to create IAT: status %d body: %s", res.StatusCode, respBody)
	}

	var iatResponse struct {
		Token string `json:"token"`
	}

	if err := json.Unmarshal(respBody, &iatResponse); err != nil { // coverage-ignore
		return "", fmt.Errorf("failed to parse IAT response: %w body: %s", err, respBody)
	}
	return iatResponse.Token, nil
}
