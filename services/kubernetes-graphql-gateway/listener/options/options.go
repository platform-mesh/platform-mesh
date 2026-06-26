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

package options

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"

	pmgatewayv1alpha1 "go.platform-mesh.io/apis/gateway/v1alpha1"
	commonsconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/kubernetes-graphql-gateway/defaults"
	providerkcp "go.platform-mesh.io/kubernetes-graphql-gateway/providers/kcp/options"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/component-base/logs"
	logsv1 "k8s.io/component-base/logs/api/v1"
)

type Options struct {
	Logs *logs.Options

	Common *commonsconfig.CommonServiceConfig

	ProviderKcp *providerkcp.Options

	ExtraOptions
}

type ExtraOptions struct {
	// Multicluster runtime provider
	Provider string
	// SingleKubeConfig is the path to a kubeconfig for the single provider cluster.
	// Only required when provider is "multi".
	SingleKubeConfig string
	// ResourceControllerProviders is a comma-separated list of provider names (kcp, single)
	// that the resource controller should watch. Only valid when provider is "multi".
	ResourceControllerProviders string
	// ClusterAccessControllerProviders is a comma-separated list of provider names (kcp, single)
	// that the clusteraccess controller should watch. Only valid when provider is "multi".
	ClusterAccessControllerProviders string
	// SchemasDir is the directory to store schema files. Only required if using file schema handler
	SchemasDir string
	// ResourceGVR is the GroupVersionResource which the reconciler will be watching
	ResourceGVR string
	// AnchorResource is the resource to watch for kubernetes provider
	// When a resource with this name exists, the controller will generate schema for the cluster
	AnchorResource string
	// ClusterMetadataFunc allows to provide cluster metadata for a given cluster name
	// when reconciling anchor namespaces.
	ClusterMetadataFunc pmgatewayv1alpha1.ClusterMetadataFunc
	// ClusterURLResolverFunc allows to provide cluster URL for a given cluster name
	ClusterURLResolverFunc pmgatewayv1alpha1.ClusterURLResolver
	// SchemaHandler is the type of schema handler to use (e.g., "file", "grpc")
	SchemaHandler string
	// GRPCListenAddr is the gRPC server listener address (only used if SchemaHandler is "grpc")
	GRPCListenAddr string
	// GRPCMaxSendMsgSize is the maximum gRPC message size in bytes the server will send.
	GRPCMaxSendMsgSize int

	AdditonalPathAnnotationKey string

	// CacheNamespaces restricts the cache to these namespaces for namespaced resources.
	// Cluster-scoped resources are unaffected.
	CacheNamespaces []string

	// EnableResourceController enables the resource controller.
	EnableResourceController bool
	// EnableClusterAccessController enables the ClusterAccess controller.
	EnableClusterAccessController bool
}

type completedOptions struct {
	Logs *logs.Options

	Common *commonsconfig.CommonServiceConfig

	// Provider specific options
	ProviderKcp *providerkcp.CompletedOptions

	ExtraOptions
}

type CompletedOptions struct {
	*completedOptions
}

func NewOptions() *Options {
	// Default to -v=2
	logs := logs.NewOptions()
	logs.Verbosity = logsv1.VerbosityLevel(2)

	opts := &Options{
		Logs:        logs,
		Common:      commonsconfig.NewDefaultConfig(),
		ProviderKcp: providerkcp.NewOptions(),

		ExtraOptions: ExtraOptions{
			Provider:                 "single",
			SchemaHandler:            "file",
			SchemasDir:               "_output/schemas",
			GRPCListenAddr:           ":50051",
			GRPCMaxSendMsgSize:       defaults.DefaultGRPCMaxMsgSize,
			AnchorResource:           "object.metadata.name == 'default'",
			ResourceGVR:              "namespaces.v1",
			EnableResourceController: true,
			ClusterURLResolverFunc:   pmgatewayv1alpha1.DefaultClusterURLResolverFunc,
		},
	}
	return opts
}

var providerAliases = map[string]string{
	"kcp":    "kcp",
	"single": "single",
	"multi":  "multi",
}

