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
	InitializerName            string `mapstructure:"initializer-name" default:"root:security"`
	DomainCALookup             bool   `mapstructure:"domain-ca-lookup" default:"false"`
	IDP                        struct {
		// SMTP settings
		SMTPServer  string `mapstructure:"idp-smtp-server"`
		SMTPPort    int    `mapstructure:"idp-smtp-port"`
		FromAddress string `mapstructure:"idp-from-address"`

		// SSL settings
		SSL      bool `mapstructure:"idp-smtp-ssl" default:"false"`
		StartTLS bool `mapstructure:"idp-smtp-starttls" default:"false"`

		// Auth settings
		SMTPUser               string `mapstructure:"idp-smtp-user"`
		SMTPPasswordSecretName string `mapstructure:"idp-smtp-password-secret-name"`
		SMTPPasswordSecretKey  string `mapstructure:"idp-smtp-password-secret-key" default:"password"`

		AdditionalRedirectURLs []string `mapstructure:"idp-additional-redirect-urls"`
	} `mapstructure:",squash"`
}
