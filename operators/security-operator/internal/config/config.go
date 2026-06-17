package config

import (
	"os"
	"time"

	"github.com/spf13/pflag"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

const (
	CoreProviderName   = "core"
	SystemProviderName = "system"
	providerSeparator  = "#"
	OrgsClusterPath    = "root:orgs"
)

type KeycloakConfig struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
}

type WebhooksConfig struct {
	Enabled bool
	Port    int
	CertDir string
}

type InitializerConfig struct {
	WorkspaceInitializerEnabled bool
	IDPEnabled                  bool
	InviteEnabled               bool
	WorkspaceAuthEnabled        bool
}

type FGAConfig struct {
	Target          string
	ObjectType      string
	ParentRelation  string
	CreatorRelation string
	StoreIDCacheTTL time.Duration
}

type KCPConfig struct {
	Kubeconfig string
}

type IDPConfig struct {
	RealmDenyList []string

	SMTPServer  string
	SMTPPort    int
	FromAddress string

	SSL      bool
	StartTLS bool

	SMTPUser     string
	SMTPPassword string

	AdditionalRedirectURLs    []string
	KubectlClientRedirectURLs []string

	AccessTokenLifespan int
	RegistrationAllowed bool
}

type APIExportEndpointSlices struct {
	CorePlatformMeshIO   string
	SystemPlatformMeshIO string
}

// Config struct to hold the app config
type Config struct {
	FGA                              FGAConfig
	KCP                              KCPConfig
	APIExportEndpointSlices          APIExportEndpointSlices
	CoreModulePath                   string
	BaseDomain                       string
	GroupClaim                       string
	UserClaim                        string
	DevelopmentAllowUnverifiedEmails bool
	WorkspacePath                    string
	WorkspaceTypeName                string
	DomainCALookup                   bool
	MigrateAuthorizationModels       bool
	HttpClientTimeoutSeconds         int
	SetDefaultPassword               bool
	AllowMemberTuplesEnabled         bool
	IDP                              IDPConfig
	Keycloak                         KeycloakConfig
	Initializer                      InitializerConfig
	Webhooks                         WebhooksConfig
	AdditionalAudiences              []string
}

func NewConfig() Config {
	return Config{
		FGA: FGAConfig{
			ObjectType:      "core_platform-mesh_io_account",
			ParentRelation:  "parent",
			CreatorRelation: "owner",
			StoreIDCacheTTL: 24 * time.Hour,
		},
		KCP: KCPConfig{
			Kubeconfig: "/api-kubeconfig/kubeconfig",
		},
		APIExportEndpointSlices: APIExportEndpointSlices{
			CorePlatformMeshIO:   "core.platform-mesh.io",
			SystemPlatformMeshIO: "system.platform-mesh.io",
		},
		BaseDomain:               "portal.dev.local:8443",
		GroupClaim:               "groups",
		UserClaim:                "email",
		WorkspacePath:            "root",
		WorkspaceTypeName:        "security",
		HttpClientTimeoutSeconds: 30,
		IDP: IDPConfig{
			KubectlClientRedirectURLs: []string{"http://localhost:8000", "http://localhost:18000"},
			AccessTokenLifespan:       28800,
		},
		Keycloak: KeycloakConfig{
			ClientID:     "security-operator",
			ClientSecret: os.Getenv("KEYCLOAK_CLIENT_SECRET"),
		},
		Initializer: InitializerConfig{
			WorkspaceInitializerEnabled: true,
			IDPEnabled:                  true,
			InviteEnabled:               true,
			WorkspaceAuthEnabled:        true,
		},
		Webhooks: WebhooksConfig{
			Port:    9443,
			CertDir: "/tmp/k8s-webhook-server/serving-certs",
		},
	}
}

