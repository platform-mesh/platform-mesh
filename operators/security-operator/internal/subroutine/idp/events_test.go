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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger/testlogger"
	iclient "go.platform-mesh.io/security-operator/internal/client"
	"go.platform-mesh.io/security-operator/internal/subroutine/idp"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"
	"go.platform-mesh.io/security-operator/pkg/config"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const readerClientID = "contrib-eventing-keycloak-reader"

var defaultEvents = config.IDPEventsConfig{Enabled: true, Types: []string{"LOGIN"}, Expiration: 168 * time.Hour}

// runEventsProcess reconciles an IPC without clients, so only the realm and
// the events part talk to Keycloak.
func runEventsProcess(t *testing.T, realm string, events config.IDPEventsConfig, register func(mux *http.ServeMux)) error {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	configureOIDCProvider(t, mux, srv.URL)
	mux.HandleFunc("POST /admin/realms", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	register(mux)

	ipc := &pmcorev1alpha1.IdentityProviderConfiguration{ObjectMeta: metav1.ObjectMeta{Name: realm}}
	mgr, _, _ := setupManagerAndCluster(t, mocks.NewMockClient(t), ipc.DeepCopy())

	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, srv.Client())
	cfg := getTestConfig(&config.Config{IDP: config.IDPConfig{Events: events}}, srv.URL)
	s, err := idp.New(ctx, cfg, mgr, iclient.NewManagerKCPClientGetter(mgr, nil))
	require.NoError(t, err)

	_, opErr := s.Process(testlogger.New().WithContext(ctx), ipc)
	return opErr
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func failOnCall(t *testing.T, mux *http.ServeMux, patterns ...string) {
	for _, p := range patterns {
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("unexpected call %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusInternalServerError)
		})
	}
}

