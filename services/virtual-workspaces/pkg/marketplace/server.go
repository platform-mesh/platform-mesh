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

package marketplace

import (
	"context"
	"path"

	"github.com/kcp-dev/client-go/dynamic"
	"github.com/kcp-dev/multicluster-provider/apiexport"
	apisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpclientset "github.com/kcp-dev/sdk/client/clientset/versioned/cluster"
	"github.com/kcp-dev/virtual-workspace-framework/framework"
	virtualworkspacesdynamic "github.com/kcp-dev/virtual-workspace-framework/pkg/dynamic"
	kcpapidefinition "github.com/kcp-dev/virtual-workspace-framework/pkg/dynamic/apidefinition"
	virtualrootapiserver "github.com/kcp-dev/virtual-workspace-framework/pkg/rootapiserver"
	"go.platform-mesh.io/virtual-workspaces/config/resources"
	"go.platform-mesh.io/virtual-workspaces/pkg/apidefinition"
	"go.platform-mesh.io/virtual-workspaces/pkg/authorization"
	"go.platform-mesh.io/virtual-workspaces/pkg/config"
	vwspath "go.platform-mesh.io/virtual-workspaces/pkg/path"
	"go.platform-mesh.io/virtual-workspaces/pkg/proxy"
	"go.platform-mesh.io/virtual-workspaces/pkg/storage"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/apiserver/pkg/authorization/authorizer"

	genericapiserver "k8s.io/apiserver/pkg/server"
)

var Name = "marketplace"

func VirtualWorkspaceBaseURL() string {
	return path.Join("/services", "marketplace")
}

func BuildVirtualWorkspace(
	ctx context.Context,
	cfg config.ServiceConfig,
	dynamicClient dynamic.ClusterInterface,
	kcpClusterClient kcpclientset.ClusterInterface,
	virtualWorkspaceBaseURL string,
	provider *apiexport.Provider,
) virtualrootapiserver.NamedVirtualWorkspace {

	clusterResolver := proxy.NewClusterResolver(kcpClusterClient)

	return virtualrootapiserver.NamedVirtualWorkspace{
		Name: Name,
		VirtualWorkspace: &virtualworkspacesdynamic.DynamicVirtualWorkspace{
			RootPathResolver: vwspath.NewPathResolver(clusterResolver, virtualWorkspaceBaseURL),
			Authorizer: authorization.NewAttributesKeeper(
				authorizer.AuthorizerFunc(func(ctx context.Context, a authorizer.Attributes) (authorizer.Decision, string, error) {
					isAuthenticated := sets.New(a.GetUser().GetGroups()...).Has("system:authenticated")
					if isAuthenticated {
						return authorizer.DecisionAllow, "user is authenticated", nil
					}

					return authorizer.DecisionDeny, "user is not authenticated", nil
				}), // TODO: we can think of a bit more complex authorization logic, e.g. doing some SAR, for now it is better than nothing
			),
			ReadyChecker: framework.ReadyFunc(func() error { return nil }),
			BootstrapAPISetManagement: func(mainConfig genericapiserver.CompletedConfig) (kcpapidefinition.APIDefinitionSetGetter, error) {

				var resourceSchema apisv1alpha1.APIResourceSchema
				err := yaml.Unmarshal([]byte(resources.ResourceSchema), &resourceSchema)
				if err != nil {
					return nil, err
				}

				marketplaceFilter := storage.Marketplace(provider, cfg)

				storeageProvider := storage.CreateStorageProviderFunc(
					dynamicClient,
					marketplaceFilter,
				)

				gvr := schema.GroupVersionResource{
					Group:    resourceSchema.Spec.Group,
					Version:  resourceSchema.Spec.Versions[0].Name,
					Resource: resourceSchema.Spec.Names.Plural,
				}

				return apidefinition.NewSingleResourceProvider(mainConfig, gvr, &resourceSchema, storeageProvider), nil
			},
		},
	}
}