func (options *Options) AddFlags(fs *pflag.FlagSet) {
	logsv1.AddFlags(options.Logs, fs)
	options.Common.AddFlags(fs)
	options.ProviderKcp.AddFlags(fs)

	// Hide commons log flags — we use k8s.io/component-base/logs instead.
	_ = fs.MarkHidden("log-level")
	_ = fs.MarkHidden("no-json")

	fs.StringVar(&options.Provider, "multicluster-runtime-provider", options.Provider,
		fmt.Sprintf("The multicluster runtime provider. Possible values are: %v", sets.List(sets.Set[string](sets.StringKeySet(providerAliases)))),
	)

	fs.StringVar(&options.SingleKubeConfig, "single-kubeconfig", options.SingleKubeConfig, "path to a kubeconfig for the single provider cluster. Only required when provider is 'multi'")
	fs.StringVar(&options.ResourceControllerProviders, "resource-controller-providers", options.ResourceControllerProviders, "comma-separated list of provider names (kcp, single) that the resource controller should watch. Only valid when provider is 'multi'. Default: kcp")
	fs.StringVar(&options.ClusterAccessControllerProviders, "clusteraccess-controller-providers", options.ClusterAccessControllerProviders, "comma-separated list of provider names (kcp, single) that the clusteraccess controller should watch. Only valid when provider is 'multi'. Default: single")

	fs.StringVar(&options.SchemaHandler, "schema-handler", options.SchemaHandler, "The type of schema handler to use (e.g., 'file', 'grpc')")
	fs.StringVar(&options.SchemasDir, "schemas-dir", options.SchemasDir, "SchemasDir is the directory to store schema files. Only required if using file schema handler")
	fs.StringVar(&options.GRPCListenAddr, "grpc-listen-addr", options.GRPCListenAddr, "The gRPC server listener address (only used if SchemaHandler is 'grpc')")
	fs.IntVar(&options.GRPCMaxSendMsgSize, "grpc-max-send-msg-size", options.GRPCMaxSendMsgSize, "maximum gRPC send message size in bytes (used with --schema-handler=grpc)")

	fs.StringVar(&options.AnchorResource, "anchor-resource", options.AnchorResource, "Resource to watch as anchor for kubernetes provider (default: default)")
	fs.StringVar(&options.ResourceGVR, "reconciler-gvr", options.ResourceGVR, "The GroupVersionResource which the reconciler will be watching (default: namespaces.v1)")

	fs.StringVar(&options.AdditonalPathAnnotationKey, "additional-path-annotation-key", options.AdditonalPathAnnotationKey, "additional path annotation key for workspace schema generation")

	fs.StringSliceVar(&options.CacheNamespaces, "cache-namespaces", options.CacheNamespaces, "Namespaces to restrict the cache to for namespaced resources (e.g. secrets, configmaps). Cluster-scoped resources are unaffected. When empty, all namespaces are cached")

	fs.BoolVar(&options.EnableResourceController, "enable-resource-controller", options.EnableResourceController, "Enable the resource controller for watching the configured anchor resource and generating schemas")
	fs.BoolVar(&options.EnableClusterAccessController, "enable-clusteraccess-controller", options.EnableClusterAccessController, "Enable the ClusterAccess controller for managing remote cluster schemas")
}

func (options *Options) Complete() (*CompletedOptions, error) {
	co := &CompletedOptions{
		completedOptions: &completedOptions{
			Logs:         options.Logs,
			Common:       options.Common,
			ExtraOptions: options.ExtraOptions,
		},
	}

	if options.Provider == "kcp" || options.Provider == "multi" {
		opts, err := options.ProviderKcp.Complete()
		if err != nil {
			return nil, err
		}
		co.ProviderKcp = opts
		co.ClusterMetadataFunc = opts.GetClusterMetadataOverrideFunc()
		co.ClusterURLResolverFunc = opts.GetClusterURLResolverFunc()
	}
	return co, nil
}

var validControllerProviderNames = sets.New("kcp", "single")

func (options *CompletedOptions) Validate() error {
	provider := providerAliases[options.Provider]
	if provider == "" {
		return fmt.Errorf("unknown provider %q, must be one of %v", options.Provider, sets.List(sets.Set[string](sets.StringKeySet(providerAliases))))
	}
	options.Provider = provider

	// Multi-mode specific validation
	if provider == "multi" {
		if options.Common.Kubeconfig == "" {
			return fmt.Errorf("--kubeconfig is required when provider is 'multi' (used for kcp endpoint)")
		}
		if options.SingleKubeConfig == "" {
			return fmt.Errorf("--single-kubeconfig is required when provider is 'multi'")
		}
	} else {
		if options.SingleKubeConfig != "" {
			return fmt.Errorf("--single-kubeconfig is only valid with --multicluster-runtime-provider=multi")
		}
		if options.ResourceControllerProviders != "" {
			return fmt.Errorf("--resource-controller-providers is only valid with --multicluster-runtime-provider=multi")
		}
		if options.ClusterAccessControllerProviders != "" {
			return fmt.Errorf("--clusteraccess-controller-providers is only valid with --multicluster-runtime-provider=multi")
		}
	}

	// Validate per-controller provider names
	if options.ResourceControllerProviders != "" {
		if !options.EnableResourceController {
			return fmt.Errorf("--resource-controller-providers requires --enable-resource-controller")
		}
		if err := validateProviderNames(options.ResourceControllerProviders, "--resource-controller-providers"); err != nil {
			return err
		}
	}
	if options.ClusterAccessControllerProviders != "" {
		if !options.EnableClusterAccessController {
			return fmt.Errorf("--clusteraccess-controller-providers requires --enable-clusteraccess-controller")
		}
		if err := validateProviderNames(options.ClusterAccessControllerProviders, "--clusteraccess-controller-providers"); err != nil {
			return err
		}
	}

	gvr, gv := schema.ParseResourceArg(options.ResourceGVR)
	if gvr == nil && gv.Empty() {
		return fmt.Errorf("invalid reconciler-gvr %q", options.ResourceGVR)
	}

	if options.SchemaHandler == "grpc" {
		if options.GRPCListenAddr == "" {
			return fmt.Errorf("grpc-listen-addr must be specified when schema-handler is 'grpc'")
		}
		if options.GRPCMaxSendMsgSize <= 0 {
			return fmt.Errorf("--grpc-max-send-msg-size must be a positive value")
		}
	}

	if options.SchemaHandler == "file" {
		if options.SchemasDir == "" {
			return fmt.Errorf("schemas-dir must be specified when schema-handler is 'file'")
		}
	}

	for _, ns := range options.CacheNamespaces {
		if strings.TrimSpace(ns) == "" {
			return fmt.Errorf("empty namespace in --cache-namespaces")
		}
	}

	return nil
}

// validateProviderNames checks that a comma-separated string contains only valid provider names.
func validateProviderNames(names string, flagName string) error {
	for name := range strings.SplitSeq(names, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("empty provider name in %s", flagName)
		}
		if !validControllerProviderNames.Has(name) {
			return fmt.Errorf("invalid provider name %q in %s, must be one of %v", name, flagName, sets.List(validControllerProviderNames))
		}
	}
	return nil
}
