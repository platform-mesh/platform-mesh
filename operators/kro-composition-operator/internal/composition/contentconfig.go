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

package composition

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/gobuffalo/flect"
)

// AccountEntity is the portal entity a generated type's nav node attaches to.
const AccountEntity = "core_platform-mesh_io_account"

// navOrder places every generated type's nav node after the static "kro" node, which
// config/provider/contentconfiguration.yaml pins at 850. Every generated type carries
// this same order, so their relative ordering is left to the portal.
const navOrder = 860

// graphQLName is the shape a GraphQL identifier must have.
var graphQLName = regexp.MustCompile(`^[_A-Za-z][_0-9A-Za-z]*$`)

// ValidateAPIGroup rejects an API group the portal could not query. The gateway only
// replaces dots with underscores, so a hyphen or a leading digit yields an unparseable
// field. Not fatal: the caller skips the portal wiring and still publishes the type.
func ValidateAPIGroup(group string) error {
	if group == "" {
		return fmt.Errorf("API group is empty")
	}
	for _, label := range strings.Split(group, ".") {
		if !graphQLName.MatchString(label) {
			return fmt.Errorf(
				"API group %q cannot be served to the portal: the segment %q is not a valid GraphQL name "+
					"(letters, digits and underscores only, not starting with a digit). Hyphens are the "+
					"usual cause, so use something like %q instead",
				group, label, "vault.demo.io")
		}
	}
	return nil
}

// BuildContentConfig renders the Luigi nav JSON for one type: list, detail and create
// views over the portal's generic components. specFields populates the forms.
func BuildContentConfig(group, version, kind, plural string, namespaced bool, specFields []string) string {
	ug := strings.ReplaceAll(group, ".", "_") // GraphQL-style underscored group
	navSeg := fmt.Sprintf("%s_%s", ug, plural)
	entityID := fmt.Sprintf("%s_%s", ug, strings.ToLower(kind))

	field := func(label, prop string) map[string]any { return map[string]any{"label": label, "property": prop} }

	listFields := []any{field("Name", "metadata.name")}
	createFields := make([]any, 0, 1+len(specFields))
	createFields = append(createFields, map[string]any{"label": "Name", "property": "metadata.name", "required": true})
	if namespaced {
		listFields = append(listFields, field("Namespace", "metadata.namespace"))
	}
	for _, f := range specFields {
		listFields = append(listFields, field(title(f), "spec."+f))
		createFields = append(createFields, map[string]any{"label": title(f), "property": "spec." + f, "required": true})
	}
	detailFields := append([]any{}, listFields...)
	listFields = append(listFields, field("State", "status.state"))
	detailFields = append(detailFields, field("State", "status.state"))

	query := "{ metadata { name } }"
	scope := "Cluster"
	if namespaced {
		query = "{ metadata { name namespace } }"
		scope = "Namespaced"
	}

	namespacePermissions := map[string]any{
		"urlParameters": map[string]any{
			"namespace": map[string]any{"read": true, "write": true},
		},
	}

	listNode := map[string]any{
		"pathSegment":             navSeg,
		"navigationContext":       navSeg,
		"label":                   kind + " (kro)",
		"icon":                    "example",
		"order":                   navOrder,
		"entityType":              "main." + AccountEntity,
		"keepSelectedForChildren": true,
		"url":                     "/assets/platform-mesh-portal-ui-wc.js#generic-list-view",
		"webcomponent":            map[string]any{"selfRegistered": true, "type": "module"},
		"clientPermissions":       namespacePermissions,
		"context": map[string]any{
			"resourceDefinition": map[string]any{
				"apiGroup": ug,
				"version":  version,
				"entity":   kind,
				// The portal queries this as a GraphQL field on the gateway, which names
				// the list field flect.Pluralize(Kind) (e.g. WebPage -> WebPages, not the
				// lowercase resource plural). Match that exactly, same lib as the gateway.
				"entityCollection": flect.Pluralize(kind),
				"scope":            scope,
				"namespace":        nil,
				"readyCondition":   map[string]any{"jsonPathExpression": "status.state", "property": []any{"status.state"}},
				"ui": map[string]any{
					"listView":   map[string]any{"fields": listFields},
					"detailView": map[string]any{"fields": detailFields},
					"createView": map[string]any{"fields": createFields},
				},
			},
		},
		"children": []any{
			map[string]any{
				"pathSegment":             ":resourceId",
				"hideFromNav":             true,
				"keepSelectedForChildren": false,
				"defineEntity": map[string]any{
					"id":         entityID,
					"contextKey": "resourceId",
					"graphqlEntity": map[string]any{
						"group": ug, "version": version, "kind": kind, "query": query,
					},
				},
				"context": map[string]any{"accountId": ":accountId", "resourceId": ":resourceId"},
			},
		},
	}
	detailNode := map[string]any{
		"entityType":   "main." + AccountEntity + "." + entityID,
		"pathSegment":  "dashboard",
		"label":        "Overview",
		"url":          "/assets/platform-mesh-portal-ui-wc.js#generic-detail-view",
		"webcomponent": map[string]any{"selfRegistered": true, "type": "module"},
		"defineEntity": map[string]any{"id": "dashboard"},
		"compound":     map[string]any{"children": []any{}},
	}
	if namespaced {
		detailNode["clientPermissions"] = namespacePermissions
	}

	doc := map[string]any{
		"name": navSeg,
		"luigiConfigFragment": map[string]any{
			"data": map[string]any{
				"nodes": []any{listNode, detailNode},
				"texts": []any{
					map[string]any{"locale": "", "textDictionary": map[string]any{navSeg: kind + " (kro)"}},
					map[string]any{"locale": "en", "textDictionary": map[string]any{navSeg: kind + " (kro)"}},
				},
			},
		},
	}
	out, _ := json.Marshal(doc)
	return string(out)
}

func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
