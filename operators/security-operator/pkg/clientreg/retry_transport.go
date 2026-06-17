package clientreg

import (
	"context"
	"net/http"
)

type contextKey string

const clientIDKey contextKey = "oidc-client-id"

func WithClientID(ctx context.Context, clientID string) context.Context {
	return context.WithValue(ctx, clientIDKey, clientID)
}

func ClientIDFromContext(ctx context.Context) string {
	if v := ctx.Value(clientIDKey); v != nil {
		return v.(string)
	}
	return ""
}

type TokenRefresher interface {
	RefreshToken(ctx context.Context, clientID string) (newToken string, err error)
}

// RetryTransport wraps an http.RoundTripper and retries requests on 401
// after refreshing the authentication token via TokenRefresher.
type RetryTransport struct {
	Base           http.RoundTripper
	TokenRefresher TokenRefresher
}

func NewRetryTransport(base http.RoundTripper, refresher TokenRefresher) *RetryTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &RetryTransport{
		Base:           base,
		TokenRefresher: refresher,
	}
}

func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.Base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusUnauthorized || t.TokenRefresher == nil {
		return resp, nil
	}

	clientID := ClientIDFromContext(req.Context())
	if clientID == "" {
		return resp, nil
	}

	newToken, err := t.TokenRefresher.RefreshToken(req.Context(), clientID)
	if err != nil {
		return resp, nil
	}

	resp.Body.Close() //nolint:errcheck

	retryReq := req.Clone(req.Context())
	retryReq.Header.Set("Authorization", "Bearer "+newToken)

	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		retryReq.Body = body
	}

	return t.Base.RoundTrip(retryReq)
}
