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

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/iam-service/pkg/config"
	fgamocks "go.platform-mesh.io/iam-service/pkg/fga/mocks"
	"go.platform-mesh.io/iam-service/pkg/idm"
	"go.platform-mesh.io/iam-service/pkg/idm/none"
	"go.platform-mesh.io/iam-service/pkg/keycloak"
	keycloakmw "go.platform-mesh.io/iam-service/pkg/middleware/keycloak"

	"k8s.io/client-go/rest"
)

const workspacePrefix = "/clusters/root:platform-mesh-system"

// fakeKCP serves just enough of the kcp API for the APIExport provider to
// start: discovery and an empty list and watch of APIExportEndpointSlices.
type fakeKCP struct {
	*httptest.Server

	mu    sync.Mutex
	paths []string
}

func newFakeKCP(t *testing.T) *fakeKCP {
	t.Helper()

	f := &fakeKCP{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.mu.Unlock()

		writeJSON := func(v any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(v)
		}

		switch strings.TrimPrefix(r.URL.Path, workspacePrefix) {
		case "/api":
			writeJSON(map[string]any{"kind": "APIVersions", "versions": []string{"v1"}})
		case "/api/v1":
			writeJSON(map[string]any{"kind": "APIResourceList", "groupVersion": "v1", "resources": []any{}})
		case "/apis":
			version := map[string]any{"groupVersion": "apis.kcp.io/v1alpha1", "version": "v1alpha1"}
			writeJSON(map[string]any{"kind": "APIGroupList", "apiVersion": "v1", "groups": []any{
				map[string]any{"name": "apis.kcp.io", "versions": []any{version}, "preferredVersion": version},
			}})
		case "/apis/apis.kcp.io/v1alpha1":
			writeJSON(map[string]any{"kind": "APIResourceList", "apiVersion": "v1", "groupVersion": "apis.kcp.io/v1alpha1", "resources": []any{
				map[string]any{"name": "apiexportendpointslices", "singularName": "apiexportendpointslice", "namespaced": false, "kind": "APIExportEndpointSlice", "verbs": []string{"get", "list", "watch"}},
			}})
		case "/apis/apis.kcp.io/v1alpha1/apiexportendpointslices":
			if r.URL.Query().Get("watch") != "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				return
			}
			writeJSON(map[string]any{"kind": "APIExportEndpointSliceList", "apiVersion": "apis.kcp.io/v1alpha1", "metadata": map[string]any{"resourceVersion": "1"}, "items": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		f.CloseClientConnections()
		f.Close()
	})

	return f
}

func (f *fakeKCP) requested(path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, p := range f.paths {
		if p == path {
			return true
		}
	}
	return false
}

func freeAddr(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close() //nolint:errcheck

	return l.Addr().String()
}

func testConfig(addr string) Config {
	common := pmconfig.NewDefaultConfig()
	common.Metrics.BindAddress = "0"
	common.HealthProbeBindAddress = "0"

	service := config.NewServiceConfig()
	service.ListenAddress = addr
	service.IDM.Provider = idm.ProviderNone
	service.Roles.FilePath = ""
	service.CORS.AllowedOrigins = []string{"http://*.localhost:4300"}

	return Config{Common: common, Service: service}
}

func testLogger(t *testing.T) *logger.Logger {
	t.Helper()

	log, err := logger.New(logger.DefaultConfig())
	require.NoError(t, err)
	return log
}

// TestRun_WithoutIdentityProvider runs the service the way an embedding process
// does: no Keycloak, an injected OpenFGA client, the built-in roles, metrics and
// health probes disabled, bound to the loopback interface.
func TestRun_WithoutIdentityProvider(t *testing.T) {
	kcp := newFakeKCP(t)
	addr := freeAddr(t)
	hostBefore := kcp.URL + workspacePrefix
	kcpCfg := &rest.Config{Host: hostBefore}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, kcpCfg, testConfig(addr), Deps{
			Logger:  testLogger(t),
			OpenFGA: fgamocks.NewOpenFGAServiceClient(t),
		})
	}()

	client := &http.Client{Timeout: 5 * time.Second}
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		select {
		case err := <-done:
			done <- err
			assert.Fail(c, "Run returned early", "%v", err)
			return
		default:
		}
		res, err := client.Get(fmt.Sprintf("http://%s/readyz", addr))
		if !assert.NoError(c, err) {
			return
		}
		defer res.Body.Close() //nolint:errcheck
		assert.Equal(c, http.StatusOK, res.StatusCode)
	}, 20*time.Second, 100*time.Millisecond, "the service must come up")

	// A preflight request of the portal is answered without credentials.
	req, err := http.NewRequest(http.MethodOptions, fmt.Sprintf("http://%s/graphql", addr), http.NoBody)
	require.NoError(t, err)
	req.Header.Set("Origin", "http://acme.localhost:4300")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	res, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	assert.Equal(t, http.StatusNoContent, res.StatusCode)
	assert.Equal(t, "http://acme.localhost:4300", res.Header.Get("Access-Control-Allow-Origin"))

	// A request without a token is rejected.
	res, err = client.Post(fmt.Sprintf("http://%s/graphql", addr), "application/json", strings.NewReader(`{"query":"{ me { email } }"}`))
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, kcp.requested(workspacePrefix+"/apis/apis.kcp.io/v1alpha1/apiexportendpointslices"))
	}, 20*time.Second, 100*time.Millisecond, "the APIExportEndpointSlice must be watched in the workspace of the kcp config")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err, "Run must return nil when its context is done")
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not return after its context was done")
	}

	assert.Equal(t, hostBefore, kcpCfg.Host, "the caller's kcp config must not be modified")
	assert.Nil(t, kcpCfg.WrapTransport, "the caller's kcp config must not be modified")
}

