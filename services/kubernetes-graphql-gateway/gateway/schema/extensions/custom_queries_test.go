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
	"testing"

	"github.com/graphql-go/graphql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/resolver"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/schema/types"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// registerMember adds a resource type under the given GVK in the registry.
func registerMember(r *types.Registry, group, version, kind string) schema.GroupVersionKind {
	gvk := schema.GroupVersionKind{Group: group, Version: version, Kind: kind}
	r.Register(r.GetUniqueTypeName(&gvk), graphql.NewObject(graphql.ObjectConfig{
		Name:   r.GetUniqueTypeName(&gvk),
		Fields: graphql.Fields{"apiVersion": {Type: graphql.String}},
	}), nil)
	return gvk
}

func TestBuildResourceUnion(t *testing.T) {
	r := types.NewRegistry()
	cert := registerMember(r, "blappers.deploy.once", "v1", "Certificate")
	issuer := registerMember(r, "blappers.deploy.once", "v1", "Issuer")

	categoryName := "cert-manager"

	cm := CategoryManager{typeByCategory: map[string][]resolver.TypeByCategory{
		categoryName: {
			resolver.TypeByCategory{Group: cert.Group, Version: cert.Version, Kind: cert.Kind},
			resolver.TypeByCategory{Group: issuer.Group, Version: issuer.Version, Kind: issuer.Kind},
		},
	}}

	union := BuildResourceUnion(&cm, r)

	root := graphql.NewObject(graphql.ObjectConfig{
		Name: "Query",
		Fields: graphql.Fields{
			"testfield": &graphql.Field{
				Type: graphql.NewList(union),
				Resolve: func(p graphql.ResolveParams) (any, error) {
					return []any{
							map[string]any{"apiVersion": "blappers.deploy.once/v1", "kind": "Certificate"},
							map[string]any{"apiVersion": "blappers.deploy.once/v1", "kind": "Issuer"},
						},
						nil
				},
			},
		},
	})

	sch, err := graphql.NewSchema(graphql.SchemaConfig{Query: root})
	require.NoError(t, err)

	response := graphql.Do(graphql.Params{
		Schema:        sch,
		Context:       t.Context(),
		RequestString: `{ testfield { __typename } }`,
	})
	require.Empty(t, response.Errors, "query should not err")

	items := response.Data.(map[string]any)["testfield"].([]any)
	_ = items

	result := make([]string, 0, len(items))
	for _, item := range items {
		typeName := item.(map[string]any)["__typename"]
		result = append(result, typeName.(string))
	}

	assert.ElementsMatch(t, []string{
		r.GetUniqueTypeName(&cert),
		r.GetUniqueTypeName(&issuer),
	}, result)
}
