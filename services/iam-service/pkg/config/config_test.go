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

package config

import (
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestNewServiceConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg := NewServiceConfig()

	require.Equal(t, 8080, cfg.Port)
	require.Equal(t, "openfga:8081", cfg.OpenFGA.GRPCAddr)
	require.Equal(t, 5*time.Minute, cfg.OpenFGA.StoreCacheTTL)
	require.Equal(t, "sub", cfg.JWT.UserIDClaim)
	require.Equal(t, []string{"welcome"}, cfg.IDM.ExcludedTenants)
	require.Equal(t, "https://portal.dev.local:8443/keycloak", cfg.Keycloak.BaseURL)
	require.Equal(t, "iam", cfg.Keycloak.ClientID)
	require.Equal(t, "", cfg.Keycloak.ClientSecret)
	require.Equal(t, 100, cfg.Keycloak.PageSize)
	require.True(t, cfg.Keycloak.Cache.Enabled)
	require.Equal(t, time.Hour, cfg.Keycloak.Cache.TTL)
	require.Equal(t, 10, cfg.Pagination.DefaultLimit)
	require.Equal(t, 1, cfg.Pagination.DefaultPage)
	require.Equal(t, "LastName", cfg.Sorting.DefaultField)
	require.Equal(t, "ASC", cfg.Sorting.DefaultDirection)
	require.Equal(t, "input/roles.yaml", cfg.Roles.FilePath)
}

func TestAddFlagsParsesIntoServiceConfig(t *testing.T) {
	t.Parallel()

	cfg := NewServiceConfig()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg.AddFlags(fs)

	err := fs.Parse([]string{
		"--port=9090",
		"--openfga-grpc-addr=fga.example:9443",
		"--openfga-store-cache-ttl=30s",
		"--jwt-user-id-claim=user_id",
		"--excluded-tenants=welcome,tenant-a",
		"--keycloak-base-url=https://keycloak.example.local",
		"--keycloak-client-id=test-client",
		"--keycloak-page-size=200",
		"--keycloak-cache-enabled=false",
		"--keycloak-user-cache-ttl=90m",
		"--pagination-default-limit=50",
		"--pagination-default-page=3",
		"--sorting-default-field=FirstName",
		"--sorting-default-direction=DESC",
		"--roles-file-path=/tmp/roles.yaml",
	})
	require.NoError(t, err)

	require.Equal(t, 9090, cfg.Port)
	require.Equal(t, "fga.example:9443", cfg.OpenFGA.GRPCAddr)
	require.Equal(t, 30*time.Second, cfg.OpenFGA.StoreCacheTTL)
	require.Equal(t, "user_id", cfg.JWT.UserIDClaim)
	require.Equal(t, []string{"welcome", "tenant-a"}, cfg.IDM.ExcludedTenants)
	require.Equal(t, "https://keycloak.example.local", cfg.Keycloak.BaseURL)
	require.Equal(t, "test-client", cfg.Keycloak.ClientID)
	require.Equal(t, "", cfg.Keycloak.ClientSecret)
	require.Equal(t, 200, cfg.Keycloak.PageSize)
	require.False(t, cfg.Keycloak.Cache.Enabled)
	require.Equal(t, 90*time.Minute, cfg.Keycloak.Cache.TTL)
	require.Equal(t, 50, cfg.Pagination.DefaultLimit)
	require.Equal(t, 3, cfg.Pagination.DefaultPage)
	require.Equal(t, "FirstName", cfg.Sorting.DefaultField)
	require.Equal(t, "DESC", cfg.Sorting.DefaultDirection)
	require.Equal(t, "/tmp/roles.yaml", cfg.Roles.FilePath)
}

func TestNewServiceConfigReadsKeycloakClientSecretFromEnv(t *testing.T) {
	t.Setenv("KEYCLOAK_CLIENT_SECRET", "test-secret")

	cfg := NewServiceConfig()

	require.Equal(t, "test-secret", cfg.Keycloak.ClientSecret)
}
