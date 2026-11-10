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

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/pm-keycloak-operator/internal/features"

	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

func TestNewConfig(t *testing.T) {
	cfg := NewConfig()

	assert.Equal(t, "/api-kubeconfig/kubeconfig", cfg.KCP.Kubeconfig)
	assert.Equal(t, "keycloak.platform-mesh.io", cfg.APIExportEndpointSlices.KeycloakPlatformMeshIO)
	assert.Equal(t, "core.platform-mesh.io", cfg.APIExportEndpointSlices.CorePlatformMeshIO)
	assert.Equal(t, "system.platform-mesh.io", cfg.APIExportEndpointSlices.SystemPlatformMeshIO)
	assert.Equal(t, "pm-keycloak-operator", cfg.Keycloak.ClientID)
	assert.Equal(t, "root:keycloak", cfg.InitializerName())
	assert.Equal(t, 9443, cfg.Webhooks.Port)
	assert.Equal(t, []string{"http://localhost:8000", "http://localhost:18000"}, cfg.IDP.KubectlClientRedirectURLs)
	assert.Nil(t, cfg.AdditionalAudiences)
}

func TestConfigAddFlags(t *testing.T) {
	cfg := NewConfig()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg.AddFlags(fs)

	err := fs.Parse([]string{
		"--kcp-kubeconfig=/tmp/kubeconfig",
		"--keycloak-api-export-endpoint-slice-name=kc",
		"--idp-kubectl-client-redirect-urls=http://localhost:7000,http://localhost:17000",
		"--webhooks-enabled=true",
		"--webhooks-port=10443",
		"--additional-audiences=aud-a,aud-b",
	})

	require.NoError(t, err)
	assert.Equal(t, "/tmp/kubeconfig", cfg.KCP.Kubeconfig)
	assert.Equal(t, "kc", cfg.APIExportEndpointSlices.KeycloakPlatformMeshIO)
	assert.Equal(t, []string{"http://localhost:7000", "http://localhost:17000"}, cfg.IDP.KubectlClientRedirectURLs)
	assert.True(t, cfg.Webhooks.Enabled)
	assert.Equal(t, 10443, cfg.Webhooks.Port)
	assert.Equal(t, []string{"aud-a", "aud-b"}, cfg.AdditionalAudiences)
}

func TestConfig_FeatureEnabled(t *testing.T) {
	cases := map[string]struct {
		args   []string
		legacy bool
	}{
		"default on":  {nil, true},
		"legacy off":  {[]string{"--feature-gates=LegacyAPIs=false"}, false},
		"explicit on": {[]string{"--feature-gates=LegacyAPIs=true"}, true},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			cfg := NewConfig()
			fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
			cfg.AddFlags(fs)

			require.NoError(t, fs.Parse(cas.args))

			assert.Equal(t, cas.legacy, cfg.FeatureEnabled(features.LegacyAPIs))
		})
	}
}

func TestConfig_FeatureEnabled_ZeroValueUsesDefaults(t *testing.T) {
	assert.True(t, Config{}.FeatureEnabled(features.LegacyAPIs))
}

func TestConfigAddFlags_UnknownFeatureGate(t *testing.T) {
	cfg := NewConfig()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg.AddFlags(fs)

	require.Error(t, fs.Parse([]string{"--feature-gates=NoSuchGate=true"}))
}

func TestInitContainerConfigAddFlags(t *testing.T) {
	cfg := NewInitContainerConfig()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg.AddFlags(fs)

	err := fs.Parse([]string{"--config-file=/tmp/config.yaml"})

	assert.NoError(t, err)
	assert.Equal(t, "/tmp/config.yaml", cfg.ConfigFile)
}

func TestCoreClusterName(t *testing.T) {
	cases := map[string]struct {
		engaged  string
		expected string
	}{
		"keycloak provider":  {"keycloak#abc", "core#abc"},
		"core provider":      {"core#abc", "core#abc"},
		"system provider":    {"system#abc", "core#abc"},
		"no provider prefix": {"abc", "core#abc"},
		"path with colons":   {"keycloak#root:orgs", "core#root:orgs"},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			assert.Equal(t, cas.expected, string(CoreClusterName(multicluster.ClusterName(cas.engaged))))
		})
	}
}
