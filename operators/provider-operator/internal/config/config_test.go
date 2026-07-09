package config

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
)

func TestNewOperatorConfig(t *testing.T) {
	cfg := NewOperatorConfig()

	assert.Equal(t, "/operator/", cfg.WorkspaceDir)
	assert.Equal(t, "platform-mesh-system", cfg.KCP.Namespace)
	assert.Equal(t, "root", cfg.KCP.RootShardName)
	assert.Equal(t, "frontproxy", cfg.KCP.FrontProxyName)
	assert.Equal(t, "8443", cfg.KCP.FrontProxyPort)
	assert.Equal(t, "kcp-cluster-admin-client-cert", cfg.KCP.ClusterAdminSecretName)

	assert.True(t, cfg.Subroutines.Deployment.Enabled)
	assert.Equal(t, "kcp-webhook-secret", cfg.Subroutines.Deployment.AuthorizationWebhookSecretName)
	assert.Equal(t, "rebac-authz-webhook-cert", cfg.Subroutines.Deployment.AuthorizationWebhookSecretCAName)
	assert.True(t, cfg.Subroutines.Deployment.EnableIstio)

	assert.True(t, cfg.Subroutines.KcpSetup.Enabled)
	assert.Equal(t, "domain-certificate", cfg.Subroutines.KcpSetup.DomainCertificateCASecretName)
	assert.Equal(t, "ca.crt", cfg.Subroutines.KcpSetup.DomainCertificateCASecretKey)

	assert.True(t, cfg.Subroutines.ProviderSecret.Enabled)
	assert.False(t, cfg.Subroutines.FeatureToggles.Enabled)
	assert.True(t, cfg.Subroutines.Wait.Enabled)

	assert.Equal(t, "providers.platform-mesh.io", cfg.Providers.ProvidersAPIExportEndpointSliceName)
	assert.Equal(t, "root:platform-mesh-system", cfg.Providers.ProvidersAPIExportEndpointSliceWorkspace)
	assert.True(t, cfg.Subroutines.Provider.Workspace.Enabled)
	assert.True(t, cfg.Subroutines.Provider.Kubeconfig.Enabled)
}

func TestOperatorConfigAddFlags(t *testing.T) {
	cfg := NewOperatorConfig()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg.AddFlags(fs)

	err := fs.Parse([]string{
		"--workspace-dir=/tmp/ws",
		"--kcp-url=https://kcp.example.local",
		"--kcp-namespace=custom-ns",
		"--kcp-root-shard-name=custom-root",
		"--kcp-front-proxy-name=custom-proxy",
		"--kcp-front-proxy-port=7443",
		"--kcp-cluster-admin-secret-name=custom-admin-secret",
		"--idp-registration-allowed=true",
		"--idp-welcome-additional-redirect-uris=https://extra.example.com/callback,https://other.example.com/callback",
		"--idp-welcome-additional-post-logout-redirect-uris=https://extra.example.com/logout",
		"--subroutines-deployment-enabled=false",
		"--authorization-webhook-secret-name=authz-secret",
		"--authorization-webhook-secret-ca-name=authz-ca",
		"--subroutines-deployment-enable-istio=false",
		"--subroutines-kcp-setup-enabled=false",
		"--domain-certificate-ca-secret-name=domain-ca",
		"--domain-certificate-ca-secret-key=ca.crt",
		"--subroutines-provider-secret-enabled=false",
		"--subroutines-feature-toggles-enabled=true",
		"--subroutines-wait-enabled=false",
	})

	assert.NoError(t, err)
	assert.Equal(t, "/tmp/ws", cfg.WorkspaceDir)
	assert.Equal(t, "https://kcp.example.local", cfg.KCP.Url)
	assert.Equal(t, "custom-ns", cfg.KCP.Namespace)
	assert.Equal(t, "custom-root", cfg.KCP.RootShardName)
	assert.Equal(t, "custom-proxy", cfg.KCP.FrontProxyName)
	assert.Equal(t, "7443", cfg.KCP.FrontProxyPort)
	assert.Equal(t, "custom-admin-secret", cfg.KCP.ClusterAdminSecretName)
	assert.True(t, cfg.IDP.RegistrationAllowed)
	assert.Equal(t, []string{"https://extra.example.com/callback", "https://other.example.com/callback"}, cfg.IDP.WelcomeAdditionalRedirectUris)
	assert.Equal(t, []string{"https://extra.example.com/logout"}, cfg.IDP.WelcomeAdditionalPostLogoutRedirectUris)

	assert.False(t, cfg.Subroutines.Deployment.Enabled)
	assert.Equal(t, "authz-secret", cfg.Subroutines.Deployment.AuthorizationWebhookSecretName)
	assert.Equal(t, "authz-ca", cfg.Subroutines.Deployment.AuthorizationWebhookSecretCAName)
	assert.False(t, cfg.Subroutines.Deployment.EnableIstio)

	assert.False(t, cfg.Subroutines.KcpSetup.Enabled)
	assert.Equal(t, "domain-ca", cfg.Subroutines.KcpSetup.DomainCertificateCASecretName)
	assert.Equal(t, "ca.crt", cfg.Subroutines.KcpSetup.DomainCertificateCASecretKey)

	assert.False(t, cfg.Subroutines.ProviderSecret.Enabled)
	assert.True(t, cfg.Subroutines.FeatureToggles.Enabled)
	assert.False(t, cfg.Subroutines.Wait.Enabled)
}

func TestOperatorConfigAddFlagsProviders(t *testing.T) {
	cfg := NewOperatorConfig()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg.AddFlags(fs)

	err := fs.Parse([]string{
		"--providers-apiexport-endpointslice-name=custom.providers.io",
		"--providers-apiexport-endpointslice-workspace=root:custom-ws",
	})

	assert.NoError(t, err)
	assert.Equal(t, "custom.providers.io", cfg.Providers.ProvidersAPIExportEndpointSliceName)
	assert.Equal(t, "root:custom-ws", cfg.Providers.ProvidersAPIExportEndpointSliceWorkspace)
}
