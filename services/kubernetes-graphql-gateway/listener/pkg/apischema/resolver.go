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

package apischema

import (
	"context"
	"fmt"

	"go.platform-mesh.io/kubernetes-graphql-gateway/apischema"

	"k8s.io/client-go/openapi"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Resolver orchestrates schema loading and enrichment.
type Resolver struct {
	loader    *SchemaLoader
	enrichers []Enricher
}

// Enricher modifies schemas in place to add metadata or extensions.
// Enrichers are run as a pipeline after schema loading.
type Enricher interface {
	// Name returns a human-readable name for logging.
	Name() string

	// Enrich modifies schemas in the set.
	// Returns error if enrichment fails critically.
	Enrich(ctx context.Context, schemas *apischema.SchemaSet) error
}

// NewResolver creates a new Resolver with the given enrichers.
// Enrichers are applied in order after schemas are loaded.
func NewResolver(enrichers ...Enricher) *Resolver {
	return &Resolver{
		loader:    NewSchemaLoader(),
		enrichers: enrichers,
	}
}

// Resolve loads schemas from the OpenAPI client and applies enrichments.
func (r *Resolver) Resolve(ctx context.Context, oc openapi.Client) ([]byte, error) {
	logger := log.FromContext(ctx)

	// 1. Load schemas from OpenAPI
	schemas, err := r.loader.Load(ctx, oc)
	if err != nil {
		return nil, err
	}

	logger.Info("loaded schemas", "count", schemas.Size())

	// 2. Run enrichers
	for _, e := range r.enrichers {
		if err := e.Enrich(ctx, schemas); err != nil {
			return nil, fmt.Errorf("enricher %s failed: %w", e.Name(), err)
		}
		logger.V(4).Info("applied enricher", "name", e.Name())
	}

	// 3. Marshal output
	result, err := schemas.Marshal()
	if err != nil {
		return nil, fmt.Errorf("marshal schemas: %w", err)
	}

	logger.Info("resolved schema",
		"schemaCount", schemas.Size(),
		"enricherCount", len(r.enrichers))

	return result, nil
}
