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

type MutationGenerator struct {
	resolver *resolver.Service
}

func NewMutationGenerator(resolver *resolver.Service) *MutationGenerator {
	return &MutationGenerator{resolver: resolver}
}

func (g *MutationGenerator) Generate(rc *ResourceContext, target *graphql.Object) {
	target.AddFieldConfig("create"+rc.SingularName, &graphql.Field{
		Type:    rc.ResourceType,
		Args:    resolver.CreateArgs(rc.Scope, rc.InputType),
		Resolve: g.resolver.CreateItem(rc.GVK, rc.Scope),
	})

	target.AddFieldConfig("update"+rc.SingularName, &graphql.Field{
		Type:    rc.ResourceType,
		Args:    resolver.UpdateArgs(rc.Scope, rc.InputType),
		Resolve: g.resolver.UpdateItem(rc.GVK, rc.Scope),
	})

	target.AddFieldConfig("delete"+rc.SingularName, &graphql.Field{
		Type:    graphql.Boolean,
		Args:    resolver.DeleteArgs(rc.Scope),
		Resolve: g.resolver.DeleteItem(rc.GVK, rc.Scope),
	})
}
