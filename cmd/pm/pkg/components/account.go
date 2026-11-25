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

package components

import (
	"context"
	"os"
	"path/filepath"

	"go.platform-mesh.io/account-operator/pkg/app"
	"go.platform-mesh.io/account-operator/pkg/config"
	accountresources "go.platform-mesh.io/apis/static-assets/resources/account-operator"
)

const (
	coreExport    = "core.platform-mesh.io"
	openfgaExport = "openfga.platform-mesh.io"
	gatewayExport = "gateway.platform-mesh.io"
)

// AccountOperator reconciles Accounts into workspaces and serves the
// admission webhooks kcp calls for every Account.
func AccountOperator() Component {
	return Component{
		Name:      "account-operator",
		Resources: accountresources.Resources,
		Bindings:  []string{coreExport},
		Run: func(ctx context.Context, env Env) error {
			log, err := newLogger("account-operator", env.LogLevel)
			if err != nil {
				return err
			}
			cfg := config.NewOperatorConfig()
			cfg.Kcp.Workspace = env.Platform.System.String()
			cfg.Kcp.ApiExportEndpointSliceName = coreExport
			cfg.Kcp.ProviderWorkspace = env.Platform.Root.String()
			if env.AccountWebhookPort != 0 {
				// The bootstrap registered this server in
				// :root:platform-mesh-system.
				cfg.Webhooks.Enabled = true
				cfg.Webhooks.Host = "127.0.0.1"
				cfg.Webhooks.Port = env.AccountWebhookPort
				cfg.Webhooks.CertDir = env.AccountWebhookCertDir
				// As the chart: accounts in an organization, besides
				// organizations.
				cfg.Webhooks.AdditionalAccountTypes = []string{"account"}
			}
			if env.OIDC != nil {
				// Published in every AccountInfo, for the kubeconfigs clients
				// build. The client secret is public anyway.
				secrets := filepath.Join(env.DataDir, "oidc", "client-secrets")
				if err := os.MkdirAll(secrets, 0o700); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(secrets, "kubectl"), []byte(env.OIDC.ClientSecret), 0o600); err != nil {
					return err
				}
				cfg.OIDC.IssuerURL = env.OIDC.IssuerURL
				cfg.OIDC.Clients = map[string]string{"kubectl": env.OIDC.ClientID}
				cfg.OIDC.ClientSecretsDir = secrets
				cfg.OIDC.Scopes = []string{"email", "groups"}
				cfg.OIDC.CAFile = env.OIDC.CAFile
			}
			return app.Run(ctx, env.Config, cfg, app.Deps{Log: log, Common: quietCommon()})
		},
	}
}
