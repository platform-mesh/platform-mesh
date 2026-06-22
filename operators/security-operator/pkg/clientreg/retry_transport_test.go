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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"
)

func closedServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	return srv
}

func TestClientIDFromContext(t *testing.T) {
	tests := []struct {
		name   string
		ctx    context.Context
		wantID string
	}{
		{
			name:   "context has client ID",
			ctx:    WithClientID(context.Background(), "my-client"),
			wantID: "my-client",
		},
		{
			name:   "empty context returns empty string",
			ctx:    context.Background(),
			wantID: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantID, ClientIDFromContext(tt.ctx))
		})
	}
}

func TestNewRetryTransport(t *testing.T) {
	t.Run("nil base uses DefaultTransport", func(t *testing.T) {
		rt := NewRetryTransport(nil, mocks.NewMockTokenRefresher(t))
		assert.Equal(t, http.DefaultTransport, rt.Base)
	})
	t.Run("non-nil base is preserved", func(t *testing.T) {
		rt := NewRetryTransport(http.DefaultTransport, mocks.NewMockTokenRefresher(t))
		assert.Equal(t, http.DefaultTransport, rt.Base)
	})
}

func TestRetryTransport_RoundTrip(t *testing.T) {
	tests := []struct {
		name           string
		setupServer    func(t *testing.T) *httptest.Server
		tokenRefresher func(t *testing.T) *mocks.MockTokenRefresher
		ctx            func() context.Context
		withBody       bool
		wantStatus     int
		wantErr        bool
	}{
		{
			name:        "base transport error",
			setupServer: closedServer,
			wantErr:     true,
		},
		{
			name: "non-401 passes through",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "401 with nil refresher passes through",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				}))
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "401 without client ID in context passes through",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				}))
			},
			tokenRefresher: func(t *testing.T) *mocks.MockTokenRefresher {
				return mocks.NewMockTokenRefresher(t)
			},
			ctx:        func() context.Context { return context.Background() },
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "401 with RefreshToken error passes through original response",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				}))
			},
			tokenRefresher: func(t *testing.T) *mocks.MockTokenRefresher {
				r := mocks.NewMockTokenRefresher(t)
				r.EXPECT().RefreshToken(mock.Anything, "my-client").Return("", fmt.Errorf("token refresh failed"))
				return r
			},
			ctx:        func() context.Context { return WithClientID(context.Background(), "my-client") },
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "401 with successful refresh retries request",
			setupServer: func(t *testing.T) *httptest.Server {
				call := 0
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					call++
					if call == 1 {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					assert.Equal(t, "Bearer new-token", r.Header.Get("Authorization"))
					w.WriteHeader(http.StatusOK)
				}))
			},
			tokenRefresher: func(t *testing.T) *mocks.MockTokenRefresher {
				r := mocks.NewMockTokenRefresher(t)
				r.EXPECT().RefreshToken(mock.Anything, "my-client").Return("new-token", nil)
				return r
			},
			ctx:        func() context.Context { return WithClientID(context.Background(), "my-client") },
			wantStatus: http.StatusOK,
		},
		{
			name: "401 with body replays body on retry",
			setupServer: func(t *testing.T) *httptest.Server {
				call := 0
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					call++
					if call == 1 {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					w.WriteHeader(http.StatusOK)
				}))
			},
			tokenRefresher: func(t *testing.T) *mocks.MockTokenRefresher {
				r := mocks.NewMockTokenRefresher(t)
				r.EXPECT().RefreshToken(mock.Anything, "my-client").Return("new-token", nil)
				return r
			},
			ctx:        func() context.Context { return WithClientID(context.Background(), "my-client") },
			withBody:   true,
			wantStatus: http.StatusOK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupServer(t)
			defer srv.Close()

			var refresher TokenRefresher
			if tt.tokenRefresher != nil {
				refresher = tt.tokenRefresher(t)
			}

			rt := &RetryTransport{
				Base:           http.DefaultTransport,
				TokenRefresher: refresher,
			}

			ctx := context.Background()
			if tt.ctx != nil {
				ctx = tt.ctx()
			}

			var body io.Reader
			if tt.withBody {
				body = strings.NewReader("request payload")
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, body)
			require.NoError(t, err)

			resp, err := rt.RoundTrip(req)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			defer resp.Body.Close() //nolint:errcheck
			assert.Equal(t, tt.wantStatus, resp.StatusCode)
		})
	}
}
