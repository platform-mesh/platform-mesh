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

func TestUnionFoo(t *testing.T) {
	r := types.NewRegistry()

	first := schema.GroupVersionKind{
		Group:   "blappers.deploy.once",
		Version: "v1",
		Kind:    "Certificate",
	}
	second := schema.GroupVersionKind{
		Group:   "blappers.deploy.once",
		Version: "v1beta1",
		Kind:    "Issuer",
	}

	key := r.GetUniqueTypeName(&first)

	r.Register(key, graphql.NewObject(graphql.ObjectConfig{
		Name: "First",
		Fields: graphql.Fields{
			"apiVersion": &graphql.Field{
				Type: graphql.String,
			},
		},
	}), nil)
	r.Register(
		r.GetUniqueTypeName(&second),
		graphql.NewObject(graphql.ObjectConfig{
			Name: "Another",
			Fields: graphql.Fields{
				"apiVersion": &graphql.Field{
					Type: graphql.String,
				},
			},
		}), nil)

	typesByCat := map[string][]resolver.TypeByCategory{
		"cert-manager": {
			{
				Group:   first.Group,
				Version: first.Version,
				Kind:    first.Kind,
			},
			{
				Group:   second.Group,
				Version: second.Version,
				Kind:    second.Kind,
			},
		},
	}

	cm := CategoryManager{
		typeByCategory: typesByCat,
	}
	g := CustomQueryGenerator{
		registry:        r,
		categoryManager: &cm}

	root := graphql.NewObject(
		graphql.ObjectConfig{
			Name:   "Query",
			Fields: graphql.Fields{},
		},
	)

	resources := BuildResourceUnion(&cm, g.registry)
	g.AddResourcesByCategoryQuery(root, resources)

	field := root.Fields()["resourcesByCategory"]
	require.NotNil(t, field)

	x, ok := field.Type.(*graphql.NonNull)
	require.True(t, ok)

	foo, ok := x.OfType.(*graphql.List)
	require.True(t, ok)

	bar, ok := foo.OfType.(*graphql.NonNull)
	require.True(t, ok)

	baz, ok := bar.OfType.(*graphql.Union)
	require.True(t, ok)

	require.Len(t, baz.Types(), 2)
	assert.Equal(t, "CategoryResource", baz.Name())
}