func (c *Config) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.FGA.Target, "fga-target", c.FGA.Target, "Set the OpenFGA API target")
	fs.DurationVar(&c.FGA.StoreIDCacheTTL, "fga-store-id-cache-ttl", c.FGA.StoreIDCacheTTL, "TTL for the OpenFGA store ID cache (e.g. 5m, 1h)")
	fs.StringVar(&c.FGA.ObjectType, "fga-object-type", c.FGA.ObjectType, "Set the OpenFGA object type for account tuples")
	fs.StringVar(&c.FGA.ParentRelation, "fga-parent-relation", c.FGA.ParentRelation, "Set the OpenFGA parent relation name")
	fs.StringVar(&c.FGA.CreatorRelation, "fga-creator-relation", c.FGA.CreatorRelation, "Set the OpenFGA creator relation name")
	fs.StringVar(&c.KCP.Kubeconfig, "kcp-kubeconfig", c.KCP.Kubeconfig, "Set the KCP kubeconfig path")
	fs.StringVar(&c.APIExportEndpointSlices.CorePlatformMeshIO, "api-export-endpoint-slice-name", c.APIExportEndpointSlices.CorePlatformMeshIO, "Set the core.platform-mesh.io APIExportEndpointSlice name")
	fs.StringVar(&c.APIExportEndpointSlices.SystemPlatformMeshIO, "system-api-export-endpoint-slice-name", c.APIExportEndpointSlices.SystemPlatformMeshIO, "Set the system.platform-mesh.io APIExportEndpointSlice name")
	fs.StringVar(&c.CoreModulePath, "core-module-path", c.CoreModulePath, "Set the path to the core module FGA model file")
	fs.StringVar(&c.BaseDomain, "base-domain", c.BaseDomain, "Set the base domain used to construct issuer URLs")
	fs.StringVar(&c.GroupClaim, "group-claim", c.GroupClaim, "Set the ID token group claim")
	fs.StringVar(&c.UserClaim, "user-claim", c.UserClaim, "Set the ID token user claim")
	fs.BoolVar(&c.DevelopmentAllowUnverifiedEmails, "development-allow-unverified-emails", c.DevelopmentAllowUnverifiedEmails, "Allow unverified emails in development mode")
	fs.StringVar(&c.WorkspacePath, "workspace-path", c.WorkspacePath, "Set the parent workspace path for created workspaces")
	fs.StringVar(&c.WorkspaceTypeName, "workspace-type-name", c.WorkspaceTypeName, "Set the workspace type name")
	fs.BoolVar(&c.DomainCALookup, "domain-ca-lookup", c.DomainCALookup, "Enable lookup of domain CA from Kubernetes secret")
	fs.BoolVar(&c.MigrateAuthorizationModels, "migrate-authorization-models", c.MigrateAuthorizationModels, "Enable one-time authorization model migration")
	fs.IntVar(&c.HttpClientTimeoutSeconds, "http-client-timeout-seconds", c.HttpClientTimeoutSeconds, "Set HTTP client timeout in seconds")
	fs.BoolVar(&c.SetDefaultPassword, "set-default-password", c.SetDefaultPassword, "Enable setting default password for identity provider users")
	fs.BoolVar(&c.AllowMemberTuplesEnabled, "allow-member-tuples-enabled", c.AllowMemberTuplesEnabled, "Enable allow-member tuples management")
	fs.StringSliceVar(&c.IDP.RealmDenyList, "idp-realm-deny-list", c.IDP.RealmDenyList, "Comma-separated list of Keycloak realms to ignore")
	fs.StringVar(&c.IDP.SMTPServer, "idp-smtp-server", c.IDP.SMTPServer, "Set Keycloak SMTP server host")
	fs.IntVar(&c.IDP.SMTPPort, "idp-smtp-port", c.IDP.SMTPPort, "Set Keycloak SMTP server port")
	fs.StringVar(&c.IDP.FromAddress, "idp-from-address", c.IDP.FromAddress, "Set SMTP from address")
	fs.BoolVar(&c.IDP.SSL, "idp-smtp-ssl", c.IDP.SSL, "Enable SMTP SSL")
	fs.BoolVar(&c.IDP.StartTLS, "idp-smtp-starttls", c.IDP.StartTLS, "Enable SMTP STARTTLS")
	fs.StringVar(&c.IDP.SMTPUser, "idp-smtp-user", c.IDP.SMTPUser, "Set SMTP username")
	fs.StringVar(&c.IDP.SMTPPassword, "idp-smtp-password", c.IDP.SMTPPassword, "Set SMTP password")
	fs.StringSliceVar(&c.IDP.AdditionalRedirectURLs, "idp-additional-redirect-urls", c.IDP.AdditionalRedirectURLs, "Additional redirect URLs for Keycloak clients")
	fs.StringSliceVar(&c.IDP.KubectlClientRedirectURLs, "idp-kubectl-client-redirect-urls", c.IDP.KubectlClientRedirectURLs, "Redirect URLs for the kubectl Keycloak client")
	fs.IntVar(&c.IDP.AccessTokenLifespan, "idp-access-token-lifespan", c.IDP.AccessTokenLifespan, "Keycloak access token lifespan in seconds")
	fs.BoolVar(&c.IDP.RegistrationAllowed, "idp-registration-allowed", c.IDP.RegistrationAllowed, "Enable Keycloak self-registration")
	fs.StringVar(&c.Keycloak.BaseURL, "keycloak-base-url", c.Keycloak.BaseURL, "Set Keycloak base URL")
	fs.StringVar(&c.Keycloak.ClientID, "keycloak-client-id", c.Keycloak.ClientID, "Set Keycloak client ID")
	fs.BoolVar(&c.Initializer.WorkspaceInitializerEnabled, "initializer-workspace-enabled", c.Initializer.WorkspaceInitializerEnabled, "Enable workspace initialization")
	fs.BoolVar(&c.Initializer.IDPEnabled, "initializer-idp-enabled", c.Initializer.IDPEnabled, "Enable IDP initialization")
	fs.BoolVar(&c.Initializer.InviteEnabled, "initializer-invite-enabled", c.Initializer.InviteEnabled, "Enable invite initialization")
	fs.BoolVar(&c.Initializer.WorkspaceAuthEnabled, "initializer-workspace-auth-enabled", c.Initializer.WorkspaceAuthEnabled, "Enable workspace auth initialization")
	fs.StringSliceVar(&c.AdditionalAudiences, "additional-audiences", c.AdditionalAudiences, "Additional audiences to trust in workspace JWT authentication configurations")
	fs.BoolVar(&c.Webhooks.Enabled, "webhooks-enabled", c.Webhooks.Enabled, "Enable validating webhooks")
	fs.IntVar(&c.Webhooks.Port, "webhooks-port", c.Webhooks.Port, "Set webhook server port")
	fs.StringVar(&c.Webhooks.CertDir, "webhooks-cert-dir", c.Webhooks.CertDir, "Set webhook certificate directory")
}

func (config Config) InitializerName() string {
	return config.WorkspacePath + ":" + config.WorkspaceTypeName
}

func (config Config) TerminatorName() string {
	return config.WorkspacePath + ":" + config.WorkspaceTypeName
}

// MultiProviderName returns a cluster name with provider prefix and separator for multi provider.
// The multi.Provider prefixes cluster names as "providerName#clusterName"
func MultiProviderName(providerName, clusterName string) multicluster.ClusterName {
	return multicluster.ClusterName(providerName + providerSeparator + clusterName)
}
