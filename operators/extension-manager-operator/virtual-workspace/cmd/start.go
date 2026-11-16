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

package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"go.platform-mesh.io/extension-manager-operator/virtual-workspace/pkg/contentconfiguration"
	"go.platform-mesh.io/platform-mesh/libs/virtual-workspaces/server"

	"k8s.io/apimachinery/pkg/runtime"

	virtualrootapiserver "github.com/kcp-dev/virtual-workspace-framework/pkg/rootapiserver"
)

var startCmd = &cobra.Command{
	Use: "start",
	RunE: func(cmd *cobra.Command, args []string) error {
		return server.Run(cmd.Context(), serverOpts, runtime.NewScheme(), func(ctx context.Context, clients server.Clients) ([]virtualrootapiserver.NamedVirtualWorkspace, error) {
			return []virtualrootapiserver.NamedVirtualWorkspace{
				contentconfiguration.BuildVirtualWorkspace(ctx, cfg, clients.Dynamic, clients.Cluster, contentconfiguration.VirtualWorkspaceBaseURL()),
			}, nil
		})
	},
}
