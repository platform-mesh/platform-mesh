package config

// OperatorConfig struct to hold the app config
type OperatorConfig struct {
	Webhooks struct {
		Enabled  bool   `mapstructure:"webhooks-enabled" default:"false"`
		CertDir  string `mapstructure:"webhooks-cert-dir" default:"certs"`
		Port     int    `mapstructure:"webhooks-port" default:"9443"`
		DenyList string `mapstructure:"webhooks-deny-list"`
	} `mapstructure:",squash"`
	Subroutines struct {
		Workspace struct {
			Enabled bool `mapstructure:"subroutines-workspace-enabled" default:"true"`
		} `mapstructure:",squash"`
		AccountInfo struct {
			Enabled bool `mapstructure:"subroutines-account-info-enabled" default:"true"`
		} `mapstructure:",squash"`
		FGA struct {
			Enabled         bool   `mapstructure:"subroutines-fga-enabled" default:"true"`
			RootNamespace   string `mapstructure:"subroutines-fga-root-namespace" default:"platform-mesh-root"`
			GrpcAddr        string `mapstructure:"subroutines-fga-grpc-addr" default:"localhost:8081"`
			ObjectType      string `mapstructure:"subroutines-fga-object-type" default:"core_platform-mesh_io_account"`
			ParentRelation  string `mapstructure:"subroutines-fga-parent-relation" default:"parent"`
			CreatorRelation string `mapstructure:"subroutines-fga-creator-relation" default:"owner"`
		} `mapstructure:",squash"`
	} `mapstructure:",squash"`
	Kcp struct {
		ApiExportEndpointSliceName string `mapstructure:"kcp-api-export-endpoint-slice-name"`
		ProviderWorkspace          string `mapstructure:"kcp-provider-workspace" default:"root"`
	} `mapstructure:",squash"`
}
