package clientreg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type Client interface {
	Register(ctx context.Context, registrationEndpoint string, metadata ClientMetadata) (ClientInformation, error)
	Read(ctx context.Context, clientID, registrationClientURI, registrationAccessToken string) (ClientInformation, error)
	Update(ctx context.Context, registrationClientURI, registrationAccessToken string, metadata ClientMetadata) (ClientInformation, error)
	Delete(ctx context.Context, clientID, registrationClientURI, registrationAccessToken string) error
}

type client struct {
	httpClient    *http.Client
	tokenProvider TokenProvider
}

func NewClient(opts ...Option) Client {
	options := defaultOptions()
	for _, opt := range opts {
		opt(options)
	}

	return &client{
		httpClient:    options.httpClient,
		tokenProvider: options.tokenProvider,
	}
}

func (c *client) Register(ctx context.Context, registrationEndpoint string, metadata ClientMetadata) (ClientInformation, error) {
	if c.tokenProvider == nil {
		return ClientInformation{}, ErrNoTokenProvider
	}

	token, err := c.tokenProvider.TokenForRegistration(ctx)
	if err != nil {
		return ClientInformation{}, fmt.Errorf("failed to get registration token: %w", err)
	}

	return c.doRegister(ctx, registrationEndpoint, token, metadata)
}

func (c *client) doRegister(ctx context.Context, registrationEndpoint, token string, metadata ClientMetadata) (ClientInformation, error) {
	body, err := json.Marshal(metadata)
	if err != nil {
		return ClientInformation{}, fmt.Errorf("failed to marshal client metadata: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registrationEndpoint, bytes.NewReader(body))
	if err != nil {
		return ClientInformation{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ClientInformation{}, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusCreated {
		return ClientInformation{}, newHTTPErrorFromResponse(resp, OperationRegister)
	}

	var info ClientInformation
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return ClientInformation{}, fmt.Errorf("failed to parse response: %w", err)
	}

	return info, nil
}

func (c *client) Read(ctx context.Context, clientID, registrationClientURI, registrationAccessToken string) (ClientInformation, error) {
	if registrationClientURI == "" {
		return ClientInformation{}, ErrNoRegistrationURI
	}

	ctx = WithClientID(ctx, clientID)
	return c.doRead(ctx, registrationClientURI, registrationAccessToken)
}

func (c *client) doRead(ctx context.Context, registrationClientURI, token string) (ClientInformation, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, registrationClientURI, nil)
	if err != nil {
		return ClientInformation{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ClientInformation{}, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return ClientInformation{}, newHTTPErrorFromResponse(resp, OperationRead)
	}

	var info ClientInformation
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return ClientInformation{}, fmt.Errorf("failed to parse response: %w", err)
	}

	return info, nil
}

func (c *client) Update(ctx context.Context, registrationClientURI, registrationAccessToken string, metadata ClientMetadata) (ClientInformation, error) {
	if registrationClientURI == "" {
		return ClientInformation{}, ErrNoRegistrationURI
	}

	ctx = WithClientID(ctx, metadata.ClientID)
	return c.doUpdate(ctx, registrationClientURI, registrationAccessToken, metadata)
}

func (c *client) doUpdate(ctx context.Context, registrationClientURI, token string, metadata ClientMetadata) (ClientInformation, error) {
	body, err := json.Marshal(metadata)
	if err != nil {
		return ClientInformation{}, fmt.Errorf("failed to marshal client metadata: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, registrationClientURI, bytes.NewReader(body))
	if err != nil {
		return ClientInformation{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ClientInformation{}, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return ClientInformation{}, newHTTPErrorFromResponse(resp, OperationUpdate)
	}

	var info ClientInformation
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return ClientInformation{}, fmt.Errorf("failed to parse response: %w", err)
	}

	return info, nil
}

func (c *client) Delete(ctx context.Context, clientID, registrationClientURI, registrationAccessToken string) error {
	if registrationClientURI == "" {
		return ErrNoRegistrationURI
	}

	ctx = WithClientID(ctx, clientID)
	return c.doDelete(ctx, registrationClientURI, registrationAccessToken)
}

func (c *client) doDelete(ctx context.Context, registrationClientURI, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, registrationClientURI, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNoContent {
		return newHTTPErrorFromResponse(resp, OperationDelete)
	}

	return nil
}
