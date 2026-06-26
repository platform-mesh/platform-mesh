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

package listener

import (
	"crypto/tls"
	"fmt"
	"net"
	"slices"
	"strings"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	pmgatewayv1alpha1 "go.platform-mesh.io/apis/gateway/v1alpha1"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/options"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/pkg/schemahandler"
	kcpprovider "go.platform-mesh.io/kubernetes-graphql-gateway/providers/kcp"
	"go.platform-mesh.io/kubernetes-graphql-gateway/sdk"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	ctrlcluster "sigs.k8s.io/controller-runtime/pkg/cluster"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	multiprovider "sigs.k8s.io/multicluster-runtime/providers/multi"
	singleprovider "sigs.k8s.io/multicluster-runtime/providers/single"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

type Config struct {
	Options *options.CompletedOptions

	Provider multicluster.Provider

	Manager mcmanager.Manager
	Scheme  *runtime.Scheme

	ClientConfig *rest.Config

	ReconcilerGVK schema.GroupVersionKind

	SchemaHandler schemahandler.Handler

	// ResourceReconcilerClusterMetadataFunc allows to provide cluster metadata for a given cluster name
	// when reconciling anchor namespaces.
	ResourceReconcilerClusterMetadataFunc func(clusterName string) (*pmgatewayv1alpha1.ClusterMetadata, error)

	// Per-controller builder options (provider filters). Nil means no filter (watch all providers).
	ResourceControllerForOptions      []mcbuilder.ForOption
	ClusterAccessControllerForOptions []mcbuilder.ForOption

	// singleCluster holds the cluster.Cluster for the single provider so it can
	// be added as a runnable to the manager (the single provider does not start it).
	singleCluster ctrlcluster.Cluster

	// grpcServer holds a reference to the gRPC server so it can be gracefully stopped.
	grpcServer *grpc.Server
}

// GracefulStop gracefully stops the gRPC server, if one was started.
func (c *Config) GracefulStop() {
	if c.grpcServer != nil {
		log.Info().Msg("Gracefully stopping gRPC server")
		c.grpcServer.GracefulStop()
	}
}

func NewConfig(options *options.CompletedOptions) (*Config, error) {
	config := &Config{
		Options: options,
	}

	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = options.Common.Kubeconfig

	var err error
	config.ClientConfig, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, nil).ClientConfig()
	if err != nil {
		return nil, err
	}

	config.ClientConfig = rest.CopyConfig(config.ClientConfig)
	config.ClientConfig = rest.AddUserAgent(config.ClientConfig, "kubernetes-graphql-gateway-listener")

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("error adding client-go scheme: %w", err)
	}
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("error adding apiextensions scheme: %w", err)
	}
	if err := pmgatewayv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("error adding kubebind scheme: %w", err)
	}

	config.Scheme = scheme

	var cacheNamespaces map[string]ctrlcache.Config
	for _, ns := range options.CacheNamespaces {
		if cacheNamespaces == nil {
			cacheNamespaces = make(map[string]ctrlcache.Config, len(options.CacheNamespaces))
		}
		cacheNamespaces[ns] = ctrlcache.Config{}
	}

	switch options.Provider {
	case "single":
		cl, err := ctrlcluster.New(config.ClientConfig, func(o *ctrlcluster.Options) {
			o.Scheme = scheme
			o.Cache.DefaultNamespaces = cacheNamespaces
		})
		if err != nil {
			return nil, fmt.Errorf("error creating cluster for single provider: %w", err)
		}
		config.Provider = singleprovider.New("single", cl)
		// The single provider does not start the cluster, so we need to
		// add it to the manager as a runnable to start the cache.
		config.singleCluster = cl

	case "kcp":
		if err := addKcpSchemes(scheme); err != nil {
			return nil, err
		}

		kcpClientCfg, err := options.ProviderKcp.ApplyLogicalClusterToConfig(config.ClientConfig)
		if err != nil {
			return nil, fmt.Errorf("error applying logical cluster to kcp client config: %w", err)
		}

		provider, err := kcpprovider.New(kcpClientCfg, options.ProviderKcp.APIExportEndpointSliceName, apiexport.Options{
			Scheme: scheme,
		})
		if err != nil {
			return nil, fmt.Errorf("error setting up kcp provider: %w", err)
		}

		config.Provider = provider
		config.ResourceReconcilerClusterMetadataFunc = options.ProviderKcp.GetClusterMetadataOverrideFunc()

	case "multi":
		if err := addKcpSchemes(scheme); err != nil {
			return nil, err
		}

		kcpClientCfg, err := options.ProviderKcp.ApplyLogicalClusterToConfig(config.ClientConfig)
		if err != nil {
			return nil, fmt.Errorf("error applying logical cluster to kcp client config: %w", err)
		}

		// Create kcp provider from main kubeconfig
		kcpProv, err := kcpprovider.New(kcpClientCfg, options.ProviderKcp.APIExportEndpointSliceName, apiexport.Options{
			Scheme: scheme,
		})
		if err != nil {
			return nil, fmt.Errorf("error setting up kcp provider: %w", err)
		}
		config.ResourceReconcilerClusterMetadataFunc = options.ProviderKcp.GetClusterMetadataOverrideFunc()

		// Create single provider from --single-kubeconfig
		singleConfig, err := clientcmd.BuildConfigFromFlags("", options.SingleKubeConfig)
		if err != nil {
			return nil, fmt.Errorf("error loading single-kubeconfig: %w", err)
		}
		singleConfig = rest.AddUserAgent(singleConfig, "kubernetes-graphql-gateway-listener")

		singleCluster, err := ctrlcluster.New(singleConfig, func(o *ctrlcluster.Options) {
			o.Scheme = scheme
			o.Cache.DefaultNamespaces = cacheNamespaces
		})
		if err != nil {
			return nil, fmt.Errorf("error creating cluster for single provider: %w", err)
		}
		singleProv := singleprovider.New("single", singleCluster)

		// Compose into multi provider
		multiProv := multiprovider.New(multiprovider.Options{})
		if err := multiProv.AddProvider("kcp", kcpProv); err != nil {
			return nil, fmt.Errorf("error adding kcp provider to multi provider: %w", err)
		}
		if err := multiProv.AddProvider("single", singleProv); err != nil {
			return nil, fmt.Errorf("error adding single provider to multi provider: %w", err)
		}
		config.Provider = multiProv
		// The single provider does not start its cluster.
		config.singleCluster = singleCluster

		// Build per-controller provider filters
		config.ResourceControllerForOptions = buildControllerForOptions(options.ResourceControllerProviders, "kcp")
		config.ClusterAccessControllerForOptions = buildControllerForOptions(options.ClusterAccessControllerProviders, "single")

	default:
		return nil, fmt.Errorf("unknown provider %q", options.Provider)
	}

	var tlsOpts []func(*tls.Config)
	if !options.Common.EnableHTTP2 {
		disableHTTP2 := func(c *tls.Config) {
			log.Info().Msg("disabling http/2")
			c.NextProtos = []string{"http/1.1"}
		}
		tlsOpts = []func(c *tls.Config){disableHTTP2}
	}

	opts := ctrl.Options{
		Controller: ctrlconfig.Controller{
			MaxConcurrentReconciles: options.Common.MaxConcurrentReconciles,
		},
		Cache: ctrlcache.Options{
			DefaultNamespaces: cacheNamespaces,
		},
		Metrics: metricsserver.Options{
			BindAddress:   options.Common.Metrics.BindAddress,
			SecureServing: options.Common.Metrics.Secure,
			TLSOpts:       tlsOpts,
		},
		Scheme:                  scheme,
		LeaderElection:          options.Common.LeaderElectionEnabled,
		LeaderElectionID:        "72231e1f.platform-mesh.io",
		HealthProbeBindAddress:  options.Common.HealthProbeBindAddress,
		GracefulShutdownTimeout: &options.Common.ShutdownTimeout,
	}
	if options.Common.Metrics.Secure {
		opts.Metrics.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	manager, err := mcmanager.New(config.ClientConfig, config.Provider, opts)
	if err != nil {
		return nil, fmt.Errorf("error setting up controller manager: %w", err)
	}

	config.Manager = manager

	// The single provider does not start its cluster, so we add it as a
	// runnable to the manager to ensure the cache is started.
	if config.singleCluster != nil {
		if err := manager.GetLocalManager().Add(config.singleCluster); err != nil {
			return nil, fmt.Errorf("error adding single cluster to manager: %w", err)
		}
	}

	switch options.SchemaHandler {
	case "file":
		config.SchemaHandler, err = schemahandler.NewFileHandler(options.SchemasDir)
		if err != nil {
			return nil, fmt.Errorf("error creating file handler: %w", err)
		}
	case "grpc":

		lis, err := net.Listen("tcp", options.GRPCListenAddr)
		if err != nil {
			return nil, fmt.Errorf("error creating gRPC listener: %w", err)
		}

		handler := schemahandler.NewGRPCHandler()

		srv := grpc.NewServer(grpc.MaxSendMsgSize(options.GRPCMaxSendMsgSize))
		sdk.RegisterSchemaHandlerServer(srv, handler)
		reflection.Register(srv)

		config.SchemaHandler = handler
		config.grpcServer = srv

		go func() {
			if err := srv.Serve(lis); err != nil {
				log.Error().Err(err).Msg("error serving gRPC")
			}
		}()
	}

	return config, nil
}

