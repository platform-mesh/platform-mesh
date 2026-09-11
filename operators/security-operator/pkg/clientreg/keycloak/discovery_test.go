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

package keycloak

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	discoveryHostValidator = func(context.Context, string) error { return nil }
}

func TestFetchOIDCDiscovery_rejectsBlockedDiscoveryURL(t *testing.T) {
	t.Parallel()

	_, err := FetchOIDCDiscovery(context.Background(), http.DefaultClient, "https://127.0.0.1/.well-known/openid-configuration")
	assert.Error(t, err)
}

func TestFetchOIDCDiscovery_rejectsUnsafeEndpointsInDocument(t *testing.T) {
	t.Parallel()

	client := &http.Client{
		Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			body := `{
				"issuer": "https://127.0.0.1",
				"authorization_endpoint": "https://idp.example.com/auth",
				"token_endpoint": "https://idp.example.com/token",
				"jwks_uri": "https://idp.example.com/jwks"
			}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}),
	}

	_, err := FetchOIDCDiscovery(
		context.Background(),
		client,
		"https://idp.example.com/.well-known/openid-configuration",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsafe endpoints")
}

func TestFetchOIDCDiscovery_success(t *testing.T) {
	t.Parallel()

	client := &http.Client{
		Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			body := `{
				"issuer": "https://idp.example.com",
				"authorization_endpoint": "https://idp.example.com/auth",
				"token_endpoint": "https://idp.example.com/token",
				"jwks_uri": "https://idp.example.com/jwks"
			}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}),
	}

	discovery, err := FetchOIDCDiscovery(
		context.Background(),
		client,
		"https://idp.example.com/.well-known/openid-configuration",
	)
	require.NoError(t, err)
	assert.Equal(t, "https://idp.example.com/token", discovery.TokenURL)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
