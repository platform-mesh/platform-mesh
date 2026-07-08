package config

import "github.com/spf13/pflag"

type KCPConfig struct {
	Url                    string
	Namespace              string
	RootShardName          string
	FrontProxyName         string
	FrontProxyPort         string
	ClusterAdminSecretName string
}

type IDPConfig struct {
	RegistrationAllowed                     bool
	WelcomeAdditionalRedirectUris           []string
	WelcomeAdditionalPostLogoutRedirectUris []string
}

type DeploymentSubroutineConfig struct {
	Enabled                          bool
	AuthorizationWebhookSecretName   string
	AuthorizationWebhookSecretCAName string
	EnableIstio                      bool
}

type KcpSetupSubroutineConfig struct {
	Enabled                       bool
	DomainCertificateCASecretName string
	DomainCertificateCASecretKey  string
}

type ProviderSecretSubroutineConfig struct {
	Enabled bool
}

type FeatureTogglesSubroutineConfig struct {
	Enabled bool
}

type WaitSubroutineConfig struct {
	Enabled bool
}

type RemoteClusterConfig struct {
	Kubeconfig      string
	InfraSecretName string
	InfraSecretKey  string
}

func (r *RemoteClusterConfig) IsEnabled() bool {
	return r.Kubeconfig != ""
}

type ManagedProviderSubroutineConfig struct {
	Enabled bool
}

type ProviderSubroutineConfig struct {
	Enabled bool
}

type ManagedProviderSubroutinesConfig struct {
	ProviderResource ManagedProviderSubroutineConfig
	WaitProvider     ManagedProviderSubroutineConfig
	KubeconfigCopy   ManagedProviderSubroutineConfig
	Deploy           ManagedProviderSubroutineConfig
}

type SubroutinesConfig struct {
	Deployment      DeploymentSubroutineConfig
	KcpSetup        KcpSetupSubroutineConfig
	ProviderSecret  ProviderSecretSubroutineConfig
	FeatureToggles  FeatureTogglesSubroutineConfig
	Wait            WaitSubroutineConfig
	ManagedProvider ManagedProviderSubroutinesConfig
	Provider        ProviderSubroutinesConfig
}

// OperatorConfig struct to hold the app config
type OperatorConfig struct {
	WorkspaceDir  string
	KCP           KCPConfig
	IDP           IDPConfig
	Subroutines   SubroutinesConfig
	RemoteRuntime RemoteClusterConfig
	RemoteInfra   RemoteClusterConfig
	Providers     ProvidersConfig
}

func NewOperatorConfig() OperatorConfig {
	return OperatorConfig{
		WorkspaceDir: "/operator/",
		KCP: KCPConfig{
			Namespace:              "platform-mesh-system",
			RootShardName:          "root",
			FrontProxyName:         "frontproxy",
			FrontProxyPort:         "8443",
			ClusterAdminSecretName: "kcp-cluster-admin-client-cert",
		},
		Providers: NewProvidersConfig(),
		Subroutines: SubroutinesConfig{
			Deployment: DeploymentSubroutineConfig{
				Enabled:                          true,
				AuthorizationWebhookSecretName:   "kcp-webhook-secret",
				AuthorizationWebhookSecretCAName: "rebac-authz-webhook-cert",
				EnableIstio:                      true,
			},
			KcpSetup: KcpSetupSubroutineConfig{
				Enabled:                       true,
				DomainCertificateCASecretName: "domain-certificate",
				DomainCertificateCASecretKey:  "ca.crt",
			},
			ProviderSecret: ProviderSecretSubroutineConfig{
				Enabled: true,
			},
			FeatureToggles: FeatureTogglesSubroutineConfig{
				Enabled: false,
			},
			Wait: WaitSubroutineConfig{
				Enabled: true,
			},
			ManagedProvider: ManagedProviderSubroutinesConfig{
				ProviderResource: ManagedProviderSubroutineConfig{Enabled: true},
				WaitProvider:     ManagedProviderSubroutineConfig{Enabled: true},
				KubeconfigCopy:   ManagedProviderSubroutineConfig{Enabled: true},
				Deploy:           ManagedProviderSubroutineConfig{Enabled: true},
			},
			Provider: ProviderSubroutinesConfig{
				Workspace:  ProviderSubroutineConfig{Enabled: true},
				Kubeconfig: ProviderSubroutineConfig{Enabled: true},
			},
		},
	}
}

