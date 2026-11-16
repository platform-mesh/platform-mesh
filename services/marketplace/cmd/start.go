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

	pmuiv1alpha1 "go.platform-mesh.io/apis/ui/v1alpha1"
	"go.platform-mesh.io/marketplace/pkg/marketplace"
	"go.platform-mesh.io/platform-mesh/libs/virtual-workspaces/server"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	virtualrootapiserver "github.com/kcp-dev/virtual-workspace-framework/pkg/rootapiserver"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpapisv1alpha2.AddToScheme(scheme))
	utilruntime.Must(kcpcorev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha1.AddToScheme(scheme))
	utilruntime.Must(pmuiv1alpha1.AddToScheme(scheme))
}

var startCmd = &cobra.Command{
	Use: "start",
	RunE: func(cmd *cobra.Command, args []string) error {
		return server.Run(cmd.Context(), serverOpts, scheme, func(ctx context.Context, clients server.Clients) ([]virtualrootapiserver.NamedVirtualWorkspace, error) {
			// ProviderMetadata and APIExports of every provider, through the
			// APIExport the kubeconfig points at.
			provider, err := apiexport.New(clients.ProviderConfig, cfg.APIExportEndpointSliceName, apiexport.Options{
				Scheme: scheme,
			})
			if err != nil {
				return nil, err
			}

			go func() {
				if err := provider.Start(ctx, nil); err != nil {
					klog.ErrorS(err, "apiexport provider stopped with error")
				}
			}()

			return []virtualrootapiserver.NamedVirtualWorkspace{
				marketplace.BuildVirtualWorkspace(ctx, cfg, clients.Dynamic, clients.Cluster, marketplace.VirtualWorkspaceBaseURL(), provider),
			}, nil
		})
	},
}