func addKcpSchemes(scheme *runtime.Scheme) error {
	if err := kcpapisv1alpha1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("error adding apis v1alpha1 scheme: %w", err)
	}
	if err := kcpapisv1alpha2.AddToScheme(scheme); err != nil {
		return fmt.Errorf("error adding apis v1alpha2 scheme: %w", err)
	}
	if err := kcpcorev1alpha1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("error adding core scheme: %w", err)
	}
	if err := kcptenancyv1alpha1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("error adding tenancy scheme: %w", err)
	}
	return nil
}

// buildControllerForOptions builds a ForOption slice that filters a controller to the given providers.
// If names is empty, defaultNames is used.
// The multi.Provider prefixes cluster names as "providerName#clusterName", so the
// filter matches on the provider prefix to route clusters to the correct controller.
func buildControllerForOptions(names string, defaultNames string) []mcbuilder.ForOption {
	if names == "" {
		names = defaultNames
	}

	var allowed []string
	for name := range strings.SplitSeq(names, ",") {
		if n := strings.TrimSpace(name); n != "" {
			allowed = append(allowed, n)
		}
	}

	if len(allowed) == 0 {
		return nil
	}

	return []mcbuilder.ForOption{
		mcbuilder.WithClusterFilter(func(clusterName multicluster.ClusterName, _ ctrlcluster.Cluster) bool {
			prefix, _, ok := strings.Cut(string(clusterName), "#")
			if !ok {
				return false
			}
			return slices.Contains(allowed, prefix)
		}),
	}
}
