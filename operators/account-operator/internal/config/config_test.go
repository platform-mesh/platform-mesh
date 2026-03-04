package config

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestNewOperatorConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg := NewOperatorConfig()

	require.False(t, cfg.Webhooks.Enabled)
	require.Equal(t, "certs", cfg.Webhooks.CertDir)
	require.Equal(t, 9443, cfg.Webhooks.Port)
	require.Empty(t, cfg.Webhooks.DenyList)
	require.Nil(t, cfg.Webhooks.AdditionalAccountTypes)

	require.True(t, cfg.Subroutines.WorkspaceType.Enabled)
	require.True(t, cfg.Subroutines.Workspace.Enabled)
	require.True(t, cfg.Subroutines.WorkspaceReady.Enabled)
	require.True(t, cfg.Subroutines.AccountInfo.Enabled)

	require.True(t, cfg.Controllers.AccountInfo.Enabled)

	require.Equal(t, "core.platform-mesh.io", cfg.Kcp.ApiExportEndpointSliceName)
	require.Equal(t, "root", cfg.Kcp.ProviderWorkspace)
}

func TestOperatorConfigAddFlagsParsesValues(t *testing.T) {
	t.Parallel()

	cfg := NewOperatorConfig()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg.AddFlags(fs)

	err := fs.Parse([]string{
		"--webhooks-enabled=true",
		"--webhooks-cert-dir=/tmp/certs",
		"--webhooks-port=10443",
		"--webhooks-deny-list=foo,bar",
		"--webhooks-additional-account-types=trial,paid",
		"--subroutines-workspace-type-enabled=false",
		"--subroutines-workspace-enabled=false",
		"--subroutines-workspace-ready-enabled=false",
		"--subroutines-account-info-enabled=false",
		"--controllers-account-info-enabled=false",
		"--kcp-api-export-endpoint-slice-name=custom.endpoint.slice",
		"--kcp-provider-workspace=root:orgs",
	})
	require.NoError(t, err)

	require.True(t, cfg.Webhooks.Enabled)
	require.Equal(t, "/tmp/certs", cfg.Webhooks.CertDir)
	require.Equal(t, 10443, cfg.Webhooks.Port)
	require.Equal(t, "foo,bar", cfg.Webhooks.DenyList)
	require.Equal(t, []string{"trial", "paid"}, cfg.Webhooks.AdditionalAccountTypes)

	require.False(t, cfg.Subroutines.WorkspaceType.Enabled)
	require.False(t, cfg.Subroutines.Workspace.Enabled)
	require.False(t, cfg.Subroutines.WorkspaceReady.Enabled)
	require.False(t, cfg.Subroutines.AccountInfo.Enabled)

	require.False(t, cfg.Controllers.AccountInfo.Enabled)

	require.Equal(t, "custom.endpoint.slice", cfg.Kcp.ApiExportEndpointSliceName)
	require.Equal(t, "root:orgs", cfg.Kcp.ProviderWorkspace)
}
