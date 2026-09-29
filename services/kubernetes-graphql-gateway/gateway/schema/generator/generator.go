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

package generator

import (
	"cmp"
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/gobuffalo/flect"
	"github.com/graphql-go/graphql"

	"go.platform-mesh.io/kubernetes-graphql-gateway/apischema"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/resolver"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/schema/extensions"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/schema/fields"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/schema/types"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kube-openapi/pkg/validation/spec"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Resource holds parsed metadata for a Kubernetes resource.
type Resource struct {
	Key            string
	Schema         *spec.Schema
	GVK            schema.GroupVersionKind
	Scope          apiextensionsv1.ResourceScope
	SingularName   string
	PluralName     string
	SanitizedGroup string
}

// SchemaGenerator transforms Kubernetes OpenAPI definitions into a GraphQL schema.
type SchemaGenerator struct {
	definitions map[string]*spec.Schema
	resolver    *resolver.Service

	typeRegistry  *types.Registry
	typeConverter *types.Converter

	queryGen        *fields.QueryGenerator
	mutationGen     *fields.MutationGenerator
	subscriptionGen *fields.SubscriptionGenerator

	categoryManager *extensions.CategoryManager
	customQueryGen  *extensions.CustomQueryGenerator
	customSubGen    *extensions.CustomSubscriptionGenerator

	// resourcesByCategoryEnabled is a PoC feature flag which will be removed eventually.
	resourcesByCategoryEnabled bool
}

// New creates a new schema generator.
func New(
	definitions map[string]*spec.Schema,
	resolverProvider *resolver.Service,
	customSubGen *extensions.CustomSubscriptionGenerator,
	resourcesByCategoryEnabled bool,
) *SchemaGenerator {
	registry := types.NewRegistry()
	categoryManager := extensions.NewCategoryManager(definitions)

	return &SchemaGenerator{
		definitions:     definitions,
		resolver:        resolverProvider,
		typeRegistry:    registry,
		typeConverter:   types.NewConverter(registry),
		queryGen:        fields.NewQueryGenerator(resolverProvider),
		mutationGen:     fields.NewMutationGenerator(resolverProvider),
		subscriptionGen: fields.NewSubscriptionGenerator(resolverProvider),
		categoryManager: categoryManager,
		customQueryGen: extensions.NewCustomQueryGenerator(
			resolverProvider,
			categoryManager,
			registry,
		),
		customSubGen:               customSubGen,
		resourcesByCategoryEnabled: resourcesByCategoryEnabled,
	}
}

// Generate constructs the complete GraphQL schema.
func (g *SchemaGenerator) Generate(ctx context.Context) (*graphql.Schema, error) {
	logger := log.FromContext(ctx)

	rootQuery := graphql.NewObject(graphql.ObjectConfig{Name: "Query", Fields: graphql.Fields{}})
	rootMutation := graphql.NewObject(graphql.ObjectConfig{Name: "Mutation", Fields: graphql.Fields{}})
	rootSubscription := graphql.NewObject(graphql.ObjectConfig{Name: "Subscription", Fields: graphql.Fields{}})

	resources := g.parseResources()
	for _, r := range resources {
		g.typeRegistry.ReserveTypeName(g.typeRegistry.GetUniqueTypeName(&r.GVK))
	}
	groups := groupByAPIGroup(resources)

	sortedGroups := make([]string, 0, len(groups))
	for group := range groups {
		sortedGroups = append(sortedGroups, group)
	}
	sort.Strings(sortedGroups)

	for _, group := range sortedGroups {
		g.processGroup(ctx, group, groups[group], rootQuery, rootMutation, rootSubscription)
	}

	g.customQueryGen.AddTypeByCategoryQuery(rootQuery)

	if g.resourcesByCategoryEnabled {
		resourceWithCategory := extensions.BuildCategoryResourceUnion(g.categoryManager, g.typeRegistry)
		if resourceWithCategory != nil {
			g.customQueryGen.AddResourcesByCategoryQuery(rootQuery, resourceWithCategory)
			g.customQueryGen.AddResourcesByCategorySubscription(rootSubscription, resourceWithCategory)
		}
	}

	g.addApplyYamlMutation(rootMutation)

	if g.customSubGen != nil {
		g.customSubGen.AddPodLogsSubscription(rootSubscription, g.definitions)
	}

	schema, err := graphql.NewSchema(graphql.SchemaConfig{
		Query:        rootQuery,
		Mutation:     rootMutation,
		Subscription: rootSubscription,
	})
	if err != nil {
		logger.Error(err, "Error creating GraphQL schema")
		return nil, err
	}

	return &schema, nil
}

// parseResources extracts and validates all resources from definitions.
func (g *SchemaGenerator) parseResources() []*Resource {
	var resources []*Resource

	for key, def := range g.definitions {
		gvk, err := apischema.ExtractGVK(def)
		if err != nil || gvk == nil || gvk.Kind == "" {
			continue
		}

		scope, err := apischema.ExtractScope(def)
		if err != nil {
			continue
		}

		sanitizedGroup := ""
		if gvk.Group != "" {
			sanitizedGroup = types.SanitizeGroupName(gvk.Group)
		}

		resources = append(resources, &Resource{
			Key:            key,
			Schema:         def,
			GVK:            *gvk,
			Scope:          scope,
			SingularName:   types.SanitizeFieldName(gvk.Kind),
			PluralName:     types.SanitizeFieldName(flect.Pluralize(gvk.Kind)),
			SanitizedGroup: sanitizedGroup,
		})
	}

	kinds := sets.New[schema.GroupVersionKind]()
	for _, r := range resources {
		kinds.Insert(r.GVK)
	}
	// XList is the list type of X when X exists in the same group and version.
	resources = slices.DeleteFunc(resources, func(r *Resource) bool {
		item, isList := strings.CutSuffix(r.GVK.Kind, "List")
		return isList && kinds.Has(r.GVK.GroupVersion().WithKind(item))
	})

	slices.SortFunc(resources, func(a, b *Resource) int {
		return cmp.Or(
			cmp.Compare(a.SanitizedGroup, b.SanitizedGroup),
			cmp.Compare(a.GVK.Version, b.GVK.Version),
			cmp.Compare(a.GVK.Kind, b.GVK.Kind),
			cmp.Compare(a.Key, b.Key),
		)
	})

	return resources
}

// groupByAPIGroup organizes resources into a hierarchy: group → version → resources.
func groupByAPIGroup(resources []*Resource) map[string]map[string][]*Resource {
	groups := make(map[string]map[string][]*Resource)

	for _, r := range resources {
		group := r.SanitizedGroup
		version := r.GVK.Version

		if groups[group] == nil {
			groups[group] = make(map[string][]*Resource)
		}
		groups[group][version] = append(groups[group][version], r)
	}

	return groups
}

// processGroup processes all resources in an API group.
func (g *SchemaGenerator) processGroup(
	ctx context.Context,
	group string,
	versions map[string][]*Resource,
	rootQuery, rootMutation, rootSubscription *graphql.Object,
) {
	logger := log.FromContext(ctx)
	isRoot := group == ""

	var queryGroupType, mutationGroupType *graphql.Object
	if !isRoot {
		queryGroupType = g.createGroupType(group, "Query")
		mutationGroupType = g.createGroupType(group, "Mutation")
	}

	sortedVersions := make([]string, 0, len(versions))
	for v := range versions {
		sortedVersions = append(sortedVersions, v)
	}
	sort.Strings(sortedVersions)

	for _, version := range sortedVersions {
		resources := versions[version]
		queryVersionType := g.createVersionType(group, version, "Query")
		mutationVersionType := g.createVersionType(group, version, "Mutation")

		for _, resource := range resources {
			g.processResource(ctx, resource, queryVersionType, mutationVersionType, rootSubscription)
		}

		if len(queryVersionType.Fields()) > 0 {
			if isRoot {
				rootQuery.AddFieldConfig(version, &graphql.Field{
					Type:    queryVersionType,
					Resolve: g.resolver.CommonResolver(),
				})
			} else {
				queryGroupType.AddFieldConfig(version, &graphql.Field{
					Type:    queryVersionType,
					Resolve: g.resolver.CommonResolver(),
				})
			}
		}

		if len(mutationVersionType.Fields()) > 0 {
			if isRoot {
				rootMutation.AddFieldConfig(version, &graphql.Field{
					Type:    mutationVersionType,
					Resolve: g.resolver.CommonResolver(),
				})
			} else {
				mutationGroupType.AddFieldConfig(version, &graphql.Field{
					Type:    mutationVersionType,
					Resolve: g.resolver.CommonResolver(),
				})
			}
		}
	}

	if !isRoot {
		if len(queryGroupType.Fields()) > 0 {
			rootQuery.AddFieldConfig(group, &graphql.Field{
				Type:    queryGroupType,
				Resolve: g.resolver.CommonResolver(),
			})
		}
		if len(mutationGroupType.Fields()) > 0 {
			rootMutation.AddFieldConfig(group, &graphql.Field{
				Type:    mutationGroupType,
				Resolve: g.resolver.CommonResolver(),
			})
		}
	}

	logger.V(4).Info("Processed group", "group", group, "versionCount", len(versions))
}

// processResource generates GraphQL types and fields for a single resource.
func (g *SchemaGenerator) processResource(
	ctx context.Context,
	r *Resource,
	queryVersionType, mutationVersionType, rootSubscription *graphql.Object,
) {
	logger := log.FromContext(ctx)

	// Store category for custom queries
	if err := g.categoryManager.Store(r.Key, &r.GVK, r.Scope); err != nil {
		logger.V(4).Info("Resource has no categories", "resource", r.Key, "reason", err.Error())
	}

	uniqueTypeName := g.typeRegistry.GetUniqueTypeName(&r.GVK)
	listTypeName := g.typeRegistry.TypeName(uniqueTypeName, "List")
	eventTypeName := g.typeRegistry.TypeName(uniqueTypeName, "Event")

	gqlFields, inputFields, err := g.typeConverter.ConvertFields(r.Schema, g.definitions, uniqueTypeName)
	if err != nil {
		logger.Error(err, "Error generating fields", "resource", r.SingularName)
		return
	}

	if len(gqlFields) == 0 {
		logger.V(4).Info("No fields found", "resource", r.SingularName)
		return
	}

	resourceType := graphql.NewObject(graphql.ObjectConfig{
		Name:   uniqueTypeName,
		Fields: gqlFields,
	})

	inputType := graphql.NewInputObject(graphql.InputObjectConfig{
		Name:   uniqueTypeName + "_Input",
		Fields: inputFields,
	})
	g.typeRegistry.RegisterResource(r.GVK, resourceType)

	rc := &fields.ResourceContext{
		GVK:            r.GVK,
		Scope:          r.Scope,
		UniqueTypeName: uniqueTypeName,
		ListTypeName:   listTypeName,
		EventTypeName:  eventTypeName,
		ResourceType:   resourceType,
		InputType:      inputType,
		SingularName:   r.SingularName,
		PluralName:     r.PluralName,
		SanitizedGroup: r.SanitizedGroup,
	}

	g.queryGen.Generate(rc, queryVersionType)
	g.mutationGen.Generate(rc, mutationVersionType)
	g.subscriptionGen.Generate(rc, rootSubscription)
}

func (g *SchemaGenerator) addApplyYamlMutation(rootMutation *graphql.Object) {
	rootMutation.AddFieldConfig("applyYaml", &graphql.Field{
		Type:    types.JSONStringScalar,
		Args:    resolver.ApplyYamlArgs(),
		Resolve: g.resolver.ApplyYaml(),
	})
}

func (g *SchemaGenerator) createGroupType(group, suffix string) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name:   g.typeRegistry.TypeName(flect.Pascalize(group), suffix),
		Fields: graphql.Fields{},
	})
}

func (g *SchemaGenerator) createVersionType(group, version, suffix string) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name:   g.typeRegistry.TypeName(flect.Pascalize(group+"_"+version), suffix),
		Fields: graphql.Fields{},
	})
}
