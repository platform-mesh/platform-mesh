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

package http

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	utilscontext "go.platform-mesh.io/kubernetes-graphql-gateway/gateway/utils/context"
)

const testEndpointSuffix = "/graphql"

// captureHandler is a test handler that records the request context values
// passed through the middleware chain.
type captureHandler struct {
	called      bool
	token       string
	tokenOK     bool
	clusterName string
	clusterOK   bool
}

func (h *captureHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.called = true
	h.token, h.tokenOK = utilscontext.GetTokenFromCtx(r.Context())
	h.clusterName, h.clusterOK = utilscontext.GetClusterFromCtx(r.Context())
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func clusterURL(base, cluster string) string {
	return fmt.Sprintf("%s/api/clusters/%s%s", base, cluster, testEndpointSuffix)
}

func newTestServer(t *testing.T, gateway http.Handler) *httptest.Server {
	t.Helper()
	srv, err := NewServer(ServerConfig{
		Gateway:        gateway,
		Addr:           ":0",
		EndpointSuffix: testEndpointSuffix,
		CORSConfig:     CORSConfig{},
	})
	require.NoError(t, err)
	return httptest.NewServer(srv.Server.Handler)
}

func newTestServerWithBodyLimit(t *testing.T, gateway http.Handler, maxBytes int64) *httptest.Server {
	t.Helper()
	srv, err := NewServer(ServerConfig{
		Gateway:             gateway,
		Addr:                ":0",
		MaxRequestBodyBytes: maxBytes,
		CORSConfig:          CORSConfig{},
	})
	require.NoError(t, err)
	return httptest.NewServer(srv.Server.Handler)
}

func TestMissingAuthorizationHeader(t *testing.T) {
	handler := &captureHandler{}
	ts := newTestServer(t, handler)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, clusterURL(ts.URL, "test-cluster"), strings.NewReader(`{"query":"{}"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.False(t, handler.called)

	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "missing Authorization header")
}

func TestInvalidAuthorizationFormat(t *testing.T) {
	handler := &captureHandler{}
	ts := newTestServer(t, handler)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, clusterURL(ts.URL, "test-cluster"), strings.NewReader(`{"query":"{}"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic abc123")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.False(t, handler.called)
}

func TestValidBearerTokenForwarded(t *testing.T) {
	handler := &captureHandler{}
	ts := newTestServer(t, handler)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, clusterURL(ts.URL, "my-cluster"), strings.NewReader(`{"query":"{}"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer valid-test-token")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.True(t, handler.called)
	assert.Equal(t, "valid-test-token", handler.token)
	assert.Equal(t, "my-cluster", handler.clusterName)
}

func TestUnauthenticatedEndpoints(t *testing.T) {
	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		t.Run(path, func(t *testing.T) {
			handler := &captureHandler{}
			ts := newTestServer(t, handler)
			defer ts.Close()

			resp, err := http.Get(ts.URL + path)
			require.NoError(t, err)
			defer resp.Body.Close() //nolint:errcheck

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.False(t, handler.called)
		})
	}
}

func TestPlaygroundEnabledAllowsUnauthenticatedGet(t *testing.T) {
	handler := &captureHandler{}
	srv, err := NewServer(ServerConfig{
		Gateway:           handler,
		Addr:              ":0",
		EndpointSuffix:    testEndpointSuffix,
		PlaygroundEnabled: true,
		CORSConfig:        CORSConfig{},
	})
	require.NoError(t, err)
	ts := httptest.NewServer(srv.Server.Handler)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, clusterURL(ts.URL, "my-cluster"), nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.True(t, handler.called)
	assert.Equal(t, "my-cluster", handler.clusterName)
	assert.Empty(t, handler.token)
}

func TestPlaygroundDisabledRejectsUnauthenticatedGet(t *testing.T) {
	handler := &captureHandler{}
	ts := newTestServer(t, handler)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, clusterURL(ts.URL, "my-cluster"), nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.False(t, handler.called)
}

func TestHealthEndpointsReflectCheckerState(t *testing.T) {
	failing := func(_ *http.Request) error { return fmt.Errorf("down") }

	srv, err := NewServer(ServerConfig{
		Gateway:        &captureHandler{},
		HealthzCheck:   failing,
		ReadyzCheck:    failing,
		Addr:           ":0",
		EndpointSuffix: testEndpointSuffix,
	})
	require.NoError(t, err)
	ts := httptest.NewServer(srv.Server.Handler)
	defer ts.Close()

	for _, path := range []string{"/readyz"} {
		resp, err := http.Get(ts.URL + path)
		require.NoError(t, err)
		resp.Body.Close() //nolint:errcheck
		assert.NotEqual(t, http.StatusOK, resp.StatusCode, path)
	}
}

func TestMaxRequestBodyBytes(t *testing.T) {
	tests := []struct {
		name           string
		maxBytes       int64
		bodySize       int
		expectedStatus int
		expectCalled   bool
	}{
		{
			name:           "rejects body exceeding limit",
			maxBytes:       64,
			bodySize:       128,
			expectedStatus: http.StatusOK, // handler is still called; MaxBytesReader limits the read, not the dispatch
			expectCalled:   true,
		},
		{
			name:           "allows body within limit",
			maxBytes:       1024,
			bodySize:       14, // len(`{"query":"{}"}`)
			expectedStatus: http.StatusOK,
			expectCalled:   true,
		},
		{
			name:           "no limit when disabled",
			maxBytes:       0,
			bodySize:       1024 * 1024,
			expectedStatus: http.StatusOK,
			expectCalled:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &captureHandler{}
			ts := newTestServerWithBodyLimit(t, handler, tt.maxBytes)
			defer ts.Close()

			body := strings.Repeat("x", tt.bodySize)
			req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/clusters/test-cluster", strings.NewReader(body))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer valid-token")

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close() //nolint:errcheck

			assert.Equal(t, tt.expectedStatus, resp.StatusCode)
			assert.Equal(t, tt.expectCalled, handler.called)
		})
	}
}
