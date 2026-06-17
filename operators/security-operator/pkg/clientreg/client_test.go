package clientreg

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockTokenProvider is a mock implementation of TokenProvider.
type mockTokenProvider struct {
	mock.Mock
}

func (m *mockTokenProvider) TokenForRegistration(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}

func TestClient_Register(t *testing.T) {
	tests := []struct {
		name         string
		metadata     ClientMetadata
		setupServer  func(t *testing.T) *httptest.Server
		setupMocks   func(*mockTokenProvider)
		wantErr      bool
		wantErrMsg   string
		wantClientID string
		wantSecret   string
	}{
		{
			name: "successful registration",
			metadata: ClientMetadata{
				ClientName:   "test-client",
				RedirectURIs: []string{"https://example.com/callback"},
				GrantTypes:   []string{GrantTypeAuthorizationCode, GrantTypeRefreshToken},
			},
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
					assert.Equal(t, "Bearer test-initial-token", r.Header.Get("Authorization"))

					var req ClientMetadata
					err := json.NewDecoder(r.Body).Decode(&req)
					require.NoError(t, err)
					assert.Equal(t, "test-client", req.ClientName)

					w.WriteHeader(http.StatusCreated)
					json.NewEncoder(w).Encode(ClientInformation{ //nolint:errcheck
						ClientID:                "client-123",
						ClientSecret:            "secret-456",
						RegistrationAccessToken: "rat-789",
						RegistrationClientURI:   "https://idp.example.com/clients/client-123",
					})
				}))
			},
			setupMocks: func(tp *mockTokenProvider) {
				tp.On("TokenForRegistration", mock.Anything).Return("test-initial-token", nil)
			},
			wantClientID: "client-123",
			wantSecret:   "secret-456",
		},
		{
			name: "registration fails with 400",
			metadata: ClientMetadata{
				ClientName: "invalid-client",
			},
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte(`{"error":"invalid_client_metadata"}`)) //nolint:errcheck
				}))
			},
			setupMocks: func(tp *mockTokenProvider) {
				tp.On("TokenForRegistration", mock.Anything).Return("test-token", nil)
			},
			wantErr:    true,
			wantErrMsg: "HTTP 400",
		},
		{
			name:     "no token provider configured",
			metadata: ClientMetadata{ClientName: "test"},
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Fatal("server should not be called")
				}))
			},
			setupMocks: nil, // No token provider
			wantErr:    true,
			wantErrMsg: "token provider is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setupServer(t)
			defer server.Close()

			var opts []Option
			if tt.setupMocks != nil {
				tp := &mockTokenProvider{}
				tt.setupMocks(tp)
				opts = append(opts, WithTokenProvider(tp))
			}

			client := NewClient(opts...)
			info, err := client.Register(context.Background(), server.URL, tt.metadata)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantClientID, info.ClientID)
			assert.Equal(t, tt.wantSecret, info.ClientSecret)
		})
	}
}

func TestClient_Read(t *testing.T) {
	tests := []struct {
		name         string
		clientID     string
		regURI       string
		regToken     string
		setupServer  func(t *testing.T) *httptest.Server
		setupMocks   func(*mockTokenProvider)
		wantErr      bool
		wantClientID string
	}{
		{
			name:     "successful read",
			clientID: "client-123",
			regURI:   "/clients/client-123",
			regToken: "rat-789",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodGet, r.Method)
					assert.Equal(t, "Bearer rat-789", r.Header.Get("Authorization"))

					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(ClientInformation{ //nolint:errcheck
						ClientID:     "client-123",
						ClientSecret: "secret-456",
					})
				}))
			},
			wantClientID: "client-123",
		},
		{
			name:     "read fails with 404",
			clientID: "not-found",
			regURI:   "/clients/not-found",
			regToken: "rat-789",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			wantErr: true,
		},
		{
			name:     "empty registration URI",
			clientID: "client-123",
			regURI:   "",
			regToken: "rat-789",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Fatal("server should not be called")
				}))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setupServer(t)
			defer server.Close()

			var opts []Option
			if tt.setupMocks != nil {
				tp := &mockTokenProvider{}
				tt.setupMocks(tp)
				opts = append(opts, WithTokenProvider(tp))
			}

			client := NewClient(opts...)

			regURI := tt.regURI
			if regURI != "" {
				regURI = server.URL + tt.regURI
			}

			info, err := client.Read(context.Background(), tt.clientID, regURI, tt.regToken)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantClientID, info.ClientID)
		})
	}
}

