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

package schema

import (
	"context"

	"github.com/graphql-go/graphql"

	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/resolver"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/schema/extensions"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/schema/generator"

	"k8s.io/kube-openapi/pkg/validation/spec"
)

// Provider provides access to the generated GraphQL schema.
// It acts as a thin facade over the generator package.
type Provider struct {
	schema *graphql.Schema
}

// New creates a new Provider with a GraphQL schema built from OpenAPI definitions.
func New(
	ctx context.Context,
	definitions map[string]*spec.Schema,
	resolverProvider *resolver.Service,
	customSubGen *extensions.CustomSubscriptionGenerator,
	resourcesByCategoryEnabled bool,
) (*Provider, error) {
	schema, err := generator.New(definitions, resolverProvider, customSubGen, resourcesByCategoryEnabled).
		Generate(ctx)
	if err != nil {
		return nil, err
	}

	return &Provider{schema: schema}, nil
}

// GetSchema returns the generated GraphQL schema.
func (p *Provider) GetSchema() *graphql.Schema {
	return p.schema
}
