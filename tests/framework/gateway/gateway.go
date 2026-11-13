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
// Package gateway reads through the GraphQL gateway of a deployed Platform
// Mesh, which is how the portal reads: the gateway serves every workspace
// as a GraphQL schema generated from the APIs of the workspace, and sends
// the queries on to kcp as the user.
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"go.platform-mesh.io/platform-mesh/tests/framework/graphql"
	"go.platform-mesh.io/platform-mesh/tests/framework/pm"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// path is where the gateway serves the workspace.
func path(workspace string) string { return "/gateway/api/clusters/" + workspace + "/graphql" }

// List returns the names, sorted, of the objects of a kind in the workspace
// at the path workspace of the organization org, as the user with the ID
// token (tenant.Login). kinds is the group and version of the kind and its
// name in the plural, as the schema of the gateway spells it:
//
//	gateway.List(ctx, inst, org, path, token, schema.GroupVersionKind{
//		Group: "core.platform-mesh.io", Version: "v1alpha1", Kind: "Accounts",
//	})
//
// The gateway answers only for an API that is in the schema it generated
// for the workspace, so List also tells whether it has taken up an API the
// workspace bound.
func List(ctx context.Context, inst pm.Installation, org, workspace, token string, kinds schema.GroupVersionKind) ([]string, error) {
	// The schema has the group with every character GraphQL does not
	// allow in a name replaced.
	group := strings.NewReplacer(".", "_", "-", "_").Replace(kinds.Group)
	query := fmt.Sprintf(`{ %s { %s { %s { items { metadata { name } } } } } }`, group, kinds.Version, kinds.Kind)

	var answer map[string]map[string]map[string]struct {
		Items []struct{ Metadata struct{ Name string } }
	}
	var data json.RawMessage
	if err := graphql.Query(ctx, inst, org, path(workspace), token, query, &data); err != nil {
		return nil, fmt.Errorf("listing %s of %s through the gateway: %w", kinds.Kind, workspace, err)
	}
	if err := json.Unmarshal(data, &answer); err != nil {
		return nil, fmt.Errorf("listing %s of %s through the gateway: %w", kinds.Kind, workspace, err)
	}
	items := answer[group][kinds.Version][kinds.Kind].Items
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Metadata.Name)
	}
	slices.Sort(names)
	return names, nil
}
