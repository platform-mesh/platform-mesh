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

// Package server runs the kcp virtual workspace API server of this binary:
// Options are its flags, Run serves the virtual workspaces it is handed.
package server

import (
	"context"

	"github.com/spf13/pflag"

	"go.platform-mesh.io/golang-commons/virtual-workspaces/authentication"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apiserver/pkg/authentication/request/union"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericapiserveroptions "k8s.io/apiserver/pkg/server/options"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/kcp-dev/client-go/dynamic"
	kcpclientset "github.com/kcp-dev/sdk/client/clientset/versioned/cluster"
	"github.com/kcp-dev/virtual-workspace-framework/pkg/authorization"
	virtualrootapiserver "github.com/kcp-dev/virtual-workspace-framework/pkg/rootapiserver"
)

// Options are the flags every virtual workspace server has.
type Options struct {
	// Kubeconfig is the kubeconfig the server reaches kcp with.
	Kubeconfig string
	// ServerURL replaces the kubeconfig's server for the cluster-aware clients:
	// the root of the kcp instance (not a cluster in it).
	ServerURL string

	SecureServing  genericapiserveroptions.SecureServingOptions
	Authentication genericapiserveroptions.DelegatingAuthenticationOptions
}

// NewOptions returns the Options with their defaults.
func NewOptions() *Options {
	return &Options{
		Authentication: *genericapiserveroptions.NewDelegatingAuthenticationOptions(),
	}
}

// AddFlags registers the flags of o.
func (o *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Kubeconfig, "kubeconfig", o.Kubeconfig, "Set the kubeconfig file path")
	fs.StringVar(&o.ServerURL, "server-url", o.ServerURL, "Set the server URL")
	o.Authentication.AddFlags(fs)
	o.SecureServing.AddFlags(fs)
}

// Clients are the kcp clients a virtual workspace is built from.
type Clients struct {
	// ProviderConfig is the kubeconfig as loaded, for clients that address
	// what the kubeconfig points at (an APIExport provider).
	ProviderConfig *rest.Config
	// Dynamic and Cluster address any logical cluster through Options.ServerURL.
	Dynamic dynamic.ClusterInterface
	Cluster kcpclientset.ClusterInterface
}

// BuildFunc returns the virtual workspaces a server serves.
type BuildFunc func(ctx context.Context, clients Clients) ([]virtualrootapiserver.NamedVirtualWorkspace, error)

// Run serves the virtual workspaces returned by build until the process is
// signalled. ctx bounds what build starts in the background.
func Run(ctx context.Context, o *Options, scheme *runtime.Scheme, build BuildFunc) error {
	ctrl.SetLogger(klog.Background())
	codecs := serializer.NewCodecFactory(scheme)

	clientCfg, err := clientcmd.BuildConfigFromFlags("", o.Kubeconfig)
	if err != nil {
		return err
	}
	providerCfg := rest.CopyConfig(clientCfg)

	if o.ServerURL != "" {
		clientCfg.Host = o.ServerURL
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

	err = o.SecureServing.ApplyTo(&recommendedConfig.SecureServing)
	if err != nil {
		return err
	}

	err = o.Authentication.ApplyTo(&recommendedConfig.Authentication, recommendedConfig.SecureServing, recommendedConfig.OpenAPIConfig)
	if err != nil {
		return err
	}

	rootAPIServerConfig, err := virtualrootapiserver.NewConfig(recommendedConfig)
	if err != nil {
		return err
	}

	rootAPIServerConfig.Extra.VirtualWorkspaces, err = build(ctx, Clients{
		ProviderConfig: providerCfg,
		Dynamic:        dynamicClient,
		Cluster:        clusterClient,
	})
	if err != nil {
		return err
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
}
