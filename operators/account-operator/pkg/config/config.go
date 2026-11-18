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
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/pflag"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
)

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

// OIDCConfig is an OIDC issuer that is the same for every account: an external
// issuer, used instead of Keycloak. Without an issuer the account-operator
// leaves the OIDC of an AccountInfo to the pm-keycloak-operator, which knows
// the realm and clients of each organization.
type OIDCConfig struct {
	IssuerURL string
	// Clients maps a client name, as consumers look it up, to its client ID.
	Clients map[string]string
	Scopes  []string
	// CAFile is a PEM file with the CA of the issuer, for an issuer whose
	// certificate is not publicly trusted. Read once, at startup.
	CAFile string
}

// Info returns what every AccountInfo has as OIDC, nil without an issuer.
func (c OIDCConfig) Info() (*pmcorev1alpha1.OIDCInfo, error) {
	if c.IssuerURL == "" {
		return nil, nil
	}
	var ca string
	if c.CAFile != "" {
		pem, err := os.ReadFile(c.CAFile)
		if err != nil {
			return nil, fmt.Errorf("reading --oidc-ca-file: %w", err)
		}
		if !strings.Contains(string(pem), "-----BEGIN CERTIFICATE-----") {
			return nil, fmt.Errorf("--oidc-ca-file %s holds no PEM certificate", c.CAFile)
		}
		ca = string(pem)
	}
	clients := make(map[string]pmcorev1alpha1.ClientInfo, len(c.Clients))
	for name, id := range c.Clients {
		clients[name] = pmcorev1alpha1.ClientInfo{ClientID: id}
	}
	return &pmcorev1alpha1.OIDCInfo{
		IssuerURL: c.IssuerURL,
		Clients:   clients,
		Scopes:    append([]string(nil), c.Scopes...),
		CA:        ca,
	}, nil
}

func (c OIDCConfig) validate() error {
	if c.IssuerURL == "" {
		if len(c.Clients) > 0 || len(c.Scopes) > 0 || c.CAFile != "" {
			return errors.New("--oidc-clients, --oidc-scopes and --oidc-ca-file need --oidc-issuer-url")
		}
		return nil
	}
	if !strings.HasPrefix(c.IssuerURL, "https://") {
		return errors.New("--oidc-issuer-url must start with https://")
	}
	if len(c.Clients) == 0 {
		return errors.New("--oidc-issuer-url needs at least one client in --oidc-clients")
	}
	for name, id := range c.Clients {
		if name == "" || id == "" {
			return errors.New("--oidc-clients needs name=clientID pairs, both non-empty")
		}
	}
	return nil
}

type OperatorConfig struct {
	Webhooks    WebhooksConfig
	Subroutines SubroutinesConfig
	Controllers ControllersConfig
	Kcp         KcpConfig
	OIDC        OIDCConfig
}

// Validate reports flags that contradict each other.
func (c *OperatorConfig) Validate() error {
	return c.OIDC.validate()
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

	fs.StringVar(&c.OIDC.IssuerURL, "oidc-issuer-url", c.OIDC.IssuerURL, "External OIDC issuer to publish in every AccountInfo; leave empty with Keycloak, whose operator publishes the realm of each organization")
	fs.StringToStringVar(&c.OIDC.Clients, "oidc-clients", c.OIDC.Clients, "Clients of --oidc-issuer-url as name=clientID pairs, e.g. kubectl=platform-mesh")
	fs.StringSliceVar(&c.OIDC.Scopes, "oidc-scopes", c.OIDC.Scopes, "Scopes clients of --oidc-issuer-url request besides openid, e.g. email,groups")
	fs.StringVar(&c.OIDC.CAFile, "oidc-ca-file", c.OIDC.CAFile, "PEM file with the CA of --oidc-issuer-url, published for clients to verify the issuer with; leave empty for a publicly trusted certificate")
}