func TestClient_Update(t *testing.T) {
	tests := []struct {
		name         string
		regURI       string
		regToken     string
		metadata     ClientMetadata
		setupServer  func(t *testing.T) *httptest.Server
		wantErr      bool
		wantClientID string
	}{
		{
			name:     "successful update",
			regURI:   "/clients/client-123",
			regToken: "rat-789",
			metadata: ClientMetadata{
				ClientID:     "client-123",
				ClientName:   "updated-client",
				RedirectURIs: []string{"https://example.com/new-callback"},
			},
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodPut, r.Method)
					assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
					assert.Equal(t, "Bearer rat-789", r.Header.Get("Authorization"))

					var req ClientMetadata
					err := json.NewDecoder(r.Body).Decode(&req)
					require.NoError(t, err)
					assert.Equal(t, "updated-client", req.ClientName)

					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(ClientInformation{ //nolint:errcheck
						ClientID:                "client-123",
						RegistrationAccessToken: "new-rat-999",
					})
				}))
			},
			wantClientID: "client-123",
		},
		{
			name:     "update fails with 401",
			regURI:   "/clients/client-123",
			regToken: "expired-token",
			metadata: ClientMetadata{ClientID: "client-123"},
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				}))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setupServer(t)
			defer server.Close()

			client := NewClient()
			info, err := client.Update(context.Background(), server.URL+tt.regURI, tt.regToken, tt.metadata)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantClientID, info.ClientID)
		})
	}
}

func TestClient_Delete(t *testing.T) {
	tests := []struct {
		name        string
		clientID    string
		regURI      string
		regToken    string
		setupServer func(t *testing.T) *httptest.Server
		wantErr     bool
	}{
		{
			name:     "successful delete",
			clientID: "client-123",
			regURI:   "/clients/client-123",
			regToken: "rat-789",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodDelete, r.Method)
					assert.Equal(t, "Bearer rat-789", r.Header.Get("Authorization"))
					w.WriteHeader(http.StatusNoContent)
				}))
			},
			wantErr: false,
		},
		{
			name:     "delete fails with 403",
			clientID: "client-123",
			regURI:   "/clients/client-123",
			regToken: "rat-789",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				}))
			},
			wantErr: true,
		},
		{
			name:     "empty registration URI",
			clientID: "client-123",
			regURI:   "",
			regToken: "rat-789",
			setupServer: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Fatal("server should not be called")
				}))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setupServer(t)
			defer server.Close()

			client := NewClient()

			regURI := tt.regURI
			if regURI != "" {
				regURI = server.URL + tt.regURI
			}

			err := client.Delete(context.Background(), tt.clientID, regURI, tt.regToken)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestWithHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 5 * time.Second}
	cl := NewClient(WithHTTPClient(custom)).(*client)
	assert.Equal(t, custom, cl.httpClient)
}

func TestHTTPError(t *testing.T) {
	err := NewHTTPError(401, "unauthorized", OperationUpdate)

	assert.Equal(t, 401, err.StatusCode)
	assert.Equal(t, "unauthorized", err.Body)
	assert.Equal(t, OperationUpdate, err.Operation)
	assert.Contains(t, err.Error(), "HTTP 401")
	assert.Contains(t, err.Error(), "update")

	httpErr, ok := IsHTTPError(err)
	assert.True(t, ok)
	assert.Equal(t, 401, httpErr.StatusCode)

	assert.True(t, IsUnauthorized(err))
	assert.False(t, IsNotFound(err))
}
