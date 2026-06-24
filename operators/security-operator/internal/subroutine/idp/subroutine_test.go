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

package idp_test

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
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/subroutine/idp"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
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

func getTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(pmcorev1alpha1.AddToScheme(scheme))
	return scheme
}

func setupManagerAndCluster(t *testing.T, orgsClient ctrlruntimeclient.Client, initialObjects ...ctrlruntimeclient.Object) (*mocks.MockManager, *mocks.MockCluster, ctrlruntimeclient.Client) {
	scheme := getTestScheme()

	kcpClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&pmcorev1alpha1.IdentityProviderConfiguration{}).
		WithObjects(initialObjects...).
		Build()

	mgr := mocks.NewMockManager(t)
	cluster := mocks.NewMockCluster(t)
	orgsCluster := mocks.NewMockCluster(t)

	mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Maybe()
	mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName(string(config.MultiProviderName(config.CoreProviderName, config.OrgsClusterPath)))).Return(orgsCluster, nil).Maybe()
	cluster.EXPECT().GetClient().Return(kcpClient).Maybe()
	orgsCluster.EXPECT().GetClient().Return(orgsClient).Maybe()

	return mgr, cluster, kcpClient
}

func getTestConfig(cfg *config.Config, baseURL string) *config.Config {
	if cfg == nil {
		return &config.Config{
			Keycloak: config.KeycloakConfig{
				BaseURL:  baseURL,
				ClientID: "security-operator",
			},
		}
	}
	cfg.Keycloak.BaseURL = baseURL
	return cfg
}

