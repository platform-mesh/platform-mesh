package config

import "github.com/spf13/pflag"

type WebhooksConfig struct {
	Enabled                bool
	CertDir                string
	Port                   int
	DenyList               string
	AdditionalAccountTypes []string
}

type WorkspaceTypeSubroutineConfig struct {
	Enabled bool
}

type WorkspaceSubroutineConfig struct {
	Enabled bool
}

type WorkspaceReadySubroutineConfig struct {
	Enabled bool
}

type AccountInfoSubroutineConfig struct {
	Enabled bool
}

type SubroutinesConfig struct {
	WorkspaceType  WorkspaceTypeSubroutineConfig
	Workspace      WorkspaceSubroutineConfig
	WorkspaceReady WorkspaceReadySubroutineConfig
	AccountInfo    AccountInfoSubroutineConfig
}

type AccountInfoControllerConfig struct {
	Enabled bool
}

type ControllersConfig struct {
	AccountInfo AccountInfoControllerConfig
}

type KcpConfig struct {
	ApiExportEndpointSliceName string
	ProviderWorkspace          string
}

type OperatorConfig struct {
	Webhooks    WebhooksConfig
	Subroutines SubroutinesConfig
	Controllers ControllersConfig
	Kcp         KcpConfig
}

func NewOperatorConfig() OperatorConfig {
	return OperatorConfig{
		Webhooks: WebhooksConfig{
			CertDir: "certs",
			Port:    9443,
		},
		Subroutines: SubroutinesConfig{
			WorkspaceType: WorkspaceTypeSubroutineConfig{
				Enabled: true,
			},
			Workspace: WorkspaceSubroutineConfig{
				Enabled: true,
			},
			WorkspaceReady: WorkspaceReadySubroutineConfig{
				Enabled: true,
			},
			AccountInfo: AccountInfoSubroutineConfig{
				Enabled: true,
			},
		},
		Controllers: ControllersConfig{
			AccountInfo: AccountInfoControllerConfig{
				Enabled: true,
			},
		},
		Kcp: KcpConfig{
			ApiExportEndpointSliceName: "core.platform-mesh.io",
			ProviderWorkspace:          "root",
		},
	}
}

func (c *OperatorConfig) AddFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&c.Webhooks.Enabled, "webhooks-enabled", c.Webhooks.Enabled, "Enable webhook server")
	fs.StringVar(&c.Webhooks.CertDir, "webhooks-cert-dir", c.Webhooks.CertDir, "Set webhook certificate directory")
	fs.IntVar(&c.Webhooks.Port, "webhooks-port", c.Webhooks.Port, "Set webhook server port")
	fs.StringVar(&c.Webhooks.DenyList, "webhooks-deny-list", c.Webhooks.DenyList, "Comma-separated list of denied account names")
	fs.StringSliceVar(&c.Webhooks.AdditionalAccountTypes, "webhooks-additional-account-types", c.Webhooks.AdditionalAccountTypes, "Additional allowed account types")

	fs.BoolVar(&c.Subroutines.WorkspaceType.Enabled, "subroutines-workspace-type-enabled", c.Subroutines.WorkspaceType.Enabled, "Enable workspace type subroutine")
	fs.BoolVar(&c.Subroutines.Workspace.Enabled, "subroutines-workspace-enabled", c.Subroutines.Workspace.Enabled, "Enable workspace subroutine")
	fs.BoolVar(&c.Subroutines.WorkspaceReady.Enabled, "subroutines-workspace-ready-enabled", c.Subroutines.WorkspaceReady.Enabled, "Enable workspace ready subroutine")
	fs.BoolVar(&c.Subroutines.AccountInfo.Enabled, "subroutines-account-info-enabled", c.Subroutines.AccountInfo.Enabled, "Enable account info subroutine")

	fs.BoolVar(&c.Controllers.AccountInfo.Enabled, "controllers-account-info-enabled", c.Controllers.AccountInfo.Enabled, "Enable account info controller")

	fs.StringVar(&c.Kcp.ApiExportEndpointSliceName, "kcp-api-export-endpoint-slice-name", c.Kcp.ApiExportEndpointSliceName, "Set APIExportEndpointSlice name")
	fs.StringVar(&c.Kcp.ProviderWorkspace, "kcp-provider-workspace", c.Kcp.ProviderWorkspace, "Set provider workspace")
}