func TestEventStore(t *testing.T) {
	tests := []struct {
		name      string
		realm     string
		events    config.IDPEventsConfig
		current   string
		putStatus int
		wantPut   map[string]any
		wantErr   string
	}{
		{
			name:   "disabled leaves the realm untouched",
			realm:  "acme",
			events: config.IDPEventsConfig{Types: []string{"LOGIN"}, Expiration: time.Hour},
		},
		{
			name:   "welcome is skipped",
			realm:  "welcome",
			events: defaultEvents,
		},
		{
			name:    "store off is enabled with the configured types",
			realm:   "acme",
			events:  defaultEvents,
			current: `{"eventsEnabled":false,"enabledEventTypes":["LOGIN","LOGOUT","CODE_TO_TOKEN"]}`,
			wantPut: map[string]any{"eventsEnabled": true, "eventsExpiration": float64(604800), "enabledEventTypes": []any{"LOGIN"}},
		},
		{
			name:    "missing types are added to the enabled ones",
			realm:   "acme",
			events:  config.IDPEventsConfig{Enabled: true, Types: []string{"LOGIN", "REGISTER"}, Expiration: 168 * time.Hour},
			current: `{"eventsEnabled":true,"eventsExpiration":604800,"enabledEventTypes":["LOGOUT","LOGIN"]}`,
			wantPut: map[string]any{"eventsEnabled": true, "eventsExpiration": float64(604800), "enabledEventTypes": []any{"LOGOUT", "LOGIN", "REGISTER"}},
		},
		{
			name:    "shorter retention is raised and the event types kept",
			realm:   "acme",
			events:  defaultEvents,
			current: `{"eventsEnabled":true,"eventsExpiration":3600,"enabledEventTypes":["LOGIN","LOGOUT"]}`,
			wantPut: map[string]any{"eventsEnabled": true, "eventsExpiration": float64(604800)},
		},
		{
			name:    "longer retention is kept",
			realm:   "acme",
			events:  defaultEvents,
			current: `{"eventsEnabled":true,"eventsExpiration":7776000,"enabledEventTypes":["LOGIN"]}`,
		},
		{
			name:    "no expiration is kept",
			realm:   "acme",
			events:  defaultEvents,
			current: `{"eventsEnabled":true,"enabledEventTypes":["LOGIN"]}`,
		},
		{
			name:    "longer retention is sent with added types",
			realm:   "acme",
			events:  defaultEvents,
			current: `{"eventsEnabled":true,"eventsExpiration":7776000,"enabledEventTypes":["LOGOUT"]}`,
			wantPut: map[string]any{"eventsEnabled": true, "eventsExpiration": float64(7776000), "enabledEventTypes": []any{"LOGOUT", "LOGIN"}},
		},
		{
			name:    "no expiration is sent with added types",
			realm:   "acme",
			events:  defaultEvents,
			current: `{"eventsEnabled":true,"enabledEventTypes":["LOGOUT"]}`,
			wantPut: map[string]any{"eventsEnabled": true, "eventsExpiration": float64(0), "enabledEventTypes": []any{"LOGOUT", "LOGIN"}},
		},
		{
			name:    "up to date store is not written",
			realm:   "acme",
			events:  defaultEvents,
			current: `{"eventsEnabled":true,"eventsExpiration":604800,"enabledEventTypes":["LOGOUT","LOGIN"]}`,
		},
		{
			name:    "read error is returned",
			realm:   "acme",
			events:  defaultEvents,
			wantErr: `failed to get events config of realm "acme"`,
		},
		{
			name:      "write error is returned",
			realm:     "acme",
			events:    defaultEvents,
			current:   `{"eventsEnabled":false}`,
			putStatus: http.StatusInternalServerError,
			wantErr:   `failed to update events config of realm "acme"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPut map[string]any
			err := runEventsProcess(t, tt.realm, tt.events, func(mux *http.ServeMux) {
				if tt.current == "" && tt.wantErr == "" {
					failOnCall(t, mux, "/admin/realms/"+tt.realm+"/events/config")
					return
				}
				mux.HandleFunc("GET /admin/realms/acme/events/config", func(w http.ResponseWriter, r *http.Request) {
					if tt.current == "" {
						w.WriteHeader(http.StatusForbidden)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, tt.current) //nolint:errcheck
				})
				mux.HandleFunc("PUT /admin/realms/acme/events/config", func(w http.ResponseWriter, r *http.Request) {
					assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotPut))
					if tt.putStatus != 0 {
						w.WriteHeader(tt.putStatus)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				})
			})
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantPut, gotPut)
		})
	}
}

type readerKeycloak struct {
	readerMissing     bool
	managementMissing bool
	roleMissing       bool
	assignFails       bool
	granted           []map[string]string
	assigned          []map[string]string
	assignCalls       int
}

func (k *readerKeycloak) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/realms/master/clients", func(w http.ResponseWriter, r *http.Request) {
		switch clientID := r.URL.Query().Get("clientId"); {
		case clientID == readerClientID && !k.readerMissing:
			writeJSON(w, []map[string]string{{"id": "reader-uuid", "clientId": clientID}})
		case clientID == "acme-realm" && !k.managementMissing:
			writeJSON(w, []map[string]string{{"id": "acme-realm-uuid", "clientId": clientID}})
		default:
			writeJSON(w, []map[string]string{})
		}
	})
	mux.HandleFunc("GET /admin/realms/master/clients/reader-uuid/service-account-user", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"id": "reader-sa", "username": "service-account-" + readerClientID})
	})
	mux.HandleFunc("GET /admin/realms/master/users/reader-sa/role-mappings/clients/acme-realm-uuid", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, k.granted)
	})
	mux.HandleFunc("GET /admin/realms/master/clients/acme-realm-uuid/roles/{role}", func(w http.ResponseWriter, r *http.Request) {
		if k.roleMissing {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]string{"id": r.PathValue("role") + "-id", "name": r.PathValue("role")})
	})
	mux.HandleFunc("POST /admin/realms/master/users/reader-sa/role-mappings/clients/acme-realm-uuid", func(w http.ResponseWriter, r *http.Request) {
		k.assignCalls++
		_ = json.NewDecoder(r.Body).Decode(&k.assigned)
		if k.assignFails {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func TestEventReaderRoles(t *testing.T) {
	readerOnly := config.IDPEventsConfig{ReaderClient: readerClientID}
	tests := []struct {
		name         string
		realm        string
		kc           *readerKeycloak
		wantAssigned []map[string]string
		wantErr      string
	}{
		{
			name:         "missing roles are granted",
			realm:        "acme",
			kc:           &readerKeycloak{},
			wantAssigned: []map[string]string{{"id": "view-events-id", "name": "view-events"}, {"id": "view-users-id", "name": "view-users"}},
		},
		{
			name:         "only the missing role is granted",
			realm:        "acme",
			kc:           &readerKeycloak{granted: []map[string]string{{"id": "view-users-id", "name": "view-users"}}},
			wantAssigned: []map[string]string{{"id": "view-events-id", "name": "view-events"}},
		},
		{
			name:  "already granted roles are not written",
			realm: "acme",
			kc: &readerKeycloak{granted: []map[string]string{
				{"id": "view-events-id", "name": "view-events"},
				{"id": "view-users-id", "name": "view-users"},
			}},
		},
		{
			name:    "missing reader client is an error",
			realm:   "acme",
			kc:      &readerKeycloak{readerMissing: true},
			wantErr: `event reader client "contrib-eventing-keycloak-reader" not found in realm master`,
		},
		{
			name:    "missing management client is an error",
			realm:   "acme",
			kc:      &readerKeycloak{managementMissing: true},
			wantErr: `management client "acme-realm" not found in realm master`,
		},
		{
			name:    "missing role is an error",
			realm:   "acme",
			kc:      &readerKeycloak{roleMissing: true},
			wantErr: `role "view-events" not found on client "acme-realm"`,
		},
		{
			name:    "failing grant is an error",
			realm:   "acme",
			kc:      &readerKeycloak{assignFails: true},
			wantErr: `failed to grant event reader client "contrib-eventing-keycloak-reader" roles on realm "acme"`,
		},
		{
			name:  "welcome is skipped",
			realm: "welcome",
			kc:    &readerKeycloak{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runEventsProcess(t, tt.realm, readerOnly, func(mux *http.ServeMux) {
				if tt.realm == "welcome" {
					failOnCall(t, mux, "/admin/realms/master/")
					return
				}
				tt.kc.register(mux)
			})
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				if !tt.kc.assignFails {
					assert.Zero(t, tt.kc.assignCalls)
				}
				return
			}
			assert.NoError(t, err)
			if tt.wantAssigned == nil {
				assert.Zero(t, tt.kc.assignCalls)
				return
			}
			assert.Equal(t, 1, tt.kc.assignCalls)
			assert.Equal(t, tt.wantAssigned, tt.kc.assigned)
		})
	}
}

func TestEventStoreAndReaderRoles(t *testing.T) {
	events := defaultEvents
	events.ReaderClient = readerClientID
	kc := &readerKeycloak{}
	var gotPut map[string]any

	err := runEventsProcess(t, "acme", events, func(mux *http.ServeMux) {
		kc.register(mux)
		mux.HandleFunc("GET /admin/realms/acme/events/config", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{"eventsEnabled": false})
		})
		mux.HandleFunc("PUT /admin/realms/acme/events/config", func(w http.ResponseWriter, r *http.Request) {
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotPut))
			w.WriteHeader(http.StatusNoContent)
		})
	})

	assert.NoError(t, err)
	assert.Equal(t, map[string]any{"eventsEnabled": true, "eventsExpiration": float64(604800), "enabledEventTypes": []any{"LOGIN"}}, gotPut)
	assert.Equal(t, 1, kc.assignCalls)
	assert.Len(t, kc.assigned, 2)
}

func TestNewRejectsInvalidEventsConfig(t *testing.T) {
	cfg := &config.Config{IDP: config.IDPConfig{Events: config.IDPEventsConfig{Enabled: true, Expiration: time.Hour}}}
	_, err := idp.New(context.Background(), cfg, nil, nil)
	assert.ErrorContains(t, err, "--idp-events-types must not be empty")
}
