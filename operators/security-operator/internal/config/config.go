package config

// Config struct to hold the app config
type Config struct {
	FGA struct {
		Target string `mapstructure:"fga-target"`
	} `mapstructure:",squash"`
	APIExportEndpointSliceName string `mapstructure:"api-export-endpoint-slice-name"`
	CoreModulePath             string `mapstructure:"core-module-path"`
	WorkspaceDir               string `mapstructure:"workspace-dir" default:"/operator/"`
	BaseDomain                 string `mapstructure:"base-domain" default:"portal.dev.local:8443"`
	GroupClaim                 string `mapstructure:"group-claim" default:"groups"`
	UserClaim                  string `mapstructure:"user-claim" default:"email"`
	DomainCALookup             bool   `mapstructure:"domain-ca-lookup" default:"false"`
}
