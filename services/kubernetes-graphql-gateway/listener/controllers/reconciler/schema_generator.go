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

package reconciler

import (
	"context"
	"encoding/json"
	"fmt"

	pmgatewayv1alpha1 "go.platform-mesh.io/apis/gateway/v1alpha1"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/pkg/apischema"
	"go.platform-mesh.io/kubernetes-graphql-gateway/listener/pkg/apischema/enricher"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// schemaGenerationParams contains parameters for schema generation
type schemaGenerationParams struct {
	ClusterPath     string
	DiscoveryClient discovery.DiscoveryInterface
	RESTMapper      meta.RESTMapper
	HostOverride    string // Optional: for virtual workspaces with custom URLs
}

// generateSchemaWithMetadata is a shared utility for schema generation
// Used by both regular APIBinding reconciliation and virtual workspace processing
func generateSchemaWithMetadata(
	ctx context.Context,
	params schemaGenerationParams,
	metadata *pmgatewayv1alpha1.ClusterMetadata,
) ([]byte, error) {
	logger := log.FromContext(ctx)

	logger.V(4).WithValues("clusterPath", params.ClusterPath).Info("starting API schema resolution")

	// Get preferred resources for categories enricher
	apiResources, err := params.DiscoveryClient.ServerPreferredResources()
	if err != nil {
		// Log but don't fail - some resources may still be available
		logger.Info("partial error getting server preferred resources", "error", err)
		if apiResources == nil {
			return nil, fmt.Errorf("failed to get server preferred resources: %w", err)
		}
	}

	// Create resolver with enrichers configured for this cluster
	resolver := apischema.NewResolver(
		enricher.NewScope(params.RESTMapper),
		enricher.NewCategories(apiResources),
	)

	// Resolve current schema from API server
	rawSchema, err := resolver.Resolve(ctx, params.DiscoveryClient.OpenAPIV3())
	if err != nil {
		logger.Error(err, "failed to resolve server JSON schema")
		return nil, fmt.Errorf("failed to resolve API schema: %w", err)
	}

	logger.V(4).WithValues("clusterPath", params.ClusterPath, "schemaSize", len(rawSchema)).Info("API schema resolved")

	// Parse the existing schema JSON and inject cluster metadata if provided
	if metadata != nil {
		// TODO: This is ugly! Improve in future.
		var schemaJSON map[string]any
		if err := json.Unmarshal(rawSchema, &schemaJSON); err != nil {
			return nil, fmt.Errorf("failed to parse schema JSON: %w", err)
		}

		data, err := json.Marshal(metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal cluster metadata: %w", err)
		}
		// marshal metadata into map[string]any
		var metadataMap map[string]any
		if err := json.Unmarshal(data, &metadataMap); err != nil {
			return nil, fmt.Errorf("failed to unmarshal cluster metadata: %w", err)
		}

		// Inject the metadata into the schema
		schemaJSON["x-cluster-metadata"] = metadataMap
		return json.Marshal(schemaJSON)
	}

	return rawSchema, nil
}