func TestRun_ReturnsErrors(t *testing.T) {
	kcpCfg := &rest.Config{Host: newFakeKCP(t).URL + workspacePrefix}

	t.Run("kcp not reachable", func(t *testing.T) {
		unreachable := &rest.Config{Host: "http://127.0.0.1:1" + workspacePrefix}

		err := Run(t.Context(), unreachable, testConfig("127.0.0.1:0"), Deps{Logger: testLogger(t), OpenFGA: fgamocks.NewOpenFGAServiceClient(t)})
		require.ErrorContains(t, err, "unable to construct APIExport provider")
	})

	t.Run("missing arguments", func(t *testing.T) {
		require.Error(t, Run(t.Context(), nil, testConfig("127.0.0.1:0"), Deps{Logger: testLogger(t)}))
		require.Error(t, Run(t.Context(), kcpCfg, Config{}, Deps{Logger: testLogger(t)}))
		require.Error(t, Run(t.Context(), kcpCfg, testConfig("127.0.0.1:0"), Deps{}))
	})

	t.Run("unknown idm provider", func(t *testing.T) {
		cfg := testConfig("127.0.0.1:0")
		cfg.Service.IDM.Provider = "ldap"

		err := Run(t.Context(), kcpCfg, cfg, Deps{Logger: testLogger(t), OpenFGA: fgamocks.NewOpenFGAServiceClient(t)})
		require.ErrorContains(t, err, `unknown idm provider "ldap"`)
	})

	t.Run("missing roles file", func(t *testing.T) {
		cfg := testConfig("127.0.0.1:0")
		cfg.Service.Roles.FilePath = "does/not/exist.yaml"

		err := Run(t.Context(), kcpCfg, cfg, Deps{Logger: testLogger(t), OpenFGA: fgamocks.NewOpenFGAServiceClient(t)})
		require.ErrorContains(t, err, "does/not/exist.yaml")
	})

	t.Run("address in use", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		defer l.Close() //nolint:errcheck

		err = Run(t.Context(), kcpCfg, testConfig(l.Addr().String()), Deps{Logger: testLogger(t), OpenFGA: fgamocks.NewOpenFGAServiceClient(t)})
		require.ErrorContains(t, err, "failed to listen")
	})
}

func TestNewIDM(t *testing.T) {
	log := testLogger(t)
	fga := fgamocks.NewOpenFGAServiceClient(t)

	t.Run("none", func(t *testing.T) {
		cfg := config.NewServiceConfig()
		cfg.IDM.Provider = idm.ProviderNone

		directory, tenants, err := newIDM(t.Context(), cfg, fga, nil, log)
		require.NoError(t, err)
		assert.IsType(t, &none.Directory{}, directory)
		assert.IsType(t, none.TenantRetriever{}, tenants)
	})

	t.Run("keycloak is the default and has to be reachable", func(t *testing.T) {
		cfg := config.NewServiceConfig()
		cfg.Keycloak.BaseURL = "http://127.0.0.1:1/keycloak"
		require.Equal(t, idm.ProviderKeycloak, cfg.IDM.Provider)

		_, _, err := newIDM(t.Context(), cfg, fga, nil, log)
		require.ErrorContains(t, err, "failed to create keycloak client")
	})

	t.Run("an injected directory keeps the provider's tenant retriever", func(t *testing.T) {
		cfg := config.NewServiceConfig()
		injected := &keycloak.Service{}

		directory, tenants, err := newIDM(t.Context(), cfg, fga, injected, log)
		require.NoError(t, err)
		assert.Same(t, injected, directory)
		assert.IsType(t, keycloakmw.New(), tenants)
	})
}
