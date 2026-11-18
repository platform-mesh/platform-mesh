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
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
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

func TestOIDCConfig(t *testing.T) {
	t.Parallel()

	parse := func(t *testing.T, args ...string) OperatorConfig {
		t.Helper()
		cfg := NewOperatorConfig()
		fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
		cfg.AddFlags(fs)
		require.NoError(t, fs.Parse(args))
		return cfg
	}

	t.Run("no issuer leaves the OIDC of an AccountInfo to others", func(t *testing.T) {
		t.Parallel()
		cfg := parse(t)
		require.NoError(t, cfg.Validate())
		info, err := cfg.OIDC.Info()
		require.NoError(t, err)
		require.Nil(t, info)
	})

	t.Run("issuer, clients and scopes are published", func(t *testing.T) {
		t.Parallel()
		cfg := parse(t,
			"--oidc-issuer-url=https://issuer.example/dex",
			"--oidc-clients=kubectl=cli,portal=platform-mesh",
			"--oidc-scopes=email,groups",
		)
		require.NoError(t, cfg.Validate())
		info, err := cfg.OIDC.Info()
		require.NoError(t, err)
		require.Equal(t, &pmcorev1alpha1.OIDCInfo{
			IssuerURL: "https://issuer.example/dex",
			Clients: map[string]pmcorev1alpha1.ClientInfo{
				"kubectl": {ClientID: "cli"},
				"portal":  {ClientID: "platform-mesh"},
			},
			Scopes: []string{"email", "groups"},
		}, info)
	})

	t.Run("CA of the issuer is published", func(t *testing.T) {
		t.Parallel()
		const pem = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
		caFile := filepath.Join(t.TempDir(), "ca.crt")
		require.NoError(t, os.WriteFile(caFile, []byte(pem), 0o600))

		cfg := parse(t, "--oidc-issuer-url=https://issuer.example", "--oidc-clients=kubectl=cli", "--oidc-ca-file="+caFile)
		require.NoError(t, cfg.Validate())
		info, err := cfg.OIDC.Info()
		require.NoError(t, err)
		require.Equal(t, pem, info.CA)
	})

	t.Run("CA file that is missing or no certificate is an error", func(t *testing.T) {
		t.Parallel()
		notPEM := filepath.Join(t.TempDir(), "ca.crt")
		require.NoError(t, os.WriteFile(notPEM, []byte("not a certificate"), 0o600))

		for _, caFile := range []string{filepath.Join(t.TempDir(), "missing.crt"), notPEM} {
			cfg := parse(t, "--oidc-issuer-url=https://issuer.example", "--oidc-clients=kubectl=cli", "--oidc-ca-file="+caFile)
			_, err := cfg.OIDC.Info()
			require.Error(t, err)
		}
	})

	invalid := map[string][]string{
		"clients without an issuer": {"--oidc-clients=kubectl=cli"},
		"scopes without an issuer":  {"--oidc-scopes=email"},
		"CA without an issuer":      {"--oidc-ca-file=/ca.crt"},
		"issuer without a client":   {"--oidc-issuer-url=https://issuer.example"},
		"issuer that is not https":  {"--oidc-issuer-url=http://issuer.example", "--oidc-clients=kubectl=cli"},
		"client without an id":      {"--oidc-issuer-url=https://issuer.example", "--oidc-clients=kubectl="},
	}
	for title, args := range invalid {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			cfg := parse(t, args...)
			require.Error(t, cfg.Validate())
		})
	}
}
