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

package invite_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"golang.org/x/oauth2"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger/testlogger"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/subroutine/invite"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"

	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
)

func configureOIDCProvider(t *testing.T, mux *http.ServeMux, baseURL string) {

	mux.HandleFunc("/realms/master/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		err := json.NewEncoder(w).Encode(&map[string]string{
			"issuer":                 fmt.Sprintf("%s/realms/master", baseURL),
			"authorization_endpoint": fmt.Sprintf("%s/realms/master/protocol/openid-connect/auth", baseURL),
			"token_endpoint":         fmt.Sprintf("%s/realms/master/protocol/openid-connect/token", baseURL),
		})
		assert.NoError(t, err)
	})

	mux.HandleFunc("/realms/master/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		err := json.NewEncoder(w).Encode(&map[string]string{
			"access_token": "token",
		})
		assert.NoError(t, err)
	})

}

func TestSubroutineProcess(t *testing.T) {
	testCases := []struct {
		desc               string
		obj                ctrlruntimeclient.Object
		config             *config.Config
		setupK8sMocks      func(m *mocks.MockClient)
		setupKeycloakMocks func(mux *http.ServeMux)
		expectErr          bool
	}{
		{
			desc: "user created with default password",
			obj: &pmcorev1alpha1.Invite{
				Spec: pmcorev1alpha1.InviteSpec{
					Email: "password@acme.corp",
				},
			},
			config: &config.Config{
				SetDefaultPassword: true,
			},
			setupK8sMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo"), mock.Anything).
					RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						accountInfo := &pmcorev1alpha1.AccountInfo{
							Spec: pmcorev1alpha1.AccountInfoSpec{
								Organization: pmcorev1alpha1.AccountLocation{
									Name: "acme",
								},
								OIDC: &pmcorev1alpha1.OIDCInfo{
									IssuerURL: "https://keycloak/realms/acme",
									Clients: map[string]pmcorev1alpha1.ClientInfo{
										"acme": {ClientID: "acme"},
									},
								},
							},
						}
						*o.(*pmcorev1alpha1.AccountInfo) = *accountInfo
						return nil
					}).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux) {
				users := []map[string]any{}
				mux.HandleFunc("GET /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					err := json.NewEncoder(w).Encode(&users)
					assert.NoError(t, err)
				})
				mux.HandleFunc("POST /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")

					var body map[string]any
					err := json.NewDecoder(r.Body).Decode(&body)
					assert.NoError(t, err)

					credentials, ok := body["credentials"].([]any)
					assert.True(t, ok)
					assert.Len(t, credentials, 1)
					cred := credentials[0].(map[string]any)
					assert.Equal(t, "password", cred["type"])
					assert.Equal(t, "password", cred["value"])
					assert.Equal(t, true, cred["temporary"])

					w.WriteHeader(http.StatusCreated)
					body["id"] = "1234"
					users = append(users, body)
				})
				mux.HandleFunc("GET /admin/realms/acme/clients", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					clients := []map[string]any{
						{"id": "client-uuid", "clientId": "acme"},
					}
					err := json.NewEncoder(w).Encode(&clients)
					assert.NoError(t, err)
				})
				mux.HandleFunc("PUT /admin/realms/acme/users/{id}/execute-actions-email", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusNoContent)
				})
			},
		},
		{
			desc: "user created and invitation email sent",
			obj: &pmcorev1alpha1.Invite{
				Spec: pmcorev1alpha1.InviteSpec{
					Email: "example@acme.corp",
				},
			},
			setupK8sMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo"), mock.Anything).
					RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						accountInfo := &pmcorev1alpha1.AccountInfo{
							Spec: pmcorev1alpha1.AccountInfoSpec{
								Organization: pmcorev1alpha1.AccountLocation{
									Name: "acme",
								},
								OIDC: &pmcorev1alpha1.OIDCInfo{
									IssuerURL: "https://keycloak/realms/acme",
									Clients: map[string]pmcorev1alpha1.ClientInfo{
										"acme": {ClientID: "acme"},
									},
								},
							},
						}
						*o.(*pmcorev1alpha1.AccountInfo) = *accountInfo
						return nil
					}).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux) {
				users := []map[string]any{}
				mux.HandleFunc("GET /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					err := json.NewEncoder(w).Encode(&users)
					assert.NoError(t, err)
				})
				mux.HandleFunc("POST /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					user := map[string]any{}
					err := json.NewDecoder(r.Body).Decode(&user)
					assert.NoError(t, err)
					user["id"] = "1234"
					users = append(users, user)
				})
				mux.HandleFunc("GET /admin/realms/acme/clients", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					clients := []map[string]any{
						{"id": "client-uuid", "clientId": "acme"},
					}
					err := json.NewEncoder(w).Encode(&clients)
					assert.NoError(t, err)
				})
				mux.HandleFunc("PUT /admin/realms/acme/users/{id}/execute-actions-email", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusNoContent)
				})
			},
		},
		{
			desc: "error listing users (500 from Keycloak)",
			obj: &pmcorev1alpha1.Invite{
				Spec: pmcorev1alpha1.InviteSpec{
					Email: "error1@acme.corp",
				},
			},
			expectErr: true,
			setupK8sMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo"), mock.Anything).
					RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						accountInfo := &pmcorev1alpha1.AccountInfo{
							Spec: pmcorev1alpha1.AccountInfoSpec{
								Organization: pmcorev1alpha1.AccountLocation{
									Name: "acme",
								},
							},
						}
						*o.(*pmcorev1alpha1.AccountInfo) = *accountInfo
						return nil
					}).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux) {
				mux.HandleFunc("GET /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":"boom"}`))
				})
			},
		},
		{
			desc: "error creating user (POST returns 400)",
			obj: &pmcorev1alpha1.Invite{
				Spec: pmcorev1alpha1.InviteSpec{
					Email: "error2@acme.corp",
				},
			},
			expectErr: true,
			setupK8sMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo"), mock.Anything).
					RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						accountInfo := &pmcorev1alpha1.AccountInfo{
							Spec: pmcorev1alpha1.AccountInfoSpec{
								Organization: pmcorev1alpha1.AccountLocation{
									Name: "acme",
								},
								OIDC: &pmcorev1alpha1.OIDCInfo{
									IssuerURL: "https://keycloak/realms/acme",
									Clients: map[string]pmcorev1alpha1.ClientInfo{
										"acme": {ClientID: "acme"},
									},
								},
							},
						}
						*o.(*pmcorev1alpha1.AccountInfo) = *accountInfo
						return nil
					}).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux) {
				first := true
				mux.HandleFunc("GET /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					if first {
						first = false
						_ = json.NewEncoder(w).Encode([]map[string]any{})
						return
					}
					_ = json.NewEncoder(w).Encode([]map[string]any{
						{"id": "should-not-exist", "email": "error2@acme.corp"},
					})
				})
				mux.HandleFunc("POST /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"invalid user"}`))
				})
			},
		},
		{
			desc: "error fetching AccountInfo (not found)",
			obj: &pmcorev1alpha1.Invite{
				Spec: pmcorev1alpha1.InviteSpec{
					Email: "missing@acme.corp",
				},
			},
			expectErr: true,
			setupK8sMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo"), mock.Anything).
					Return(fmt.Errorf("accountinfo not found")).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux) {
			},
		},
		{
			desc: "error verifying client (500 from Keycloak)",
			obj: &pmcorev1alpha1.Invite{
				Spec: pmcorev1alpha1.InviteSpec{
					Email: "clienterror@acme.corp",
				},
			},
			expectErr: true,
			setupK8sMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo"), mock.Anything).
					RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						accountInfo := &pmcorev1alpha1.AccountInfo{
							Spec: pmcorev1alpha1.AccountInfoSpec{
								Organization: pmcorev1alpha1.AccountLocation{
									Name: "acme",
								},
								OIDC: &pmcorev1alpha1.OIDCInfo{
									IssuerURL: "https://keycloak/realms/acme",
									Clients: map[string]pmcorev1alpha1.ClientInfo{
										"acme": {ClientID: "acme"},
									},
								},
							},
						}
						*o.(*pmcorev1alpha1.AccountInfo) = *accountInfo
						return nil
					}).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux) {
				users := []map[string]any{}
				mux.HandleFunc("GET /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					err := json.NewEncoder(w).Encode(&users)
					assert.NoError(t, err)
				})
				mux.HandleFunc("POST /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					user := map[string]any{}
					err := json.NewDecoder(r.Body).Decode(&user)
					assert.NoError(t, err)
					user["id"] = "1234"
					users = append(users, user)
				})
				mux.HandleFunc("GET /admin/realms/acme/clients", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":"boom"}`))
				})
			},
		},
		{
			desc: "client does not exist yet, should requeue",
			obj: &pmcorev1alpha1.Invite{
				Spec: pmcorev1alpha1.InviteSpec{
					Email: "requeue@acme.corp",
				},
			},
			expectErr: false,
			setupK8sMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo"), mock.Anything).
					RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						accountInfo := &pmcorev1alpha1.AccountInfo{
							Spec: pmcorev1alpha1.AccountInfoSpec{
								Organization: pmcorev1alpha1.AccountLocation{
									Name: "acme",
								},
								OIDC: &pmcorev1alpha1.OIDCInfo{
									IssuerURL: "https://keycloak/realms/acme",
									Clients: map[string]pmcorev1alpha1.ClientInfo{
										"acme": {ClientID: "acme"},
									},
								},
							},
						}
						*o.(*pmcorev1alpha1.AccountInfo) = *accountInfo
						return nil
					}).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux) {
				users := []map[string]any{}
				mux.HandleFunc("GET /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					err := json.NewEncoder(w).Encode(&users)
					assert.NoError(t, err)
				})
				mux.HandleFunc("POST /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					user := map[string]any{}
					err := json.NewDecoder(r.Body).Decode(&user)
					assert.NoError(t, err)
					user["id"] = "1234"
					users = append(users, user)
				})
				mux.HandleFunc("GET /admin/realms/acme/clients", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					clients := []map[string]any{}
					err := json.NewEncoder(w).Encode(&clients)
					assert.NoError(t, err)
				})
			},
		},
		{
			desc: "organization name is empty in AccountInfo",
			obj: &pmcorev1alpha1.Invite{
				Spec: pmcorev1alpha1.InviteSpec{
					Email: "empty@acme.corp",
				},
			},
			expectErr: true,
			setupK8sMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo"), mock.Anything).
					RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						accountInfo := &pmcorev1alpha1.AccountInfo{
							Spec: pmcorev1alpha1.AccountInfoSpec{
								Organization: pmcorev1alpha1.AccountLocation{
									Name: "",
								},
							},
						}
						*o.(*pmcorev1alpha1.AccountInfo) = *accountInfo
						return nil
					}).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux) {
			},
		},
		{
			desc: "AccountInfo OIDC is nil",
			obj: &pmcorev1alpha1.Invite{
				Spec: pmcorev1alpha1.InviteSpec{
					Email: "oidcnil@acme.corp",
				},
			},
			expectErr: true,
			setupK8sMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo"), mock.Anything).
					RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						accountInfo := &pmcorev1alpha1.AccountInfo{
							Spec: pmcorev1alpha1.AccountInfoSpec{
								Organization: pmcorev1alpha1.AccountLocation{
									Name: "acme",
								},
								OIDC: nil,
							},
						}
						*o.(*pmcorev1alpha1.AccountInfo) = *accountInfo
						return nil
					}).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux) {
				users := []map[string]any{}
				mux.HandleFunc("GET /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(&users)
				})
			},
		},
		{
			desc: "organization not found in OIDC clients",
			obj: &pmcorev1alpha1.Invite{
				Spec: pmcorev1alpha1.InviteSpec{
					Email: "missingorg@acme.corp",
				},
			},
			expectErr: true,
			setupK8sMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo"), mock.Anything).
					RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						accountInfo := &pmcorev1alpha1.AccountInfo{
							Spec: pmcorev1alpha1.AccountInfoSpec{
								Organization: pmcorev1alpha1.AccountLocation{
									Name: "acme",
								},
								OIDC: &pmcorev1alpha1.OIDCInfo{
									IssuerURL: "https://keycloak/realms/acme",
									Clients:   map[string]pmcorev1alpha1.ClientInfo{},
								},
							},
						}
						*o.(*pmcorev1alpha1.AccountInfo) = *accountInfo
						return nil
					}).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux) {
				users := []map[string]any{}
				mux.HandleFunc("GET /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(&users)
				})
			},
		},
		{
			desc: "user already exists, skipping invite",
			obj:  &pmcorev1alpha1.Invite{Spec: pmcorev1alpha1.InviteSpec{Email: "existing@acme.corp"}},
			setupK8sMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo"), mock.Anything).
					RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						*o.(*pmcorev1alpha1.AccountInfo) = pmcorev1alpha1.AccountInfo{
							Spec: pmcorev1alpha1.AccountInfoSpec{
								Organization: pmcorev1alpha1.AccountLocation{Name: "acme"},
							},
						}
						return nil
					}).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux) {
				mux.HandleFunc("GET /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "existing-id", "email": "existing@acme.corp"}})
				})
			},
			expectErr: false,
		},
		{
			desc: "GET users returns invalid JSON",
			obj:  &pmcorev1alpha1.Invite{Spec: pmcorev1alpha1.InviteSpec{Email: "test@acme.corp"}},
			setupK8sMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo"), mock.Anything).
					RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						*o.(*pmcorev1alpha1.AccountInfo) = pmcorev1alpha1.AccountInfo{
							Spec: pmcorev1alpha1.AccountInfoSpec{
								Organization: pmcorev1alpha1.AccountLocation{Name: "acme"},
							},
						}
						return nil
					}).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux) {
				mux.HandleFunc("GET /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("not-valid-json"))
				})
			},
			expectErr: true,
		},
		{
			desc: "GET clients returns invalid JSON",
			obj:  &pmcorev1alpha1.Invite{Spec: pmcorev1alpha1.InviteSpec{Email: "test@acme.corp"}},
			setupK8sMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo"), mock.Anything).
					RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						*o.(*pmcorev1alpha1.AccountInfo) = pmcorev1alpha1.AccountInfo{
							Spec: pmcorev1alpha1.AccountInfoSpec{
								Organization: pmcorev1alpha1.AccountLocation{Name: "acme"},
								OIDC: &pmcorev1alpha1.OIDCInfo{
									Clients: map[string]pmcorev1alpha1.ClientInfo{"acme": {ClientID: "acme"}},
								},
							},
						}
						return nil
					}).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux) {
				mux.HandleFunc("GET /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode([]map[string]any{})
				})
				mux.HandleFunc("GET /admin/realms/acme/clients", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("not-valid-json"))
				})
			},
			expectErr: true,
		},
		{
			desc: "POST users returns non-201 status",
			obj:  &pmcorev1alpha1.Invite{Spec: pmcorev1alpha1.InviteSpec{Email: "test@acme.corp"}},
			setupK8sMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo"), mock.Anything).
					RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						*o.(*pmcorev1alpha1.AccountInfo) = pmcorev1alpha1.AccountInfo{
							Spec: pmcorev1alpha1.AccountInfoSpec{
								Organization: pmcorev1alpha1.AccountLocation{Name: "acme"},
								OIDC: &pmcorev1alpha1.OIDCInfo{
									Clients: map[string]pmcorev1alpha1.ClientInfo{"acme": {ClientID: "acme"}},
								},
							},
						}
						return nil
					}).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux) {
				mux.HandleFunc("GET /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode([]map[string]any{})
				})
				mux.HandleFunc("GET /admin/realms/acme/clients", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "client-uuid", "clientId": "acme"}})
				})
				mux.HandleFunc("POST /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusBadRequest)
				})
			},
			expectErr: true,
		},
		{
			desc: "second GET users returns non-200",
			obj:  &pmcorev1alpha1.Invite{Spec: pmcorev1alpha1.InviteSpec{Email: "test@acme.corp"}},
			setupK8sMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo"), mock.Anything).
					RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						*o.(*pmcorev1alpha1.AccountInfo) = pmcorev1alpha1.AccountInfo{
							Spec: pmcorev1alpha1.AccountInfoSpec{
								Organization: pmcorev1alpha1.AccountLocation{Name: "acme"},
								OIDC: &pmcorev1alpha1.OIDCInfo{
									Clients: map[string]pmcorev1alpha1.ClientInfo{"acme": {ClientID: "acme"}},
								},
							},
						}
						return nil
					}).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux) {
				callCount := 0
				mux.HandleFunc("GET /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					callCount++
					w.Header().Set("Content-Type", "application/json")
					if callCount == 1 {
						w.WriteHeader(http.StatusOK)
						_ = json.NewEncoder(w).Encode([]map[string]any{})
					} else {
						w.WriteHeader(http.StatusInternalServerError)
					}
				})
				mux.HandleFunc("GET /admin/realms/acme/clients", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "client-uuid", "clientId": "acme"}})
				})
				mux.HandleFunc("POST /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusCreated)
				})
			},
			expectErr: true,
		},
		{
			desc: "second GET users returns invalid JSON",
			obj:  &pmcorev1alpha1.Invite{Spec: pmcorev1alpha1.InviteSpec{Email: "test@acme.corp"}},
			setupK8sMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo"), mock.Anything).
					RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						*o.(*pmcorev1alpha1.AccountInfo) = pmcorev1alpha1.AccountInfo{
							Spec: pmcorev1alpha1.AccountInfoSpec{
								Organization: pmcorev1alpha1.AccountLocation{Name: "acme"},
								OIDC: &pmcorev1alpha1.OIDCInfo{
									Clients: map[string]pmcorev1alpha1.ClientInfo{"acme": {ClientID: "acme"}},
								},
							},
						}
						return nil
					}).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux) {
				callCount := 0
				mux.HandleFunc("GET /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					callCount++
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					if callCount == 1 {
						_ = json.NewEncoder(w).Encode([]map[string]any{})
					} else {
						_, _ = w.Write([]byte("not-valid-json"))
					}
				})
				mux.HandleFunc("GET /admin/realms/acme/clients", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "client-uuid", "clientId": "acme"}})
				})
				mux.HandleFunc("POST /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusCreated)
				})
			},
			expectErr: true,
		},
		{
			desc: "PUT execute-actions-email returns non-204",
			obj:  &pmcorev1alpha1.Invite{Spec: pmcorev1alpha1.InviteSpec{Email: "test@acme.corp"}},
			setupK8sMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo"), mock.Anything).
					RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						*o.(*pmcorev1alpha1.AccountInfo) = pmcorev1alpha1.AccountInfo{
							Spec: pmcorev1alpha1.AccountInfoSpec{
								Organization: pmcorev1alpha1.AccountLocation{Name: "acme"},
								OIDC: &pmcorev1alpha1.OIDCInfo{
									Clients: map[string]pmcorev1alpha1.ClientInfo{"acme": {ClientID: "acme"}},
								},
							},
						}
						return nil
					}).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux) {
				users := []map[string]any{}
				mux.HandleFunc("GET /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(users)
				})
				mux.HandleFunc("GET /admin/realms/acme/clients", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "client-uuid", "clientId": "acme"}})
				})
				mux.HandleFunc("POST /admin/realms/acme/users", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusCreated)
					users = append(users, map[string]any{"id": "new-user-id", "email": "test@acme.corp"})
				})
				mux.HandleFunc("PUT /admin/realms/acme/users/{id}/execute-actions-email", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK) // should be 204
				})
			},
			expectErr: true,
		},
	}
	for _, test := range testCases {
		t.Run(test.desc, func(t *testing.T) {
			mux := http.NewServeMux()
			srv := httptest.NewServer(mux)
			defer srv.Close()

			configureOIDCProvider(t, mux, srv.URL)
			ctx := context.WithValue(t.Context(), oauth2.HTTPClient, srv.Client())

			k8s := mocks.NewMockClient(t)
			if test.setupK8sMocks != nil {
				test.setupK8sMocks(k8s)
			}

			kcpClientGetter := mocks.NewMockKCPClientGetter(t)
			kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, "cluster1").Return(k8s, nil).Maybe()

			if test.setupKeycloakMocks != nil {
				test.setupKeycloakMocks(mux)
			}

			cfg := &config.Config{
				Keycloak: config.KeycloakConfig{
					BaseURL:  srv.URL,
					ClientID: "security-operator",
				},
				BaseDomain: "portal.dev.local:8443",
			}
			if test.config != nil {
				cfg.SetDefaultPassword = test.config.SetDefaultPassword
			}

			s, err := invite.New(ctx, cfg, kcpClientGetter)
			assert.NoError(t, err)

			l := testlogger.New()
			ctx = l.WithContext(t.Context())

			ctx = mccontext.WithCluster(ctx, "cluster1")

			_, processErr := s.Process(ctx, test.obj)
			if test.expectErr {
				assert.NotNil(t, processErr, "expected an error")
			} else {
				assert.Nil(t, processErr, "did not expect an error")
			}
		})
	}
}