func TestSubroutineProcess(t *testing.T) {
	testCases := []struct {
		desc               string
		obj                ctrlruntimeclient.Object
		cfg                *config.Config
		setupK8sMocks      func(m *mocks.MockClient, kcpClient ctrlruntimeclient.Client)
		setupKeycloakMocks func(mux *http.ServeMux, baseURL string)
		setupManager       func(t *testing.T, orgsClient ctrlruntimeclient.Client, initialObjects []ctrlruntimeclient.Object) (*mocks.MockManager, ctrlruntimeclient.Client)
		expectNewErr       bool
		expectErr          bool
	}{
		{
			desc: "realm and client created successfully without SMTP",
			obj: &pmcorev1alpha1.IdentityProviderConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-realm",
				},
				Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
					Clients: []pmcorev1alpha1.IdentityProviderClientConfig{
						{
							ClientName:   "test-realm",
							ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
							RedirectURIs: []string{"https://test.example.com/*"},
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-test-realm",
								Namespace: "default",
							},
						},
					},
				},
			},
			cfg: &config.Config{
				Keycloak: config.KeycloakConfig{
					BaseURL:  "http://localhost",
					ClientID: "security-operator",
				},
			},
			setupK8sMocks: func(m *mocks.MockClient, kcpClient ctrlruntimeclient.Client) {
				m.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Maybe()
				m.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Maybe()
				m.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "portal-client-secret-test-realm")).Maybe()
			},
			setupKeycloakMocks: func(mux *http.ServeMux, baseURL string) {
				mux.HandleFunc("POST /admin/realms", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusCreated)
				})
				mux.HandleFunc("GET /admin/realms/test-realm/clients", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					clients := []map[string]any{}
					_ = json.NewEncoder(w).Encode(clients)
				})
				mux.HandleFunc("POST /admin/realms/test-realm/clients-initial-access", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]string{"token": "initial-access-token"})
				})
				mux.HandleFunc("POST /realms/test-realm/clients-registrations/openid-connect", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]string{
						"client_id":                 "generated-client-id-123",
						"client_secret":             "client-secret-123",
						"registration_access_token": "registration-token-123",
						"registration_client_uri":   fmt.Sprintf("%s/realms/test-realm/clients-registrations/openid-connect/generated-client-id-123", baseURL),
					})
				})
			},
		},
		{
			desc: "realm already exists",
			obj: &pmcorev1alpha1.IdentityProviderConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "existing-realm",
				},
				Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
					Clients: []pmcorev1alpha1.IdentityProviderClientConfig{
						{
							ClientName:   "existing-realm",
							ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
							RedirectURIs: []string{"https://test.example.com/*"},
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-existing-realm",
								Namespace: "default",
							},
						},
					},
				},
			},
			cfg: &config.Config{
				Keycloak: config.KeycloakConfig{
					BaseURL:  "http://localhost",
					ClientID: "security-operator",
				},
			},
			setupK8sMocks: func(m *mocks.MockClient, kcpClient ctrlruntimeclient.Client) {
				m.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Maybe()
				m.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Maybe()
				m.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "portal-client-secret-existing-realm")).Maybe()
			},
			setupKeycloakMocks: func(mux *http.ServeMux, baseURL string) {
				mux.HandleFunc("POST /admin/realms", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusConflict)
				})
				mux.HandleFunc("PUT /admin/realms/existing-realm", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNoContent)
				})
				mux.HandleFunc("GET /admin/realms/existing-realm/clients", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					clients := []map[string]any{}
					_ = json.NewEncoder(w).Encode(clients)
				})
				mux.HandleFunc("POST /admin/realms/existing-realm/clients-initial-access", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]string{"token": "initial-access-token"})
				})
				mux.HandleFunc("POST /realms/existing-realm/clients-registrations/openid-connect", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]string{
						"client_id":                 "generated-client-id-existing",
						"client_secret":             "client-secret-existing",
						"registration_access_token": "registration-token-existing",
						"registration_client_uri":   fmt.Sprintf("%s/realms/existing-realm/clients-registrations/openid-connect/generated-client-id-existing", baseURL),
					})
				})
			},
		},
		{
			desc: "client already exists - update path",
			obj: &pmcorev1alpha1.IdentityProviderConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-realm",
				},
				Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
					Clients: []pmcorev1alpha1.IdentityProviderClientConfig{
						{
							ClientName:   "test-realm",
							ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
							RedirectURIs: []string{"https://test.example.com/*"},
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-test-realm",
								Namespace: "default",
							},
						},
					},
				},
				Status: pmcorev1alpha1.IdentityProviderConfigurationStatus{
					ManagedClients: map[string]pmcorev1alpha1.ManagedClient{
						"test-realm": {
							ClientID:              "existing-client-id",
							RegistrationClientURI: "http://localhost/realms/test-realm/clients-registrations/openid-connect/existing-client-id",
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-test-realm",
								Namespace: "default",
							},
						},
					},
				},
			},
			cfg: &config.Config{
				Keycloak: config.KeycloakConfig{
					BaseURL:  "http://localhost",
					ClientID: "security-operator",
				},
			},
			setupK8sMocks: func(m *mocks.MockClient, kcpClient ctrlruntimeclient.Client) {
				m.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Maybe()
				m.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Maybe()
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "portal-client-secret-test-realm",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"registration_access_token": []byte("existing-registration-token"),
					},
				}
				m.EXPECT().Get(mock.Anything, mock.MatchedBy(func(key ctrlruntimeclient.ObjectKey) bool {
					return key.Name == "portal-client-secret-test-realm" && key.Namespace == "default"
				}), mock.AnythingOfType("*v1.Secret")).Run(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) {
					s := obj.(*corev1.Secret)
					*s = *secret
				}).Return(nil).Maybe()
			},
			setupKeycloakMocks: func(mux *http.ServeMux, baseURL string) {
				mux.HandleFunc("POST /admin/realms", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusCreated)
				})
				mux.HandleFunc("GET /admin/realms/test-realm/clients", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					clients := []map[string]any{
						{"clientId": "existing-client-id", "name": "test-realm"},
					}
					_ = json.NewEncoder(w).Encode(clients)
				})
				mux.HandleFunc("PUT /realms/test-realm/clients-registrations/openid-connect/existing-client-id", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]string{
						"client_id":                 "existing-client-id",
						"client_secret":             "existing-secret",
						"registration_access_token": "updated-token",
						"registration_client_uri":   fmt.Sprintf("%s/realms/test-realm/clients-registrations/openid-connect/existing-client-id", baseURL),
					})
				})
			},
		},
		{
			desc: "update client with 401 retry",
			obj: &pmcorev1alpha1.IdentityProviderConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-realm",
				},
				Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
					Clients: []pmcorev1alpha1.IdentityProviderClientConfig{
						{
							ClientName:   "test-realm",
							ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
							RedirectURIs: []string{"https://test.example.com/*"},
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-test-realm",
								Namespace: "default",
							},
						},
					},
				},
				Status: pmcorev1alpha1.IdentityProviderConfigurationStatus{
					ManagedClients: map[string]pmcorev1alpha1.ManagedClient{
						"test-realm": {
							ClientID:              "existing-client-id",
							RegistrationClientURI: "http://localhost/realms/test-realm/clients-registrations/openid-connect/existing-client-id",
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-test-realm",
								Namespace: "default",
							},
						},
					},
				},
			},
			cfg: &config.Config{
				Keycloak: config.KeycloakConfig{
					BaseURL:  "http://localhost",
					ClientID: "security-operator",
				},
			},
			setupK8sMocks: func(m *mocks.MockClient, kcpClient ctrlruntimeclient.Client) {
				m.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Maybe()
				m.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Maybe()
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "portal-client-secret-test-realm",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"registration_access_token": []byte("stale-token"),
					},
				}
				m.EXPECT().Get(mock.Anything, mock.MatchedBy(func(key ctrlruntimeclient.ObjectKey) bool {
					return key.Name == "portal-client-secret-test-realm" && key.Namespace == "default"
				}), mock.AnythingOfType("*v1.Secret")).Run(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) {
					s := obj.(*corev1.Secret)
					*s = *secret
				}).Return(nil).Maybe()
			},
			setupKeycloakMocks: func(mux *http.ServeMux, baseURL string) {
				mux.HandleFunc("POST /admin/realms", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusCreated)
				})
				mux.HandleFunc("GET /admin/realms/test-realm/clients", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					clients := []map[string]any{
						{"id": "existing-client-uuid", "clientId": "existing-client-id", "name": "test-realm"},
					}
					_ = json.NewEncoder(w).Encode(clients)
				})
				mux.HandleFunc("POST /admin/realms/test-realm/clients/existing-client-uuid/registration-access-token", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]string{"registrationAccessToken": "new-registration-token"})
				})
				putCallCount := 0
				mux.HandleFunc("PUT /realms/test-realm/clients-registrations/openid-connect/existing-client-id", func(w http.ResponseWriter, r *http.Request) {
					putCallCount++
					if putCallCount == 1 {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]string{
						"client_id":                 "existing-client-id",
						"client_secret":             "existing-secret",
						"registration_access_token": "updated-token",
						"registration_client_uri":   fmt.Sprintf("%s/realms/test-realm/clients-registrations/openid-connect/existing-client-id", baseURL),
					})
				})
			},
		},
		{
			desc: "client in status but not in spec - deletion",
			obj: &pmcorev1alpha1.IdentityProviderConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-realm",
				},
				Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
					Clients: []pmcorev1alpha1.IdentityProviderClientConfig{
						{
							ClientName:   "new-client",
							ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
							RedirectURIs: []string{"https://test.example.com/*"},
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-new-client",
								Namespace: "default",
							},
						},
					},
				},
				Status: pmcorev1alpha1.IdentityProviderConfigurationStatus{
					ManagedClients: map[string]pmcorev1alpha1.ManagedClient{
						"old-client": {
							ClientID:              "old-client-id",
							RegistrationClientURI: "",
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-old-client",
								Namespace: "default",
							},
						},
					},
				},
			},
			cfg: &config.Config{
				Keycloak: config.KeycloakConfig{
					BaseURL:  "http://localhost",
					ClientID: "security-operator",
				},
			},
			setupK8sMocks: func(m *mocks.MockClient, kcpClient ctrlruntimeclient.Client) {
				m.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Maybe()
				m.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Maybe()
				oldClientSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "portal-client-secret-old-client",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"registration_access_token": []byte("delete-token"),
					},
				}
				m.EXPECT().Get(mock.Anything, mock.MatchedBy(func(key ctrlruntimeclient.ObjectKey) bool {
					return key.Name == "portal-client-secret-old-client" && key.Namespace == "default"
				}), mock.AnythingOfType("*v1.Secret")).Run(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) {
					s := obj.(*corev1.Secret)
					*s = *oldClientSecret
				}).Return(nil).Maybe()
				m.EXPECT().Get(mock.Anything, mock.MatchedBy(func(key ctrlruntimeclient.ObjectKey) bool {
					return key.Name == "portal-client-secret-new-client" && key.Namespace == "default"
				}), mock.AnythingOfType("*v1.Secret")).Return(apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "portal-client-secret-new-client")).Maybe()
				m.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil).Maybe()
			},
			setupKeycloakMocks: func(mux *http.ServeMux, baseURL string) {
				mux.HandleFunc("POST /admin/realms", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusCreated)
				})
				mux.HandleFunc("DELETE /realms/test-realm/clients-registrations/openid-connect/old-client-id", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNoContent)
				})
				mux.HandleFunc("GET /admin/realms/test-realm/clients", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					clients := []map[string]any{}
					_ = json.NewEncoder(w).Encode(clients)
				})
				mux.HandleFunc("POST /admin/realms/test-realm/clients-initial-access", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]string{"token": "initial-access-token"})
				})
				mux.HandleFunc("POST /realms/test-realm/clients-registrations/openid-connect", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]string{
						"client_id":                 "new-client-id",
						"client_secret":             "new-client-secret",
						"registration_access_token": "registration-token",
						"registration_client_uri":   fmt.Sprintf("%s/realms/test-realm/clients-registrations/openid-connect/new-client-id", baseURL),
					})
				})
			},
		},
		{
			desc: "error deleting client in status but not in spec",
			obj: &pmcorev1alpha1.IdentityProviderConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-realm",
				},
				Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
					Clients: []pmcorev1alpha1.IdentityProviderClientConfig{},
				},
				Status: pmcorev1alpha1.IdentityProviderConfigurationStatus{
					ManagedClients: map[string]pmcorev1alpha1.ManagedClient{
						"old-client": {
							ClientID:              "old-client-id",
							RegistrationClientURI: "http://localhost/realms/test-realm/clients-registrations/openid-connect/old-client-id",
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-old-client",
								Namespace: "default",
							},
						},
					},
				},
			},
			cfg: &config.Config{
				Keycloak: config.KeycloakConfig{
					BaseURL:  "http://localhost",
					ClientID: "security-operator",
				},
			},
			expectErr: true,
			setupK8sMocks: func(m *mocks.MockClient, kcpClient ctrlruntimeclient.Client) {
				m.EXPECT().Get(mock.Anything, mock.MatchedBy(func(key ctrlruntimeclient.ObjectKey) bool {
					return key.Name == "portal-client-secret-old-client" && key.Namespace == "default"
				}), mock.AnythingOfType("*v1.Secret")).Return(fmt.Errorf("failed to get secret")).Maybe()
			},
			setupKeycloakMocks: func(mux *http.ServeMux, baseURL string) {
				mux.HandleFunc("POST /admin/realms", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusCreated)
				})
			},
		},
		{
			desc: "error creating realm",
			obj: &pmcorev1alpha1.IdentityProviderConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "error-realm",
				},
				Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
					Clients: []pmcorev1alpha1.IdentityProviderClientConfig{
						{
							ClientName:   "error-realm",
							ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
							RedirectURIs: []string{"https://test.example.com/*"},
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-error-realm",
								Namespace: "default",
							},
						},
					},
				},
			},
			cfg: &config.Config{
				Keycloak: config.KeycloakConfig{
					BaseURL:  "http://localhost",
					ClientID: "security-operator",
				},
			},
			expectErr: true,
			setupK8sMocks: func(m *mocks.MockClient, kcpClient ctrlruntimeclient.Client) {
			},
			setupKeycloakMocks: func(mux *http.ServeMux, baseURL string) {
				mux.HandleFunc("POST /admin/realms", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":"internal server error"}`))
				})
			},
		},
		{
			desc: "error getting client ID",
			obj: &pmcorev1alpha1.IdentityProviderConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-realm",
				},
				Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
					Clients: []pmcorev1alpha1.IdentityProviderClientConfig{
						{
							ClientName:   "test-realm",
							ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
							RedirectURIs: []string{"https://test.example.com/*"},
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-test-realm",
								Namespace: "default",
							},
						},
					},
				},
			},
			cfg: &config.Config{
				Keycloak: config.KeycloakConfig{
					BaseURL:  "http://localhost",
					ClientID: "security-operator",
				},
			},
			expectErr: true,
			setupK8sMocks: func(m *mocks.MockClient, kcpClient ctrlruntimeclient.Client) {
			},
			setupKeycloakMocks: func(mux *http.ServeMux, baseURL string) {
				mux.HandleFunc("POST /admin/realms", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusCreated)
				})
				mux.HandleFunc("GET /admin/realms/test-realm/clients", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				})
			},
		},
		{
			desc: "error getting Initial Access Token",
			obj: &pmcorev1alpha1.IdentityProviderConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-realm",
				},
				Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
					Clients: []pmcorev1alpha1.IdentityProviderClientConfig{
						{
							ClientName:   "test-realm",
							ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
							RedirectURIs: []string{"https://test.example.com/*"},
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-test-realm",
								Namespace: "default",
							},
						},
					},
				},
			},
			cfg: &config.Config{
				Keycloak: config.KeycloakConfig{
					BaseURL:  "http://localhost",
					ClientID: "security-operator",
				},
			},
			expectErr: true,
			setupK8sMocks: func(m *mocks.MockClient, kcpClient ctrlruntimeclient.Client) {
			},
			setupKeycloakMocks: func(mux *http.ServeMux, baseURL string) {
				mux.HandleFunc("POST /admin/realms", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusCreated)
				})
				mux.HandleFunc("GET /admin/realms/test-realm/clients", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					clients := []map[string]any{}
					_ = json.NewEncoder(w).Encode(clients)
				})
				mux.HandleFunc("POST /admin/realms/test-realm/clients-initial-access", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":"internal server error"}`))
				})
			},
		},
		{
			desc: "error updating realm when conflict",
			obj: &pmcorev1alpha1.IdentityProviderConfiguration{
				ObjectMeta: metav1.ObjectMeta{Name: "conflict-realm"},
				Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
					Clients: []pmcorev1alpha1.IdentityProviderClientConfig{{
						ClientName:   "conflict-realm",
						ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
						RedirectURIs: []string{"https://test.example.com/*"},
						SecretRef:    corev1.SecretReference{Name: "portal-client-secret-conflict-realm", Namespace: "default"},
					}},
				},
			},
			cfg:           &config.Config{Keycloak: config.KeycloakConfig{BaseURL: "http://localhost", ClientID: "security-operator"}},
			expectErr:     true,
			setupK8sMocks: func(m *mocks.MockClient, kcpClient ctrlruntimeclient.Client) {},
			setupKeycloakMocks: func(mux *http.ServeMux, baseURL string) {
				mux.HandleFunc("POST /admin/realms", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusConflict) })
				mux.HandleFunc("PUT /admin/realms/conflict-realm", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
			},
		},
		{
			desc: "error regenerating registration access token",
			obj: &pmcorev1alpha1.IdentityProviderConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-realm",
				},
				Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
					Clients: []pmcorev1alpha1.IdentityProviderClientConfig{
						{
							ClientName:   "test-realm",
							ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
							RedirectURIs: []string{"https://test.example.com/*"},
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-test-realm",
								Namespace: "default",
							},
						},
					},
				},
			},
			cfg: &config.Config{
				Keycloak: config.KeycloakConfig{
					BaseURL:  "http://localhost",
					ClientID: "security-operator",
				},
			},
			expectErr: true,
			setupK8sMocks: func(m *mocks.MockClient, kcpClient ctrlruntimeclient.Client) {
				m.EXPECT().Get(mock.Anything, mock.MatchedBy(func(key ctrlruntimeclient.ObjectKey) bool {
					return key.Name == "portal-client-secret-test-realm" && key.Namespace == "default"
				}), mock.AnythingOfType("*v1.Secret")).Return(fmt.Errorf("failed to get secret")).Maybe()
			},
			setupKeycloakMocks: func(mux *http.ServeMux, baseURL string) {
				mux.HandleFunc("POST /admin/realms", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusCreated)
				})
				mux.HandleFunc("GET /admin/realms/test-realm/clients", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					clients := []map[string]any{
						{"clientId": "existing-client-id", "name": "test-realm"},
					}
					_ = json.NewEncoder(w).Encode(clients)
				})
			},
		},
		{
			desc: "error client registration non-201 status",
			obj: &pmcorev1alpha1.IdentityProviderConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-realm",
				},
				Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
					Clients: []pmcorev1alpha1.IdentityProviderClientConfig{
						{
							ClientName:   "test-realm",
							ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
							RedirectURIs: []string{"https://test.example.com/*"},
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-test-realm",
								Namespace: "default",
							},
						},
					},
				},
			},
			cfg: &config.Config{
				Keycloak: config.KeycloakConfig{
					BaseURL:  "http://localhost",
					ClientID: "security-operator",
				},
			},
			expectErr: true,
			setupK8sMocks: func(m *mocks.MockClient, kcpClient ctrlruntimeclient.Client) {
			},
			setupKeycloakMocks: func(mux *http.ServeMux, baseURL string) {
				mux.HandleFunc("POST /admin/realms", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusCreated)
				})
				mux.HandleFunc("GET /admin/realms/test-realm/clients", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					clients := []map[string]any{}
					_ = json.NewEncoder(w).Encode(clients)
				})
				mux.HandleFunc("POST /admin/realms/test-realm/clients-initial-access", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]string{"token": "initial-access-token"})
				})
				mux.HandleFunc("POST /realms/test-realm/clients-registrations/openid-connect", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"bad request"}`))
				})
			},
		},
		{
			desc: "error unmarshaling updateClient response",
			obj: &pmcorev1alpha1.IdentityProviderConfiguration{
				ObjectMeta: metav1.ObjectMeta{Name: "test-realm"},
				Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
					Clients: []pmcorev1alpha1.IdentityProviderClientConfig{{
						ClientName:   "test-realm",
						ClientType:   pmcorev1alpha1.IdentityProviderClientTypeConfidential,
						RedirectURIs: []string{"https://test.example.com/*"},
						SecretRef:    corev1.SecretReference{Name: "portal-client-secret-test-realm", Namespace: "default"},
					}},
				},
			},
			cfg:       &config.Config{Keycloak: config.KeycloakConfig{BaseURL: "http://localhost", ClientID: "security-operator"}},
			expectErr: true,
			setupK8sMocks: func(m *mocks.MockClient, kcpClient ctrlruntimeclient.Client) {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "portal-client-secret-test-realm",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"registration_access_token": []byte("existing-registration-token"),
					},
				}
				m.EXPECT().Get(mock.Anything, mock.MatchedBy(func(key ctrlruntimeclient.ObjectKey) bool {
					return key.Name == "portal-client-secret-test-realm" && key.Namespace == "default"
				}), mock.AnythingOfType("*v1.Secret")).Run(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) {
					s := obj.(*corev1.Secret)
					*s = *secret
				}).Return(nil).Maybe()
			},
			setupKeycloakMocks: func(mux *http.ServeMux, baseURL string) {
				mux.HandleFunc("POST /admin/realms", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusCreated) })
				mux.HandleFunc("GET /admin/realms/test-realm/clients", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode([]map[string]any{{"clientId": "existing-client-id", "name": "test-realm"}})
				})
				mux.HandleFunc("PUT /realms/test-realm/clients-registrations/openid-connect/existing-client-id", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{invalid-json`))
				})
			},
		},
		{
			desc:               "error cluster from context",
			obj:                &pmcorev1alpha1.IdentityProviderConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "test-realm"}, Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{}},
			cfg:                &config.Config{Keycloak: config.KeycloakConfig{BaseURL: "http://localhost", ClientID: "security-operator"}},
			expectErr:          true,
			setupK8sMocks:      func(m *mocks.MockClient, kcpClient ctrlruntimeclient.Client) {},
			setupKeycloakMocks: func(mux *http.ServeMux, baseURL string) {},
			setupManager: func(t *testing.T, orgsClient ctrlruntimeclient.Client, initialObjects []ctrlruntimeclient.Object) (*mocks.MockManager, ctrlruntimeclient.Client) {
				mgr := mocks.NewMockManager(t)
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(nil, fmt.Errorf("cluster error")).Once()
				return mgr, fake.NewClientBuilder().WithScheme(getTestScheme()).Build()
			},
		},
		{
			desc: "error creating secret",
			obj: &pmcorev1alpha1.IdentityProviderConfiguration{
				ObjectMeta: metav1.ObjectMeta{Name: "test-realm"},
				Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
					Clients: []pmcorev1alpha1.IdentityProviderClientConfig{{
						ClientName: "test-realm", ClientType: pmcorev1alpha1.IdentityProviderClientTypeConfidential,
						RedirectURIs: []string{"https://test.example.com/*"},
						SecretRef:    corev1.SecretReference{Name: "portal-client-secret-test-realm", Namespace: "default"},
					}},
				},
			},
			cfg:       &config.Config{Keycloak: config.KeycloakConfig{BaseURL: "http://localhost", ClientID: "security-operator"}},
			expectErr: true,
			setupK8sMocks: func(m *mocks.MockClient, kcpClient ctrlruntimeclient.Client) {
				m.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "portal-client-secret-test-realm")).Maybe()
				m.EXPECT().Create(mock.Anything, mock.Anything).Return(fmt.Errorf("create error")).Maybe()
			},
			setupKeycloakMocks: func(mux *http.ServeMux, baseURL string) {
				mux.HandleFunc("POST /admin/realms", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusCreated) })
				mux.HandleFunc("GET /admin/realms/test-realm/clients", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode([]map[string]any{})
				})
				mux.HandleFunc("POST /admin/realms/test-realm/clients-initial-access", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]string{"token": "initial-access-token"})
				})
				mux.HandleFunc("POST /realms/test-realm/clients-registrations/openid-connect", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]string{"client_id": "client-id", "client_secret": "secret", "registration_access_token": "token", "registration_client_uri": fmt.Sprintf("%s/realms/test-realm/clients-registrations/openid-connect/client-id", baseURL)})
				})
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.desc, func(t *testing.T) {
			mux := http.NewServeMux()
			srv := httptest.NewServer(mux)
			defer srv.Close()

			configureOIDCProvider(t, mux, srv.URL)
			ctx := context.WithValue(context.Background(), oauth2.HTTPClient, srv.Client())

			orgsClient := mocks.NewMockClient(t)

			if test.setupKeycloakMocks != nil {
				test.setupKeycloakMocks(mux, srv.URL)
			}

			cfg := getTestConfig(test.cfg, srv.URL)

			var initialObjects []ctrlruntimeclient.Object
			if idpObj, ok := test.obj.(*pmcorev1alpha1.IdentityProviderConfiguration); ok {
				for clientName, managedClient := range idpObj.Status.ManagedClients {
					if managedClient.ClientID != "" {
						if idpObj.Status.ManagedClients == nil {
							idpObj.Status.ManagedClients = make(map[string]pmcorev1alpha1.ManagedClient)
						}
						idpObj.Status.ManagedClients[clientName] = pmcorev1alpha1.ManagedClient{
							ClientID:              managedClient.ClientID,
							RegistrationClientURI: fmt.Sprintf("%s/realms/%s/clients-registrations/openid-connect/%s", srv.URL, idpObj.Name, managedClient.ClientID),
							SecretRef:             managedClient.SecretRef,
						}
					}
				}
				initialObjects = append(initialObjects, idpObj.DeepCopy())
			}

			var mgr *mocks.MockManager
			var kcpClient ctrlruntimeclient.Client
			if test.setupManager != nil {
				mgr, kcpClient = test.setupManager(t, orgsClient, initialObjects)
			} else {
				mgr, _, kcpClient = setupManagerAndCluster(t, orgsClient, initialObjects...)
			}

			if test.setupK8sMocks != nil {
				test.setupK8sMocks(orgsClient, kcpClient)
			}

			s, err := idp.New(ctx, cfg, mgr, iclient.NewManagerKCPClientGetter(mgr, nil))
			if test.expectNewErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			l := testlogger.New()
			ctx = l.WithContext(ctx)

			_, opErr := s.Process(ctx, test.obj)
			if test.expectErr {
				assert.NotNil(t, opErr, "expected an operator error")
			} else {
				assert.Nil(t, opErr, "did not expect an operator error")
			}
		})
	}
}

