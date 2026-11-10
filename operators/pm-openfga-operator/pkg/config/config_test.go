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

	"go.platform-mesh.io/pm-openfga-operator/internal/features"
)

func TestNewConfig(t *testing.T) {
	cfg := NewConfig()

	assert.Equal(t, "core_platform-mesh_io_account", cfg.FGA.ObjectType)
	assert.Equal(t, "/api-kubeconfig/kubeconfig", cfg.KCP.Kubeconfig)
	assert.Equal(t, "core.platform-mesh.io", cfg.APIExportEndpointSlices.CorePlatformMeshIO)
	assert.Equal(t, "system.platform-mesh.io", cfg.APIExportEndpointSlices.SystemPlatformMeshIO)
	assert.Equal(t, "openfga.platform-mesh.io", cfg.APIExportEndpointSlices.OpenfgaPlatformMeshIO)
	assert.Equal(t, "root:openfga", cfg.InitializerName())
}

func TestConfigAddFlags(t *testing.T) {
	cfg := NewConfig()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg.AddFlags(fs)

	err := fs.Parse([]string{
		"--fga-target=fga:8080",
		"--kcp-kubeconfig=/tmp/kubeconfig",
		"--rekey-orphaned-tuples-enabled=true",
	})

	assert.NoError(t, err)
	assert.Equal(t, "fga:8080", cfg.FGA.Target)
	assert.Equal(t, "/tmp/kubeconfig", cfg.KCP.Kubeconfig)
	assert.True(t, cfg.RekeyOrphanedTuplesEnabled)
}

func TestConfig_FeatureEnabled(t *testing.T) {
	cases := map[string]struct {
		args   []string
		legacy bool
	}{
		"default on":    {nil, true},
		"legacy off":    {[]string{"--feature-gates=LegacyAPIs=false"}, false},
		"explicitly on": {[]string{"--feature-gates=LegacyAPIs=true"}, true},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			cfg := NewConfig()
			fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
			cfg.AddFlags(fs)

			require.NoError(t, fs.Parse(cas.args))

			assert.Equal(t, cas.legacy, cfg.FeatureEnabled(features.LegacyAPIs), "LegacyAPIs gate")
		})
	}
}

func TestConfig_FeatureEnabled_ZeroValueUsesDefaults(t *testing.T) {
	cfg := Config{}

	assert.True(t, cfg.FeatureEnabled(features.LegacyAPIs))
}

func TestConfigAddFlags_UnknownFeatureGate(t *testing.T) {
	cfg := NewConfig()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg.AddFlags(fs)

	err := fs.Parse([]string{"--feature-gates=NoSuchGate=true"})

	require.Error(t, err)
}

func TestConfigRekeyOrphanedTuplesDefaultsToDisabled(t *testing.T) {
	cfg := NewConfig()
	assert.False(t, cfg.RekeyOrphanedTuplesEnabled)
}
