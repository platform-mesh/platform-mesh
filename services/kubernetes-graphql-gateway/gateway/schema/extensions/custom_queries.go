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

package extensions

import (
	"github.com/graphql-go/graphql"

	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/resolver"
)

const (
	typeByCategoryFieldName = "typeByCategory"
)

type CustomQueryGenerator struct {
	resolver        *resolver.Service
	categoryManager *CategoryManager
}

func NewCustomQueryGenerator(resolver *resolver.Service, categoryManager *CategoryManager) *CustomQueryGenerator {
	return &CustomQueryGenerator{
		resolver:        resolver,
		categoryManager: categoryManager,
	}
}

func (g *CustomQueryGenerator) AddTypeByCategoryQuery(rootQueryType *graphql.Object) {
	resourceType := graphql.NewObject(graphql.ObjectConfig{
		Name: typeByCategoryFieldName + "Object",
		Fields: graphql.Fields{
			"kind":    graphqlStringField(),
			"group":   graphqlStringField(),
			"version": graphqlStringField(),
			"scope":   graphqlStringField(),
		},
	})

	rootQueryType.AddFieldConfig(typeByCategoryFieldName, &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(resourceType))),
		Args: graphql.FieldConfigArgument{
			resolver.NameArg: resolver.NameArgConfig,
		},
		Resolve: g.resolver.TypeByCategory(g.categoryManager.AllCategories()),
	})
}

func graphqlStringField() *graphql.Field {
	return &graphql.Field{
		Type: graphql.NewNonNull(graphql.String),
	}
}