func TestInviteNew_OIDCProviderError(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Deliberately omit OIDC discovery endpoint so oidc.NewProvider fails.
	ctx := context.WithValue(t.Context(), oauth2.HTTPClient, srv.Client())

	_, err := invite.New(ctx, &config.Config{
		Keycloak: config.KeycloakConfig{BaseURL: srv.URL, ClientID: "security-operator"},
	}, nil) //nolint:staticcheck
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "creating OIDC provider")
}

func TestInviteSubroutine_NoClusterInContext(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	configureOIDCProvider(t, mux, srv.URL)
	ctx := context.WithValue(t.Context(), oauth2.HTTPClient, srv.Client())

	kcpClientGetter := mocks.NewMockKCPClientGetter(t)
	s, err := invite.New(ctx, &config.Config{
		Keycloak:   config.KeycloakConfig{BaseURL: srv.URL, ClientID: "security-operator"},
		BaseDomain: "portal.dev.local",
	}, kcpClientGetter)
	assert.NoError(t, err)

	l := testlogger.New()
	// Deliberately omit mccontext.WithCluster so ClusterFrom returns !ok.
	ctx = l.WithContext(t.Context())

	_, processErr := s.Process(ctx, &pmcorev1alpha1.Invite{Spec: pmcorev1alpha1.InviteSpec{Email: "test@test.com"}})
	assert.Error(t, processErr)
	assert.Contains(t, processErr.Error(), "failed to get cluster from context")
}

