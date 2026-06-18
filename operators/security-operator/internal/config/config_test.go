package config

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
)

func TestNewConfig(t *testing.T) {
	cfg := NewConfig()

	assert.Equal(t, "core_platform-mesh_io_account", cfg.FGA.ObjectType)
	assert.Equal(t, "/api-kubeconfig/kubeconfig", cfg.KCP.Kubeconfig)
	assert.Equal(t, "core.platform-mesh.io", cfg.APIExportEndpointSlices.CorePlatformMeshIO)
	assert.Equal(t, "system.platform-mesh.io", cfg.APIExportEndpointSlices.SystemPlatformMeshIO)
	assert.Equal(t, "security-operator", cfg.Keycloak.ClientID)
	assert.Equal(t, 9443, cfg.Webhooks.Port)
	assert.Equal(t, []string{"http://localhost:8000", "http://localhost:18000"}, cfg.IDP.KubectlClientRedirectURLs)
	assert.Nil(t, cfg.AdditionalAudiences)
}

func TestConfigAddFlags(t *testing.T) {
	cfg := NewConfig()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg.AddFlags(fs)

	err := fs.Parse([]string{
		"--fga-target=fga:8080",
		"--kcp-kubeconfig=/tmp/kubeconfig",
		"--idp-kubectl-client-redirect-urls=http://localhost:7000,http://localhost:17000",
		"--webhooks-enabled=true",
		"--webhooks-port=10443",
		"--additional-audiences=aud-a,aud-b",
	})

	assert.NoError(t, err)
	assert.Equal(t, "fga:8080", cfg.FGA.Target)
	assert.Equal(t, "/tmp/kubeconfig", cfg.KCP.Kubeconfig)
	assert.Equal(t, []string{"http://localhost:7000", "http://localhost:17000"}, cfg.IDP.KubectlClientRedirectURLs)
	assert.True(t, cfg.Webhooks.Enabled)
	assert.Equal(t, 10443, cfg.Webhooks.Port)
	assert.Equal(t, []string{"aud-a", "aud-b"}, cfg.AdditionalAudiences)
}

func TestInitContainerConfigAddFlags(t *testing.T) {
	cfg := NewInitContainerConfig()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg.AddFlags(fs)

	err := fs.Parse([]string{"--config-file=/tmp/config.yaml"})

	assert.NoError(t, err)
	assert.Equal(t, "/tmp/config.yaml", cfg.ConfigFile)
}
