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
		return resp, nil //nolint:nilerr
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
