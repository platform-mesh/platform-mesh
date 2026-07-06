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
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/schema/fields"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/schema/types"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	typeByCategoryFieldName      = "typeByCategory"
	resourcesByCategoryFieldName = "resourcesByCategory"
)

type CustomQueryGenerator struct {
	resolver        *resolver.Service
	categoryManager *CategoryManager
	registry        *types.Registry
}

func NewCustomQueryGenerator(
	resolver *resolver.Service,
	categoryManager *CategoryManager,
	registry *types.Registry,
) *CustomQueryGenerator {
	return &CustomQueryGenerator{
		resolver:        resolver,
		categoryManager: categoryManager,
		registry:        registry,
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

func (g *CustomQueryGenerator) AddResourcesByCategoryQuery(
	rootQueryType *graphql.Object,
	resourceUnion *graphql.Union,
) {
	rootQueryType.AddFieldConfig(resourcesByCategoryFieldName, &graphql.Field{
		Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(resourceUnion))),
		Args: graphql.FieldConfigArgument{
			resolver.NameArg:          resolver.NameArgConfig,
			resolver.NamespaceArg:     resolver.NamespaceArgConfig,
			resolver.LabelSelectorArg: resolver.LabelSelectorArgConfig,
		},
		Resolve: g.resolver.ResourcesByCategory(g.categoryManager.AllCategories()),
	})
}

func (g *CustomQueryGenerator) AddResourcesByCategorySubscription(
	rootSubscription *graphql.Object,
	resourceUnion *graphql.Union,
) {
	eventType := graphql.NewObject(
		graphql.ObjectConfig{
			Name: resourceUnion.Name() + "Event",
			Fields: graphql.Fields{
				"type": &graphql.Field{Type: graphql.NewNonNull(fields.WatchEventTypeEnum)},
				// must be nullable for delete events?
				"object": &graphql.Field{Type: resourceUnion},
			},
		},
	)

	rootSubscription.AddFieldConfig(resourcesByCategoryFieldName,
		&graphql.Field{
			Type: graphql.NewNonNull(eventType),
			Args: graphql.FieldConfigArgument{
				resolver.NameArg:          resolver.NameArgConfig,
				resolver.NamespaceArg:     resolver.NamespaceArgConfig,
				resolver.LabelSelectorArg: resolver.LabelSelectorArgConfig,
			},
			Resolve:   resolver.CreateSubscriptionResolver(),
			Subscribe: g.resolver.SubscribeResourcesByCategory(g.categoryManager.AllCategories()),
		})
}

func graphqlStringField() *graphql.Field {
	return &graphql.Field{
		Type: graphql.NewNonNull(graphql.String),
	}
}

// BuildResourceUnion returns a union of all registered resource types.
func BuildResourceUnion(cm *CategoryManager, reg *types.Registry) *graphql.Union {
	typesByGVK := make(map[schema.GroupVersionKind]*graphql.Object)
	for _, v := range cm.AllCategories() {
		for _, t := range v {
			gvk := schema.GroupVersionKind{
				Group:   t.Group,
				Version: t.Version,
				Kind:    t.Kind,
			}
			gObj, _ := reg.Get(reg.GetUniqueTypeName(&gvk))
			if gObj == nil {
				continue
			}
			typesByGVK[gvk] = gObj
		}
	}

	unionTypes := make([]*graphql.Object, 0, len(typesByGVK))
	for _, gObj := range typesByGVK {
		unionTypes = append(unionTypes, gObj)
	}

	uType := graphql.NewUnion(graphql.UnionConfig{
		Name:  "CategoryResource",
		Types: unionTypes,
		ResolveType: func(p graphql.ResolveTypeParams) *graphql.Object {
			logger := log.FromContext(p.Context)

			rawObj, ok := p.Value.(map[string]any)
			if !ok {
				return nil
			}

			apiVersion, ok, err := unstructured.NestedString(rawObj, "apiVersion")
			if err != nil {
				logger.Error(err, "reading field on resolve", "field", "apiVersion")
				return nil
			}
			if !ok {
				return nil
			}

			kind, ok, err := unstructured.NestedString(rawObj, "kind")
			if err != nil {
				logger.Error(err, "reading field on resolve: not a string", "field", "kind")
				return nil
			}
			if !ok {
				return nil
			}

			gv, err := schema.ParseGroupVersion(apiVersion)
			if err != nil {
				return nil
			}

			gvk := gv.WithKind(kind)

			return typesByGVK[gvk]
		},
	})

	return uType
}
