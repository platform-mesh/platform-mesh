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
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sync/errgroup"

	gatewayresources "go.platform-mesh.io/apis/static-assets/resources/kubernetes-graphql-gateway"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway"
	gatewayoptions "go.platform-mesh.io/kubernetes-graphql-gateway/gateway/options"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener"
	listeneroptions "go.platform-mesh.io/kubernetes-graphql-gateway/listener/options"
)

// GraphQLGateway serves the workspaces as GraphQL at
// http://localhost:<graphql-port>/api/clusters/<workspace path>/graphql. It is the
// listener, which writes a schema per workspace bound to
// core.platform-mesh.io, and the gateway, which serves them; in a cluster
// they are two Deployments sharing a volume, here two goroutines sharing a
// directory.
func GraphQLGateway() Component {
	return Component{
		Name: "kubernetes-graphql-gateway",
		// Schemas are generated for the workspaces that bind this export.
		Bindings: []string{coreExport, gatewayExport},
		// ClusterAccess, with which the gateway is pointed at an API that
		// is not a workspace: the marketplace virtual workspace.
		Resources: gatewayresources.Resources,
		Run: func(ctx context.Context, env Env) error {
			// The gateway logs a failed bind and carries on without serving.
			addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(env.GraphQLPort))
			probe, err := net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("--graphql-port: %w", err)
			}
			_ = probe.Close()

			// The schemas embed the kcp admin token, which kcp renews on
			// every start: a schema of the previous run makes the gateway
			// answer 503 until the listener happens to rewrite it. They are
			// regenerated within a second, so start from none.
			schemas := filepath.Join(env.DataDir, "graphql-schemas")
			if err := os.RemoveAll(schemas); err != nil {
				return err
			}
			if err := os.MkdirAll(schemas, 0o700); err != nil {
				return err
			}

			lo := listeneroptions.NewOptions()
			lo.Common.Kubeconfig = env.Kubeconfig
			lo.Common.Metrics.BindAddress = "0"
			lo.Common.HealthProbeBindAddress = "0"
			// "multi" is the workspaces of kcp plus one "single" cluster to
			// read ClusterAccess objects from: a Kubernetes cluster in an
			// installation, :root:platform-mesh-system here.
			single, err := kubeconfigFor(env.Config, env.ConfigFor(env.Platform.System).Host)
			if err != nil {
				return err
			}
			singleKubeconfig := filepath.Join(env.DataDir, "graphql-single.kubeconfig")
			if err := os.WriteFile(singleKubeconfig, single, 0o600); err != nil {
				return err
			}
			lo.Provider = "multi"
			lo.SingleKubeConfig = singleKubeconfig
			lo.EnableClusterAccessController = true
			lo.ProviderKcp.APIExportEndpointSliceName = coreExport
			lo.ProviderKcp.APIExportEndpointSliceLogicalCluster = env.Platform.System.String()
			// The gateway reaches the workspaces with the admin kubeconfig's
			// server and CA.
			lo.ProviderKcp.WorkspaceSchemaKubeconfigOverride = env.Kubeconfig
			// As the chart sets them: a schema per workspace with an APIBinding,
			// named after the workspace path.
			lo.ResourceGVR = "apibindings.v1alpha2.apis.kcp.io"
			lo.AnchorResource = "true"
			lo.AdditionalPathAnnotationKey = "kcp.io/path"
			lo.SchemaHandler = "file"
			lo.SchemasDir = schemas
			listenerOpts, err := lo.Complete()
			if err != nil {
				return fmt.Errorf("listener options: %w", err)
			}
			if err := listenerOpts.Validate(); err != nil {
				return fmt.Errorf("listener options: %w", err)
			}
			listenerCfg, err := listener.NewConfig(listenerOpts)
			if err != nil {
				return fmt.Errorf("listener config: %w", err)
			}
			listenerSrv, err := listener.NewServer(ctx, listenerCfg)
			if err != nil {
				return fmt.Errorf("listener: %w", err)
			}

			gwo := gatewayoptions.NewOptions()
			gwo.SchemaHandler = "file"
			gwo.SchemasDir = schemas
			gwo.ServerBindAddress = "127.0.0.1"
			gwo.ServerBindPort = env.GraphQLPort
			gwo.PlaygroundEnabled = true
			gatewayOpts, err := gwo.Complete()
			if err != nil {
				return fmt.Errorf("gateway options: %w", err)
			}
			if err := gatewayOpts.Validate(); err != nil {
				return fmt.Errorf("gateway options: %w", err)
			}
			gatewayCfg, err := gateway.NewConfig(gatewayOpts)
			if err != nil {
				return fmt.Errorf("gateway config: %w", err)
			}
			gatewaySrv, err := gateway.NewServer(gatewayCfg)
			if err != nil {
				return fmt.Errorf("gateway: %w", err)
			}

			g, ctx := errgroup.WithContext(ctx)
			g.Go(func() error {
				if err := listenerSrv.Run(ctx); err != nil {
					return fmt.Errorf("listener: %w", err)
				}
				<-ctx.Done()
				return nil
			})
			g.Go(func() error {
				if err := gatewaySrv.Run(ctx); err != nil {
					return fmt.Errorf("gateway: %w", err)
				}
				<-ctx.Done()
				return nil
			})
			return g.Wait()
		},
	}
}
