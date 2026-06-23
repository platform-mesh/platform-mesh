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
	"github.com/kcp-dev/client-go/dynamic"
	"github.com/kcp-dev/multicluster-provider/apiexport"
	"github.com/kcp-dev/virtual-workspace-framework/pkg/authorization"
	"github.com/spf13/cobra"

	"go.platform-mesh.io/virtual-workspaces/pkg/authentication"
	"go.platform-mesh.io/virtual-workspaces/pkg/contentconfiguration"
	"go.platform-mesh.io/virtual-workspaces/pkg/marketplace"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	"k8s.io/apiserver/pkg/authentication/request/union"
	genericapiserver "k8s.io/apiserver/pkg/server"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"

	extensionapiv1alpha1 "go.platform-mesh.io/apis/ui/v1alpha1"

	kcpclientset "github.com/kcp-dev/sdk/client/clientset/versioned/cluster"
	virtualrootapiserver "github.com/kcp-dev/virtual-workspace-framework/pkg/rootapiserver"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpapisv1alpha2.AddToScheme(scheme))
	utilruntime.Must(kcpcorev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha1.AddToScheme(scheme))
	utilruntime.Must(extensionapiv1alpha1.AddToScheme(scheme))
}

var startCmd = &cobra.Command{
	Use: "start",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctrl.SetLogger(klog.Background())
		codecs := serializer.NewCodecFactory(scheme)

		clientCfg, err := clientcmd.BuildConfigFromFlags("", cfg.Kubeconfig)
		if err != nil {
			return err
		}
		providerCfg := rest.CopyConfig(clientCfg)

		if cfg.ServerURL != "" {
			clientCfg.Host = cfg.ServerURL
		}

		clientCfg.QPS = -1 // Disable rate limiting for the client

		dynamicClient, err := dynamic.NewForConfig(clientCfg)
		if err != nil {
			return err
		}

		clusterClient, err := kcpclientset.NewForConfig(clientCfg)
		if err != nil {
			return err
		}

		recommendedConfig := genericapiserver.NewRecommendedConfig(codecs)

		err = secureServing.ApplyTo(&recommendedConfig.SecureServing)
		if err != nil {
			return err
		}

		err = delegatingAuthenticationOption.ApplyTo(&recommendedConfig.Authentication, recommendedConfig.SecureServing, recommendedConfig.OpenAPIConfig)
		if err != nil {
			return err
		}

		rootAPIServerConfig, err := virtualrootapiserver.NewConfig(recommendedConfig)
		if err != nil {
			return err
		}

		ctx := cmd.Context()

		marketplaceProvider, err := apiexport.New(providerCfg, cfg.ResourceAPIExportEndpointSliceName, apiexport.Options{
			Scheme: scheme,
		})
		if err != nil {
			return err
		}

		go func() {
			if err := marketplaceProvider.Start(ctx, nil); err != nil {
				klog.ErrorS(err, "apiexport provider stopped with error")
			}
		}()

		rootAPIServerConfig.Extra.VirtualWorkspaces = []virtualrootapiserver.NamedVirtualWorkspace{
			contentconfiguration.BuildVirtualWorkspace(ctx, cfg, dynamicClient, clusterClient, contentconfiguration.VirtualWorkspaceBaseURL()),
			marketplace.BuildVirtualWorkspace(ctx, cfg, dynamicClient, clusterClient, marketplace.VirtualWorkspaceBaseURL(), marketplaceProvider),
		}

		rootAPIServerConfig.Generic.Authentication.Authenticator = union.New(
			authentication.New(clientCfg),
			rootAPIServerConfig.Generic.Authentication.Authenticator,
		)

		rootAPIServerConfig.Generic.Authorization.Authorizer = authorization.NewVirtualWorkspaceAuthorizer(func() []virtualrootapiserver.NamedVirtualWorkspace {
			return rootAPIServerConfig.Extra.VirtualWorkspaces
		})

		completedRootAPIServerConfig := rootAPIServerConfig.Complete()
		rootAPIServer, err := virtualrootapiserver.NewServer(completedRootAPIServerConfig, genericapiserver.NewEmptyDelegate())
		if err != nil {
			return err
		}

		preparedRootAPIServer := rootAPIServer.GenericAPIServer.PrepareRun()
		if err := completedRootAPIServerConfig.WithOpenAPIAggregationController(preparedRootAPIServer.GenericAPIServer); err != nil {
			return err
		}

		return preparedRootAPIServer.RunWithContext(genericapiserver.SetupSignalContext())
	},
}