func TestInviteSubroutine_GetClientFails(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	configureOIDCProvider(t, mux, srv.URL)
	ctx := context.WithValue(t.Context(), oauth2.HTTPClient, srv.Client())

	kcpClientGetter := mocks.NewMockKCPClientGetter(t)
	kcpClientGetter.EXPECT().NewClientForLogicalCluster(mock.Anything, "cluster1").Return(nil, fmt.Errorf("cluster not found"))

	s, err := invite.New(ctx, &config.Config{
		Keycloak:   config.KeycloakConfig{BaseURL: srv.URL, ClientID: "security-operator"},
		BaseDomain: "portal.dev.local",
	}, kcpClientGetter)
	assert.NoError(t, err)

	l := testlogger.New()
	ctx = l.WithContext(t.Context())
	ctx = mccontext.WithCluster(ctx, "cluster1")

	_, processErr := s.Process(ctx, &pmcorev1alpha1.Invite{Spec: pmcorev1alpha1.InviteSpec{Email: "test@test.com"}})
	assert.Error(t, processErr)
	assert.Contains(t, processErr.Error(), "failed to get client for cluster")
}

func TestHelperFunctions(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	configureOIDCProvider(t, mux, srv.URL)
	ctx := context.WithValue(t.Context(), oauth2.HTTPClient, srv.Client())

	s, err := invite.New(ctx, &config.Config{
		Keycloak: config.KeycloakConfig{
			BaseURL:  srv.URL,
			ClientID: "security-operator",
		},
	}, nil) //nolint:staticcheck
	assert.NoError(t, err)

	assert.Equal(t, "Invite", s.GetName())
}
