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
	"io/fs"
	"net"
	"path/filepath"
	"strings"

	uiresources "go.platform-mesh.io/apis/static-assets/resources/extension-manager-operator"
	pmuiv1alpha1 "go.platform-mesh.io/apis/ui/v1alpha1"
	contentconfig "go.platform-mesh.io/extension-manager-operator/virtual-workspace/pkg/config"
	"go.platform-mesh.io/extension-manager-operator/virtual-workspace/pkg/contentconfiguration"
	"go.platform-mesh.io/golang-commons/virtual-workspaces/server"
	marketplaceconfig "go.platform-mesh.io/marketplace/pkg/config"
	"go.platform-mesh.io/marketplace/pkg/marketplace"
	"go.platform-mesh.io/platform-mesh/cmd/pm/pkg/certs"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	virtualrootapiserver "github.com/kcp-dev/virtual-workspace-framework/pkg/rootapiserver"
)

// VirtualWorkspacesName is the name of the VirtualWorkspaces component.
const VirtualWorkspacesName = "virtual-workspaces"

// VirtualWorkspacePaths are the kcp paths the virtual workspaces are
// reached at: kcp forwards them to the server of VirtualWorkspaces.
var VirtualWorkspacePaths = []string{
	contentconfiguration.VirtualWorkspaceBaseURL(),
	marketplace.VirtualWorkspaceBaseURL(),
}

// marketplaceAccess is the name of the ClusterAccess, and of its Secret,
// that makes the GraphQL gateway serve the marketplace virtual workspace.
const marketplaceAccess = "marketplace"

// VirtualWorkspaces serves the Platform Mesh virtual workspaces from one API
// server on localhost. In a cluster each is a Deployment behind a path of
// kcp's front-proxy; here kcp's own built-in proxy forwards the paths.
//
// A request carries the caller's bearer token, which the server checks
// against kcp: there is no front-proxy to vouch for the caller.
func VirtualWorkspaces() Component {
	return Component{
		Name: VirtualWorkspacesName,
		// The marketplace workspace reads what providers offer through this
		// export, and the gateway is told about it with a ClusterAccess.
		Bindings: []string{uiExport, gatewayExport},
		Objects: func(cfg *rest.Config) ([]*unstructured.Unstructured, error) {
			kubeconfig, err := kubeconfigFor(cfg, cfg.Host+marketplace.VirtualWorkspaceBaseURL())
			if err != nil {
				return nil, err
			}
			const namespace = "default"
			return []*unstructured.Unstructured{
				{Object: map[string]any{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata":   map[string]any{"name": marketplaceAccess, "namespace": namespace},
					"stringData": map[string]any{"kubeconfig": string(kubeconfig)},
				}},
				{Object: map[string]any{
					"apiVersion": "gateway.platform-mesh.io/v1alpha1",
					"kind":       "ClusterAccess",
					"metadata":   map[string]any{"name": marketplaceAccess},
					"spec": map[string]any{
						"introspectionPath":   "/clusters/*",
						"requestPathTemplate": "/clusters/{clusterTarget}",
						"auth": map[string]any{
							"kubeconfigSecretRef": map[string]any{"name": marketplaceAccess, "namespace": namespace, "key": "kubeconfig"},
						},
					},
				}},
			}, nil
		},
		Run: func(ctx context.Context, env Env) error {
			dir := filepath.Join(env.DataDir, "virtual-workspaces")
			if _, _, err := certs.Ensure(dir); err != nil {
				return fmt.Errorf("serving certificate: %w", err)
			}
			schema, err := schemaName(uiresources.Resources, "contentconfigurations")
			if err != nil {
				return err
			}

			o := server.NewOptions()
			o.Kubeconfig = env.Kubeconfig
			o.ServerURL = env.Config.Host
			o.SecureServing.BindAddress = net.IPv4(127, 0, 0, 1)
			o.SecureServing.BindPort = env.VirtualWorkspacesPort
			o.SecureServing.ServerCert.CertKey.CertFile = filepath.Join(dir, "tls.crt")
			o.SecureServing.ServerCert.CertKey.KeyFile = filepath.Join(dir, "tls.key")
			// No cluster to delegate authentication to.
			o.Authentication.RemoteKubeConfigFileOptional = true
			o.Authentication.SkipInClusterLookup = true

			cfg := contentconfig.New()
			cfg.EntityLabel = "ui.platform-mesh.io/entity"
			cfg.ResourceSchemaWorkspace = env.Platform.System.String()
			cfg.ResourceSchemaName = schema

			scheme := runtime.NewScheme()
			for _, add := range []func(*runtime.Scheme) error{
				kcpapisv1alpha1.AddToScheme, kcpapisv1alpha2.AddToScheme, kcpcorev1alpha1.AddToScheme,
				kcptenancyv1alpha1.AddToScheme, pmuiv1alpha1.AddToScheme,
			} {
				if err := add(scheme); err != nil {
					return err
				}
			}
			marketplaceCfg := marketplaceconfig.New()
			marketplaceCfg.APIExportEndpointSliceName = uiExport

			return server.Serve(ctx, o, scheme, func(ctx context.Context, clients server.Clients) ([]virtualrootapiserver.NamedVirtualWorkspace, error) {
				// ProviderMetadata and APIExports of every provider.
				provider, err := apiexport.New(env.ConfigFor(env.Platform.System), marketplaceCfg.APIExportEndpointSliceName, apiexport.Options{Scheme: scheme})
				if err != nil {
					return nil, fmt.Errorf("marketplace: creating APIExport provider: %w", err)
				}
				go func() {
					if err := provider.Start(ctx, nil); err != nil && ctx.Err() == nil {
						klog.FromContext(ctx).Error(err, "marketplace: APIExport provider stopped")
					}
				}()
				return []virtualrootapiserver.NamedVirtualWorkspace{
					contentconfiguration.BuildVirtualWorkspace(ctx, cfg, clients.Dynamic, clients.Cluster, contentconfiguration.VirtualWorkspaceBaseURL()),
					marketplace.BuildVirtualWorkspace(ctx, marketplaceCfg, clients.Dynamic, clients.Cluster, marketplace.VirtualWorkspaceBaseURL(), provider),
				}, nil
			})
		},
	}
}

// schemaName returns the name of the APIResourceSchema of resource among the
// manifests in fsys.
func schemaName(fsys fs.FS, resource string) (string, error) {
	files, err := fs.Glob(fsys, "apiresourceschema-"+resource+".*.yaml")
	if err != nil {
		return "", err
	}
	if len(files) != 1 {
		return "", fmt.Errorf("want one APIResourceSchema of %s, found %d", resource, len(files))
	}
	raw, err := fs.ReadFile(fsys, files[0])
	if err != nil {
		return "", err
	}
	var obj struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	}
	if err := yaml.Unmarshal(raw, &obj); err != nil {
		return "", fmt.Errorf("%s: %w", files[0], err)
	}
	if !strings.HasSuffix(obj.Metadata.Name, "."+resource+".ui.platform-mesh.io") {
		return "", fmt.Errorf("%s: unexpected schema name %q", files[0], obj.Metadata.Name)
	}
	return obj.Metadata.Name, nil
}
