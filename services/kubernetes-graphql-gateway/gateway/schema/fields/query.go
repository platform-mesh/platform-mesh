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

package fields

import (
	"github.com/graphql-go/graphql"

	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/resolver"
)

type QueryGenerator struct {
	resolver *resolver.Service
}

func NewQueryGenerator(resolver *resolver.Service) *QueryGenerator {
	return &QueryGenerator{resolver: resolver}
}

func (g *QueryGenerator) Generate(rc *ResourceContext, target *graphql.Object) {
	listArgs := resolver.ListArgs(rc.Scope)
	itemArgs := resolver.ItemArgs(rc.Scope)

	listWrapperType := graphql.NewObject(graphql.ObjectConfig{
		Name:   rc.UniqueTypeName + "List",
		Fields: resolver.ListResultFields(rc.ResourceType),
	})

	target.AddFieldConfig(rc.PluralName, &graphql.Field{
		Type:    graphql.NewNonNull(listWrapperType),
		Args:    listArgs,
		Resolve: g.resolver.ListItems(rc.GVK, rc.Scope),
	})

	target.AddFieldConfig(rc.SingularName, &graphql.Field{
		Type:    graphql.NewNonNull(rc.ResourceType),
		Args:    itemArgs,
		Resolve: g.resolver.GetItem(rc.GVK, rc.Scope),
	})

	target.AddFieldConfig(rc.SingularName+"Yaml", &graphql.Field{
		Type:    graphql.NewNonNull(graphql.String),
		Args:    itemArgs,
		Resolve: g.resolver.GetItemAsYAML(rc.GVK, rc.Scope),
	})
}
