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

	uiresources "go.platform-mesh.io/apis/static-assets/resources/extension-manager-operator"
	"go.platform-mesh.io/extension-manager-operator/pkg/app"
	"go.platform-mesh.io/extension-manager-operator/pkg/config"
)

const uiExport = "ui.platform-mesh.io"

// ExtensionManager reconciles ContentConfigurations, the pieces of
// navigation and content the portal is assembled from: it resolves each
// one's configuration into its status, which is what the portal reads
// (through the contentconfigurations virtual workspace).
//
// Manifests are the ContentConfigurations of the portal's own pages; the
// ones of the IAM and marketplace UIs are left out, those UIs do not run.
func ExtensionManager() Component {
	return Component{
		Name:      "extension-manager-operator",
		Resources: uiresources.Resources,
		Bindings:  []string{uiExport},
		Manifests: []string{
			"01-platform-mesh-system/contentconfiguration-main-home.yaml",
			"01-platform-mesh-system/contentconfiguration-main-accounts.yaml",
			"01-platform-mesh-system/contentconfiguration-account-home.yaml",
			"01-platform-mesh-system/contentconfiguration-account-namespace.yaml",
		},
		Run: func(ctx context.Context, env Env) error {
			log, err := newLogger("extension-manager-operator", env.LogLevel)
			if err != nil {
				return err
			}
			operator := config.NewOperatorConfig()
			operator.KCPWorkspace = env.Platform.System.String()
			operator.KCPAPIExportEndpointSliceName = uiExport
			return app.Run(ctx, env.Config, &config.Config{Common: quietCommon(), Operator: operator}, app.Deps{Log: log})
		},
	}
}