func TestFinalize(t *testing.T) {
	testCases := []struct {
		desc               string
		obj                ctrlruntimeclient.Object
		cfg                *config.Config
		setupK8sMocks      func(m *mocks.MockClient)
		setupKeycloakMocks func(mux *http.ServeMux, baseURL string)
		expectErr          bool
	}{
		{
			desc: "finalize deletes client and realm successfully",
			obj: &pmcorev1alpha1.IdentityProviderConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-realm",
				},
				Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
					Clients: []pmcorev1alpha1.IdentityProviderClientConfig{
						{
							ClientName: "test-realm",
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-test-realm",
								Namespace: "default",
							},
						},
					},
				},
				Status: pmcorev1alpha1.IdentityProviderConfigurationStatus{
					ManagedClients: map[string]pmcorev1alpha1.ManagedClient{
						"test-realm": {
							ClientID:              "client-id-123",
							RegistrationClientURI: "http://localhost/realms/test-realm/clients-registrations/openid-connect/client-id-123",
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-test-realm",
								Namespace: "default",
							},
						},
					},
				},
			},
			cfg: &config.Config{
				Keycloak: config.KeycloakConfig{
					BaseURL:  "http://localhost",
					ClientID: "security-operator",
				},
			},
			setupK8sMocks: func(m *mocks.MockClient) {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "portal-client-secret-test-realm",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"registration_access_token": []byte("delete-token"),
					},
				}
				m.EXPECT().Get(mock.Anything, mock.MatchedBy(func(key ctrlruntimeclient.ObjectKey) bool {
					return key.Name == "portal-client-secret-test-realm" && key.Namespace == "default"
				}), mock.AnythingOfType("*v1.Secret")).Run(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) {
					s := obj.(*corev1.Secret)
					*s = *secret
				}).Return(nil).Once()
				m.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux, baseURL string) {
				mux.HandleFunc("DELETE /realms/test-realm/clients-registrations/openid-connect/client-id-123", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNoContent)
				})
				mux.HandleFunc("DELETE /admin/realms/test-realm", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNoContent)
				})
			},
		},
		{
			desc: "finalize error reading secret",
			obj: &pmcorev1alpha1.IdentityProviderConfiguration{
				ObjectMeta: metav1.ObjectMeta{Name: "test-realm"},
				Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
					Clients: []pmcorev1alpha1.IdentityProviderClientConfig{{
						ClientName: "test-realm",
						SecretRef:  corev1.SecretReference{Name: "portal-client-secret-test-realm", Namespace: "default"},
					}},
				},
				Status: pmcorev1alpha1.IdentityProviderConfigurationStatus{
					ManagedClients: map[string]pmcorev1alpha1.ManagedClient{
						"test-realm": {
							ClientID:              "client-id-123",
							RegistrationClientURI: "http://localhost/realms/test-realm/clients-registrations/openid-connect/client-id-123",
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-test-realm",
								Namespace: "default",
							},
						},
					},
				},
			},
			cfg:       &config.Config{Keycloak: config.KeycloakConfig{BaseURL: "http://localhost", ClientID: "security-operator"}},
			expectErr: true,
			setupK8sMocks: func(m *mocks.MockClient) {
				m.EXPECT().Get(mock.Anything, mock.MatchedBy(func(key ctrlruntimeclient.ObjectKey) bool {
					return key.Name == "portal-client-secret-test-realm" && key.Namespace == "default"
				}), mock.AnythingOfType("*v1.Secret")).Return(fmt.Errorf("get error")).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux, baseURL string) {},
		},
		{
			desc: "finalize error deleting secret",
			obj: &pmcorev1alpha1.IdentityProviderConfiguration{
				ObjectMeta: metav1.ObjectMeta{Name: "test-realm"},
				Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
					Clients: []pmcorev1alpha1.IdentityProviderClientConfig{{
						ClientName: "test-realm",
						SecretRef:  corev1.SecretReference{Name: "portal-client-secret-test-realm", Namespace: "default"},
					}},
				},
				Status: pmcorev1alpha1.IdentityProviderConfigurationStatus{
					ManagedClients: map[string]pmcorev1alpha1.ManagedClient{
						"test-realm": {
							ClientID:              "client-id-123",
							RegistrationClientURI: "http://localhost/realms/test-realm/clients-registrations/openid-connect/client-id-123",
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-test-realm",
								Namespace: "default",
							},
						},
					},
				},
			},
			cfg:       &config.Config{Keycloak: config.KeycloakConfig{BaseURL: "http://localhost", ClientID: "security-operator"}},
			expectErr: true,
			setupK8sMocks: func(m *mocks.MockClient) {
				secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "portal-client-secret-test-realm", Namespace: "default"}, Data: map[string][]byte{"registration_access_token": []byte("delete-token")}}
				m.EXPECT().Get(mock.Anything, mock.MatchedBy(func(key ctrlruntimeclient.ObjectKey) bool {
					return key.Name == "portal-client-secret-test-realm" && key.Namespace == "default"
				}), mock.AnythingOfType("*v1.Secret")).Run(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) {
					*obj.(*corev1.Secret) = *secret
				}).Return(nil).Once()
				m.EXPECT().Delete(mock.Anything, mock.Anything).Return(fmt.Errorf("delete error")).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux, baseURL string) {
				mux.HandleFunc("DELETE /realms/test-realm/clients-registrations/openid-connect/client-id-123", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
			},
		},
		{
			desc: "finalize error deleting client",
			obj: &pmcorev1alpha1.IdentityProviderConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-realm",
				},
				Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
					Clients: []pmcorev1alpha1.IdentityProviderClientConfig{
						{
							ClientName: "test-realm",
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-test-realm",
								Namespace: "default",
							},
						},
					},
				},
				Status: pmcorev1alpha1.IdentityProviderConfigurationStatus{
					ManagedClients: map[string]pmcorev1alpha1.ManagedClient{
						"test-realm": {
							ClientID:              "client-id-123",
							RegistrationClientURI: "http://localhost/realms/test-realm/clients-registrations/openid-connect/client-id-123",
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-test-realm",
								Namespace: "default",
							},
						},
					},
				},
			},
			cfg: &config.Config{
				Keycloak: config.KeycloakConfig{
					BaseURL:  "http://localhost",
					ClientID: "security-operator",
				},
			},
			expectErr: true,
			setupK8sMocks: func(m *mocks.MockClient) {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "portal-client-secret-test-realm",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"registration_access_token": []byte("delete-token"),
					},
				}
				m.EXPECT().Get(mock.Anything, mock.MatchedBy(func(key ctrlruntimeclient.ObjectKey) bool {
					return key.Name == "portal-client-secret-test-realm" && key.Namespace == "default"
				}), mock.AnythingOfType("*v1.Secret")).Run(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) {
					s := obj.(*corev1.Secret)
					*s = *secret
				}).Return(nil).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux, baseURL string) {
				mux.HandleFunc("DELETE /realms/test-realm/clients-registrations/openid-connect/client-id-123", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"bad request"}`))
				})
			},
		},
		{
			desc: "finalize error deleting realm",
			obj: &pmcorev1alpha1.IdentityProviderConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-realm",
				},
				Spec: pmcorev1alpha1.IdentityProviderConfigurationSpec{
					Clients: []pmcorev1alpha1.IdentityProviderClientConfig{
						{
							ClientName: "test-realm",
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-test-realm",
								Namespace: "default",
							},
						},
					},
				},
				Status: pmcorev1alpha1.IdentityProviderConfigurationStatus{
					ManagedClients: map[string]pmcorev1alpha1.ManagedClient{
						"test-realm": {
							ClientID:              "client-id-123",
							RegistrationClientURI: "http://localhost/realms/test-realm/clients-registrations/openid-connect/client-id-123",
							SecretRef: corev1.SecretReference{
								Name:      "portal-client-secret-test-realm",
								Namespace: "default",
							},
						},
					},
				},
			},
			cfg: &config.Config{
				Keycloak: config.KeycloakConfig{
					BaseURL:  "http://localhost",
					ClientID: "security-operator",
				},
			},
			expectErr: true,
			setupK8sMocks: func(m *mocks.MockClient) {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "portal-client-secret-test-realm",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"registration_access_token": []byte("delete-token"),
					},
				}
				m.EXPECT().Get(mock.Anything, mock.MatchedBy(func(key ctrlruntimeclient.ObjectKey) bool {
					return key.Name == "portal-client-secret-test-realm" && key.Namespace == "default"
				}), mock.AnythingOfType("*v1.Secret")).Run(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) {
					s := obj.(*corev1.Secret)
					*s = *secret
				}).Return(nil).Once()
				m.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil).Once()
			},
			setupKeycloakMocks: func(mux *http.ServeMux, baseURL string) {
				mux.HandleFunc("DELETE /realms/test-realm/clients-registrations/openid-connect/client-id-123", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNoContent)
				})
				mux.HandleFunc("DELETE /admin/realms/test-realm", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				})
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.desc, func(t *testing.T) {
			mux := http.NewServeMux()
			srv := httptest.NewServer(mux)
			defer srv.Close()

			configureOIDCProvider(t, mux, srv.URL)
			ctx := context.WithValue(context.Background(), oauth2.HTTPClient, srv.Client())

			orgsClient := mocks.NewMockClient(t)
			mgr := mocks.NewMockManager(t)

			if test.setupK8sMocks != nil {
				test.setupK8sMocks(orgsClient)
			}

			if test.setupKeycloakMocks != nil {
				test.setupKeycloakMocks(mux, srv.URL)
			}

			// Update RegistrationClientURI with the actual server URL
			if idpObj, ok := test.obj.(*pmcorev1alpha1.IdentityProviderConfiguration); ok {
				for clientName, managedClient := range idpObj.Status.ManagedClients {
					if managedClient.ClientID != "" {
						managedClient.RegistrationClientURI = fmt.Sprintf("%s/realms/%s/clients-registrations/openid-connect/%s", srv.URL, idpObj.Name, managedClient.ClientID)
						idpObj.Status.ManagedClients[clientName] = managedClient
					}
				}
			}

			cfg := getTestConfig(test.cfg, srv.URL)

			orgsCluster := mocks.NewMockCluster(t)
			mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName(string(config.MultiProviderName(config.CoreProviderName, config.OrgsClusterPath)))).Return(orgsCluster, nil).Maybe()
			orgsCluster.EXPECT().GetClient().Return(orgsClient).Maybe()

			s, err := idp.New(ctx, cfg, mgr, iclient.NewManagerKCPClientGetter(mgr, nil))
			assert.NoError(t, err)

			l := testlogger.New()
			ctx = l.WithContext(ctx)

			_, opErr := s.Finalize(ctx, test.obj)
			if test.expectErr {
				assert.NotNil(t, opErr, "expected an operator error")
			} else {
				assert.Nil(t, opErr, "did not expect an operator error")
			}
		})
	}
}

func TestHelperFunctions(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	configureOIDCProvider(t, mux, srv.URL)
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, srv.Client())

	orgsClient := fake.NewClientBuilder().WithScheme(getTestScheme()).Build()
	mgr, _, _ := setupManagerAndCluster(t, orgsClient)

	s, err := idp.New(ctx, &config.Config{
		Keycloak: config.KeycloakConfig{
			BaseURL:  srv.URL,
			ClientID: "security-operator",
		},
	}, mgr, iclient.NewManagerKCPClientGetter(mgr, nil))
	assert.NoError(t, err)

	assert.Equal(t, "IdentityProviderConfiguration", s.GetName())
	assert.Equal(t, []string{"core.platform-mesh.io/idp-finalizer"}, s.Finalizers(nil))

	res, finalizerErr := s.Finalize(ctx, &pmcorev1alpha1.IdentityProviderConfiguration{})
	assert.Nil(t, finalizerErr)
	assert.Equal(t, subroutines.OK(), res)
}
