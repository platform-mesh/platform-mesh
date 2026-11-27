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
	"errors"
	"path/filepath"

	"go.platform-mesh.io/platform-mesh/cmd/pm/pkg/certs"
	authzapp "go.platform-mesh.io/pm-openfga-authz-webhook/pkg/app"
	authzconfig "go.platform-mesh.io/pm-openfga-authz-webhook/pkg/config"
)

// AuthzWebhookName is the name of the AuthzWebhook component.
const AuthzWebhookName = "pm-openfga-authz-webhook"

// AuthzWebhookPath is the path of the webhook's SubjectAccessReview
// endpoint.
const AuthzWebhookPath = authzapp.AuthzPath

// AuthzWebhookCertDir is where the webhook's serving certificate is kept
// under the data directory. kcp is given its CA before it starts.
func AuthzWebhookCertDir(dataDir string) string {
	return filepath.Join(dataDir, "authz-webhook")
}

// AuthzWebhook answers kcp's authorization questions from OpenFGA: kcp asks
// it about every request its own RBAC does not allow. It only ever grants,
// so the kcp admin the components of the process act as is not affected.
//
// It starts serving once the "orgs" store exists in OpenFGA, which the
// pm-openfga-operator creates; until then kcp gets no answer from it and
// falls back to its RBAC alone.
func AuthzWebhook() Component {
	return Component{
		Name:     AuthzWebhookName,
		Bindings: []string{coreExport, openfgaExport},
		Run: func(ctx context.Context, env Env) error {
			if env.OpenFGA == nil {
				return errors.New("needs OpenFGA")
			}
			dir := AuthzWebhookCertDir(env.DataDir)
			if _, _, err := certs.Ensure(dir); err != nil {
				return err
			}
			cfg := authzconfig.New()
			cfg.MetricsBindAddress, cfg.HealthProbeBindAddress = "0", "0"
			cfg.KCPWorkspace = env.Platform.System.String()
			cfg.StoreWaitTimeout = 0
			cfg.OpenFGAAddr = env.OpenFGA.Addr
			cfg.Webhook.Host = "127.0.0.1"
			cfg.Webhook.Port = env.AuthzWebhookPort
			cfg.Webhook.CertDir = dir
			return authzapp.Run(ctx, env.Config, cfg, authzapp.Deps{OpenFGA: env.OpenFGA.Client})
		},
	}
}
