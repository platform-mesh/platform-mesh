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

package config

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

type GroupVersionKindProvider struct {
	schema.GroupVersionKind
	Provider string
}

func (g *GroupVersionKindProvider) Unmarshal(s string) error {
	s = strings.Trim(s, "{}")
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return fmt.Errorf("expected 'group,version,kind,provider', got %q", s)
	}
	g.Group = parts[0]
	g.Version = parts[1]
	g.Kind = parts[2]
	g.Provider = parts[3]
	return nil
}

type OperatorConfig struct {
	KCPKubeconfig              string
	APIExportEndpointSliceName string
	SearchableResources        []GroupVersionKindProvider
	OpenSearchIndexNamePrefix  string
	OpenSearchSemanticModelID  string
}

func NewOperatorConfig() OperatorConfig {
	return OperatorConfig{
		KCPKubeconfig:              "/api-kubeconfig/kubeconfig",
		APIExportEndpointSliceName: "search.platform-mesh.io",
		OpenSearchIndexNamePrefix:  "pm-orgs",
	}
}

func (c *OperatorConfig) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.KCPKubeconfig, "kcp-kubeconfig", c.KCPKubeconfig, "Path to the kcp kubeconfig file")
	fs.StringVar(&c.APIExportEndpointSliceName, "api-export-endpoint-slice-name", c.APIExportEndpointSliceName, "Name of the APIExportEndpointSlice to use for the multicluster provider")
	fs.StringVar(&c.OpenSearchIndexNamePrefix, "opensearch-index-name-prefix", c.OpenSearchIndexNamePrefix, "Static prefix for all operator-managed OpenSearch index names and aliases")
	fs.StringVar(&c.OpenSearchSemanticModelID, "opensearch-semantic-model-id", c.OpenSearchSemanticModelID, "OpenSearch ML model ID used for semantic field mappings (optional)")
}
