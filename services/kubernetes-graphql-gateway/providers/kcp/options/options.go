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
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/spf13/pflag"

	pmgatewayv1alpha1 "go.platform-mesh.io/apis/gateway/v1alpha1"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Options struct {
	ExtraOptions
}

type ExtraOptions struct {
	// APIExportEndpointSliceName is the name of the APIExport EndpointSlice to watch.
	APIExportEndpointSliceName string
	// APIExportEndpointSliceLogicalCluster is the logical cluster path where the
	// APIExportEndpointSlice lives, e.g. "root:providers". When set, the listener
	// rest config host is rewritten to point to that logical cluster so the
	// EndpointSlice is fetched from the correct workspace regardless of what the
	// kubeconfig's current context points to. Leave empty to use the kubeconfig
	// current context.
	APIExportEndpointSliceLogicalCluster string
	// WorkspaceSchemaHostOverride is the host override for workspace schema generation.
	WorkspaceSchemaHostOverride string
	// WorkspaceSchemaKubeconfigOverride is the kubeconfig override for workspace schema generation.
	// If set together with WorkspaceSchemaHostOverride, WorkspaceSchemaHostOverride will take precedence.
	WorkspaceSchemaKubeconfigOverride string
	// WorkspaceSchemaKubeconfigRestConfig is the rest config built from WorkspaceSchemaKubeconfigOverride
	WorkspaceSchemaKubeconfigRestConfig *rest.Config
}

type completedOptions struct {
	ExtraOptions
}

type CompletedOptions struct {
	*completedOptions
}

func NewOptions() *Options {
	return &Options{
		ExtraOptions: ExtraOptions{
			APIExportEndpointSliceName: "graphql-gateway-apiexports",
		},
	}
}

func (options *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&options.APIExportEndpointSliceName, "apiexport-endpoint-slice-name", options.APIExportEndpointSliceName, "name of the APIExport EndpointSlice to watch")
	fs.StringVar(&options.APIExportEndpointSliceLogicalCluster, "apiexport-endpoint-slice-logicalcluster", options.APIExportEndpointSliceLogicalCluster, "logical cluster path where the APIExportEndpointSlice lives, e.g. root:providers. When set, overrides the kubeconfig current-context workspace.")
	fs.StringVar(&options.WorkspaceSchemaHostOverride, "workspace-schema-host-override", options.WorkspaceSchemaHostOverride, "host override for workspace schema generation")
	fs.StringVar(&options.WorkspaceSchemaKubeconfigOverride, "workspace-schema-kubeconfig-override", options.WorkspaceSchemaKubeconfigOverride, "kubeconfig override for workspace schema generation. If set together with --workspace-schema-host-override, the host override will take precedence.")
}

func (options *Options) Complete() (*CompletedOptions, error) {
	if options.WorkspaceSchemaKubeconfigOverride != "" {
		// Load the kubeconfig and build rest config
		config, err := clientcmd.BuildConfigFromFlags("", options.WorkspaceSchemaKubeconfigOverride)
		if err != nil {
			return nil, fmt.Errorf("failed to build rest config from kubeconfig: %w", err)
		}

		options.WorkspaceSchemaKubeconfigRestConfig = config
	}

	return &CompletedOptions{
		completedOptions: &completedOptions{
			ExtraOptions: options.ExtraOptions,
		},
	}, nil
}

func (options *CompletedOptions) Validate() error {
	if options.WorkspaceSchemaKubeconfigOverride != "" {
		// Check if kubeconfig file exists
		if _, err := os.Stat(options.WorkspaceSchemaKubeconfigOverride); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("kubeconfig file does not exist: %s", options.WorkspaceSchemaKubeconfigOverride)
			}
			return fmt.Errorf("failed to access kubeconfig file: %w", err)
		}
	}

	return nil
}

// ApplyLogicalClusterToConfig returns a copy of cfg with Host rewritten to
// point at the logical cluster path configured via
// APIExportEndpointSliceLogicalCluster. If the field is empty, cfg is returned
// unchanged.
func (options *CompletedOptions) ApplyLogicalClusterToConfig(cfg *rest.Config) (*rest.Config, error) {
	if options.APIExportEndpointSliceLogicalCluster == "" {
		return cfg, nil
	}
	parsed, err := url.Parse(cfg.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to parse rest config host %q: %w", cfg.Host, err)
	}
	parsed.Path = path.Join("clusters", options.APIExportEndpointSliceLogicalCluster)
	out := rest.CopyConfig(cfg)
	out.Host = parsed.String()
	return out, nil
}

func (options *CompletedOptions) GetClusterMetadataOverrideFunc() pmgatewayv1alpha1.ClusterMetadataFunc {
	return func(clusterName string) (*pmgatewayv1alpha1.ClusterMetadata, error) {
		if options.WorkspaceSchemaKubeconfigRestConfig != nil {
			metadata, err := pmgatewayv1alpha1.BuildClusterMetadataFromConfig(options.WorkspaceSchemaKubeconfigRestConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to build metadata from rest config: %w", err)
			}

			parsed, err := url.Parse(options.WorkspaceSchemaKubeconfigRestConfig.Host)
			if err != nil {
				return nil, fmt.Errorf("failed to parse host from rest config: %w", err)
			}
			parsed.Path = path.Join("clusters", clusterName)
			metadata.Host = parsed.String()

			return metadata, nil
		}

		metadata := &pmgatewayv1alpha1.ClusterMetadata{}
		if options.WorkspaceSchemaHostOverride != "" {
			metadata.Host = options.WorkspaceSchemaHostOverride
		}
		return metadata, nil
	}
}

func (options *CompletedOptions) GetClusterURLResolverFunc() pmgatewayv1alpha1.ClusterURLResolver {
	return func(currentURL string, clusterName string) (string, error) {
		if options.WorkspaceSchemaHostOverride != "" {
			return options.WorkspaceSchemaHostOverride, nil
		}
		if options.WorkspaceSchemaKubeconfigRestConfig != nil {
			parsed, err := url.Parse(options.WorkspaceSchemaKubeconfigRestConfig.Host)
			if err != nil {
				return "", fmt.Errorf("failed to parse host from kubeconfig override: %w", err)
			}
			parsed.Path = path.Join("clusters", clusterName)
			return parsed.String(), nil
		}
		parts := strings.Split(currentURL, "/services/")
		if len(parts) != 2 {
			return "", fmt.Errorf("invalid current URL format: %s", currentURL)
		}
		newURL := fmt.Sprintf("%s/clusters/%s", parts[0], clusterName)
		return newURL, nil
	}
}
