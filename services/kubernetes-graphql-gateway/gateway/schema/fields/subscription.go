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
	"fmt"
	"strings"

	"github.com/graphql-go/graphql"

	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/resolver"
)

var WatchEventTypeEnum = graphql.NewEnum(graphql.EnumConfig{
	Name: "WatchEventType",
	Values: graphql.EnumValueConfigMap{
		resolver.EventTypeAdded:    &graphql.EnumValueConfig{Value: resolver.EventTypeAdded},
		resolver.EventTypeModified: &graphql.EnumValueConfig{Value: resolver.EventTypeModified},
		resolver.EventTypeDeleted:  &graphql.EnumValueConfig{Value: resolver.EventTypeDeleted},
	},
})

type SubscriptionGenerator struct {
	resolver *resolver.Service
}

func NewSubscriptionGenerator(resolver *resolver.Service) *SubscriptionGenerator {
	return &SubscriptionGenerator{resolver: resolver}
}

func (g *SubscriptionGenerator) Generate(rc *ResourceContext, target *graphql.Object) {
	eventType := graphql.NewObject(graphql.ObjectConfig{
		Name: rc.UniqueTypeName + "Event",
		Fields: graphql.Fields{
			"type":   &graphql.Field{Type: graphql.NewNonNull(WatchEventTypeEnum)},
			"object": &graphql.Field{Type: rc.ResourceType},
		},
	})

	singularName := g.buildSubscriptionName(rc, rc.SingularName)
	pluralName := g.buildSubscriptionName(rc, rc.PluralName)

	target.AddFieldConfig(singularName, &graphql.Field{
		Type:        eventType,
		Args:        resolver.SubscriptionItemArgs(rc.Scope),
		Resolve:     resolver.CreateSubscriptionResolver(),
		Subscribe:   g.resolver.SubscribeItem(rc.GVK, rc.Scope),
		Description: fmt.Sprintf("Subscribe to changes of %s", rc.SingularName),
	})

	target.AddFieldConfig(pluralName, &graphql.Field{
		Type:        eventType,
		Args:        resolver.SubscriptionListArgs(rc.Scope),
		Resolve:     resolver.CreateSubscriptionResolver(),
		Subscribe:   g.resolver.SubscribeItems(rc.GVK, rc.Scope),
		Description: fmt.Sprintf("Subscribe to changes of %s", rc.PluralName),
	})
}

func (g *SubscriptionGenerator) buildSubscriptionName(rc *ResourceContext, name string) string {
	if rc.SanitizedGroup == "" {
		return strings.ToLower(fmt.Sprintf("%s_%s", rc.GVK.Version, name))
	}
	return strings.ToLower(fmt.Sprintf("%s_%s_%s", rc.SanitizedGroup, rc.GVK.Version, name))
}