func (c *OperatorConfig) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.WorkspaceDir, "workspace-dir", c.WorkspaceDir, "Set workspace directory")

	fs.StringVar(&c.KCP.Url, "kcp-url", c.KCP.Url, "Set KCP URL")
	fs.StringVar(&c.KCP.Namespace, "kcp-namespace", c.KCP.Namespace, "Set KCP namespace")
	fs.StringVar(&c.KCP.RootShardName, "kcp-root-shard-name", c.KCP.RootShardName, "Set KCP root shard name")
	fs.StringVar(&c.KCP.FrontProxyName, "kcp-front-proxy-name", c.KCP.FrontProxyName, "Set KCP front-proxy name")
	fs.StringVar(&c.KCP.FrontProxyPort, "kcp-front-proxy-port", c.KCP.FrontProxyPort, "Set KCP front-proxy port")
	fs.StringVar(&c.KCP.ClusterAdminSecretName, "kcp-cluster-admin-secret-name", c.KCP.ClusterAdminSecretName, "Set cluster-admin secret name")

	fs.BoolVar(&c.IDP.RegistrationAllowed, "idp-registration-allowed", c.IDP.RegistrationAllowed, "Allow IDP registration")
	fs.StringSliceVar(&c.IDP.WelcomeAdditionalRedirectUris, "idp-welcome-additional-redirect-uris", c.IDP.WelcomeAdditionalRedirectUris, "Additional redirect URIs for the welcome client (comma-separated)")
	fs.StringSliceVar(&c.IDP.WelcomeAdditionalPostLogoutRedirectUris, "idp-welcome-additional-post-logout-redirect-uris", c.IDP.WelcomeAdditionalPostLogoutRedirectUris, "Additional post-logout redirect URIs for the welcome client (comma-separated)")

	fs.BoolVar(&c.Subroutines.Deployment.Enabled, "subroutines-deployment-enabled", c.Subroutines.Deployment.Enabled, "Enable deployment subroutine")
	fs.StringVar(&c.Subroutines.Deployment.AuthorizationWebhookSecretName, "authorization-webhook-secret-name", c.Subroutines.Deployment.AuthorizationWebhookSecretName, "Authorization webhook secret name")
	fs.StringVar(&c.Subroutines.Deployment.AuthorizationWebhookSecretCAName, "authorization-webhook-secret-ca-name", c.Subroutines.Deployment.AuthorizationWebhookSecretCAName, "Authorization webhook CA secret name")
	fs.BoolVar(&c.Subroutines.Deployment.EnableIstio, "subroutines-deployment-enable-istio", c.Subroutines.Deployment.EnableIstio, "Enable Istio integration in deployment subroutine")

	fs.BoolVar(&c.Subroutines.KcpSetup.Enabled, "subroutines-kcp-setup-enabled", c.Subroutines.KcpSetup.Enabled, "Enable KCP setup subroutine")
	fs.StringVar(&c.Subroutines.KcpSetup.DomainCertificateCASecretName, "domain-certificate-ca-secret-name", c.Subroutines.KcpSetup.DomainCertificateCASecretName, "Domain certificate secret name")
	fs.StringVar(&c.Subroutines.KcpSetup.DomainCertificateCASecretKey, "domain-certificate-ca-secret-key", c.Subroutines.KcpSetup.DomainCertificateCASecretKey, "Domain certificate secret key")

	fs.BoolVar(&c.Subroutines.ProviderSecret.Enabled, "subroutines-provider-secret-enabled", c.Subroutines.ProviderSecret.Enabled, "Enable provider secret subroutine")
	fs.BoolVar(&c.Subroutines.FeatureToggles.Enabled, "subroutines-feature-toggles-enabled", c.Subroutines.FeatureToggles.Enabled, "Enable feature toggles subroutine")
	fs.BoolVar(&c.Subroutines.Wait.Enabled, "subroutines-wait-enabled", c.Subroutines.Wait.Enabled, "Enable wait subroutine")
	fs.BoolVar(&c.Subroutines.ManagedProvider.ProviderResource.Enabled, "subroutines-managed-provider-resource-enabled", c.Subroutines.ManagedProvider.ProviderResource.Enabled, "Enable ManagedProvider provider-resource subroutine")
	fs.BoolVar(&c.Subroutines.ManagedProvider.WaitProvider.Enabled, "subroutines-managed-provider-wait-enabled", c.Subroutines.ManagedProvider.WaitProvider.Enabled, "Enable ManagedProvider wait-provider subroutine")
	fs.BoolVar(&c.Subroutines.ManagedProvider.KubeconfigCopy.Enabled, "subroutines-managed-provider-kubeconfig-enabled", c.Subroutines.ManagedProvider.KubeconfigCopy.Enabled, "Enable ManagedProvider kubeconfig-copy subroutine")
	fs.BoolVar(&c.Subroutines.ManagedProvider.Deploy.Enabled, "subroutines-managed-provider-deploy-enabled", c.Subroutines.ManagedProvider.Deploy.Enabled, "Enable ManagedProvider deploy subroutine")
	fs.BoolVar(&c.Subroutines.Provider.Workspace.Enabled, "subroutines-providers-workspace-enabled", c.Subroutines.Provider.Workspace.Enabled, "Enable Provider workspace subroutine")
	fs.BoolVar(&c.Subroutines.Provider.Kubeconfig.Enabled, "subroutines-providers-kubeconfig-enabled", c.Subroutines.Provider.Kubeconfig.Enabled, "Enable Provider scoped-kubeconfig subroutine")

	fs.StringVar(&c.Providers.ProvidersAPIExportEndpointSliceName, "providers-apiexport-endpointslice-name", c.Providers.ProvidersAPIExportEndpointSliceName, "Set name of the Providers APIExport endpoint slice to use")
	fs.StringVar(&c.Providers.ProvidersAPIExportEndpointSliceWorkspace, "providers-apiexport-endpointslice-workspace", c.Providers.ProvidersAPIExportEndpointSliceWorkspace, "Set workspace of the Providers APIExport endpoint slice to use")

	fs.StringVar(&c.RemoteRuntime.Kubeconfig, "remote-runtime-kubeconfig", c.RemoteRuntime.Kubeconfig, "Kubeconfig for remote runtime cluster")
	fs.StringVar(&c.RemoteRuntime.InfraSecretName, "remote-runtime-infra-secret-name", c.RemoteRuntime.InfraSecretName, "Secret name for remote runtime infra kubeconfig")
	fs.StringVar(&c.RemoteRuntime.InfraSecretKey, "remote-runtime-infra-secret-key", c.RemoteRuntime.InfraSecretKey, "Secret key for remote runtime infra kubeconfig")

	fs.StringVar(&c.RemoteInfra.Kubeconfig, "remote-infra-kubeconfig", c.RemoteInfra.Kubeconfig, "Kubeconfig for remote infra cluster")
}

type ProviderSubroutinesConfig struct {
	Workspace  ProviderSubroutineConfig
	Kubeconfig ProviderSubroutineConfig
}

type ProvidersConfig struct {
	ProvidersAPIExportEndpointSliceName      string
	ProvidersAPIExportEndpointSliceWorkspace string
}

func NewProvidersConfig() ProvidersConfig {
	return ProvidersConfig{
		ProvidersAPIExportEndpointSliceName:      "providers.platform-mesh.io",
		ProvidersAPIExportEndpointSliceWorkspace: "root:platform-mesh-system",
	}
}
