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
	"testing"

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

func testConfig(addr string) Config {
	common := pmconfig.NewDefaultConfig()
	common.Metrics.BindAddress = "0"
	common.HealthProbeBindAddress = "0"

	service := config.NewServiceConfig()
	service.ListenAddress = addr
	service.IDM.Provider = idm.ProviderNone
	service.Roles.FilePath = ""

	return Config{Common: common, Service: service}
}

func testLogger(t *testing.T) *logger.Logger {
	t.Helper()

	log, err := logger.New(logger.DefaultConfig())
	require.NoError(t, err)
	return log
}

func TestRun_ReturnsErrors(t *testing.T) {
	kcpCfg := &rest.Config{Host: "http://127.0.0.1:1" + workspacePrefix}

	t.Run("kcp not reachable", func(t *testing.T) {
		err := Run(t.Context(), kcpCfg, testConfig("127.0.0.1:0"), Deps{Logger: testLogger(t), OpenFGA: fgamocks.NewOpenFGAServiceClient(t)})
		require.ErrorContains(t, err, "unable to construct APIExport provider")
	})

	t.Run("missing arguments", func(t *testing.T) {
		require.Error(t, Run(t.Context(), nil, testConfig("127.0.0.1:0"), Deps{Logger: testLogger(t)}))
		require.Error(t, Run(t.Context(), kcpCfg, Config{}, Deps{Logger: testLogger(t)}))
		require.Error(t, Run(t.Context(), kcpCfg, testConfig("127.0.0.1:0"), Deps{}))
	})
}

func TestNewRolesRetriever(t *testing.T) {
	log := testLogger(t)

	t.Run("built-in default roles", func(t *testing.T) {
		cfg := config.NewServiceConfig()
		cfg.Roles.FilePath = ""

		retriever, run, err := newRolesRetriever(&rest.Config{}, NewScheme(), cfg, log)
		require.NoError(t, err)
		assert.NotNil(t, retriever)
		assert.Nil(t, run)
	})

	t.Run("missing roles file", func(t *testing.T) {
		cfg := config.NewServiceConfig()
		cfg.Roles.FilePath = "does/not/exist.yaml"

		_, _, err := newRolesRetriever(&rest.Config{}, NewScheme(), cfg, log)
		require.ErrorContains(t, err, "does/not/exist.yaml")
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

	t.Run("unknown idm provider", func(t *testing.T) {
		cfg := config.NewServiceConfig()
		cfg.IDM.Provider = "ldap"

		_, _, err := newIDM(t.Context(), cfg, fga, nil, log)
		require.ErrorContains(t, err, `unknown idm provider "ldap"`)
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
