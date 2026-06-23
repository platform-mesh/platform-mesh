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
	"github.com/vrischmann/envconfig"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Config holds the configuration for the search-operator
type Config struct {
	KCP struct {
		// Kubeconfig is the path to the kcp kubeconfig file
		Kubeconfig string `mapstructure:"kcp-kubeconfig" envconfig:"default=/api-kubeconfig/kubeconfig"`
	} `mapstructure:",squash"`

	SearchableResource struct {
		Resources []schema.GroupVersionKind `mapstructure:"resources" envconfig:"default={core.platform-mesh.io;v1alpha1;Account}"`
	} `mapstructure:",squash"`

	OpenSearch struct {
		// URL is the OpenSearch endpoint URL
		URL string `mapstructure:"opensearch-url"  envconfig:"default=https://opensearch.portal.localhost:8443"`
		// Username for OpenSearch authentication
		Username string `mapstructure:"opensearch-username" envconfig:"default=admin"`
		// Password for OpenSearch authentication
		Password string `mapstructure:"opensearch-password" envconfig:"default=admin"`
		// IndexNamePrefix is a static prefix for all operator-managed index names and aliases.
		IndexNamePrefix string `mapstructure:"opensearch-index-name-prefix" envconfig:"default=pm-orgs"`
		// SemanticModelID is the OpenSearch ML model ID used for semantic field mappings.
		SemanticModelID string `mapstructure:"opensearch-semantic-model-id" envconfig:"optional,OPENSEARCH_SEMANTIC_MODEL_ID"`
	} `mapstructure:",squash"`
}

func (c Config) InitializerName() string {
	return "search"
}

// NewFromEnv creates a Config from environment values
func NewFromEnv() (*Config, error) {
	appConfig := Config{}
	err := envconfig.Init(&appConfig)
	return &appConfig, err
}
