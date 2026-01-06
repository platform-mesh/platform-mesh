package config

type InviteConfig struct {
	KeycloakBaseURL      string `mapstructure:"invite-keycloak-base-url"`
	KeycloakClientID     string `mapstructure:"invite-keycloak-client-id" default:"security-operator"`
	KeycloakClientSecret string `mapstructure:"invite-keycloak-client-secret"`
}

// Config struct to hold the app config
type Config struct {
	FGA struct {
		Target string `mapstructure:"fga-target"`
	} `mapstructure:",squash"`
	KCP struct {
		Kubeconfig string `mapstructure:"kcp-kubeconfig" default:"/api-kubeconfig/kubeconfig"`
	} `mapstructure:",squash"`
	APIExportEndpointSliceName string `mapstructure:"api-export-endpoint-slice-name"`
	CoreModulePath             string `mapstructure:"core-module-path"`
	BaseDomain                 string `mapstructure:"base-domain" default:"portal.dev.local:8443"`
	GroupClaim                 string `mapstructure:"group-claim" default:"groups"`
	UserClaim                  string `mapstructure:"user-claim" default:"email"`
	InitializerName            string `mapstructure:"initializer-name" default:"root:security"`
	DomainCALookup             bool   `mapstructure:"domain-ca-lookup" default:"false"`
	HttpClientTimeoutSeconds   int    `mapstructure:"http-client-timeout-seconds" default:"30"`
	IDP                        struct {
		// SMTP settings
		SMTPServer  string `mapstructure:"idp-smtp-server"`
		SMTPPort    int    `mapstructure:"idp-smtp-port"`
		FromAddress string `mapstructure:"idp-from-address"`

		// SSL settings
		SSL      bool `mapstructure:"idp-smtp-ssl" default:"false"`
		StartTLS bool `mapstructure:"idp-smtp-starttls" default:"false"`

		// Auth settings
		SMTPUser     string `mapstructure:"idp-smtp-user"`
		SMTPPassword string `mapstructure:"idp-smtp-password"`

		AdditionalRedirectURLs []string `mapstructure:"idp-additional-redirect-urls"`
	} `mapstructure:",squash"`
	Invite InviteConfig `mapstructure:",squash"`
}
