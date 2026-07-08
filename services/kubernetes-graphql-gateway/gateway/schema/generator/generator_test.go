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
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmgateway "go.platform-mesh.io/apis/gateway"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/resolver"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/schema/types"
	"go.platform-mesh.io/kubernetes-graphql-gateway/internal/testfakes"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kube-openapi/pkg/validation/spec"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestGenerate_resourcesByCategory(t *testing.T) {
	t.Run("result for objects in two gvks", func(t *testing.T) {
		categoryName := "foo-managed"
		first := schemaWithCategory(
			"foo.tests.bar",
			"v1beta1",
			"Foo",
			apiextensionsv1.NamespaceScoped,
			categoryName,
		)
		second := schemaWithCategory(
			"foo.tests.bar",
			"v1",
			"Bar",
			apiextensionsv1.NamespaceScoped,
			categoryName,
		)

		foo := unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "foo.tests.bar/v1beta1",
				"kind":       "Foo",
				"metadata": map[string]any{
					"name": "first",
				},
			},
		}
		bar := unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "foo.tests.bar/v1",
				"kind":       "Bar",
				"metadata": map[string]any{
					"name": "second",
				},
			},
		}
		sut := setup(listItems(foo, bar), first, second)

		schemagen, err := sut.Generate(t.Context())
		require.NoError(t, err)
		require.NotNil(t, schemagen)

		reg := types.NewRegistry()
		fooType := reg.GetUniqueTypeName(&schema.GroupVersionKind{Group: "foo.tests.bar", Version: "v1beta1", Kind: "Foo"})
		barType := reg.GetUniqueTypeName(&schema.GroupVersionKind{Group: "foo.tests.bar", Version: "v1", Kind: "Bar"})

		query := fmt.Sprintf(`{
	resourcesByCategory(name: "%s") {
    __typename
    ... on %s { metadata { name } }
    ... on %s { metadata { name } }
  }
	}`, categoryName, fooType, barType)

		result := graphql.Do(graphql.Params{
			Schema:        *schemagen,
			Context:       t.Context(),
			RequestString: query,
		})

		require.Empty(t, result.Errors)

		queryResults := result.Data.(map[string]any)["resourcesByCategory"].([]any)
		assert.Len(t, queryResults, 2)

		byType := map[string]string{}
		for _, v := range queryResults {
			data := v.(map[string]any)
			meta := data["metadata"].(map[string]any)
			typeName := data["__typename"].(string)
			byType[typeName] = meta["name"].(string)
		}

		assert.Equal(t, "first", byType[fooType])
		assert.Equal(t, "second", byType[barType])
	})
	t.Run("exposes subscribeToAll arg", func(t *testing.T) {
		schemaDef := schemaWithCategory("a.b.c", "v1", "Fooer", apiextensionsv1.NamespaceScoped, "cat")
		sut := setup(listItems(), schemaDef)

		sch, err := sut.Generate(t.Context())
		require.NoError(t, err)

		field := sch.SubscriptionType().Fields()["resourcesByCategory"]
		require.NotNil(t, field)

		var argFound bool
		for _, v := range field.Args {
			if v.Name() == "subscribeToAll" {
				argFound = true
				break
			}
		}

		assert.True(t, argFound, "subscribeToAll arg should be on the query")
	})

	t.Run("resources have no category", func(t *testing.T) {
		uncategorized := schemaWithCategory(
			"pm.io",
			"v1",
			"Workload",
			apiextensionsv1.NamespaceScoped,
		)
		instance := unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "pm.io/v1",
				"kind":       "Workload",
				"metadata": map[string]any{
					"name": "foo",
				},
			},
		}

		sut := setup(listItems(instance), uncategorized)
		sch, err := sut.Generate(t.Context())
		require.NoError(t, err)

		reg := types.NewRegistry()
		workloadType := reg.GetUniqueTypeName(&schema.GroupVersionKind{Group: "pm.io", Version: "v1", Kind: "Workload"})

		schemaType := sch.Type(workloadType)
		require.NotNil(t, schemaType, "resource with no category should be in the schema")
	})
}

// listItems returns a client List function returning objs filtered by GVK.
func listItems(
	objs ...unstructured.Unstructured,
) func(context.Context, ctrlruntimeclient.ObjectList, ...ctrlruntimeclient.ListOption) error {
	byGVK := map[schema.GroupVersionKind][]unstructured.Unstructured{}
	for _, v := range objs {
		gvk := v.GroupVersionKind()
		byGVK[gvk] = append(byGVK[gvk], v)
	}

	return func(_ context.Context, list ctrlruntimeclient.ObjectList, _ ...ctrlruntimeclient.ListOption) error {
		ul := list.(*unstructured.UnstructuredList)
		ul.SetResourceVersion("100")

		gvk := ul.GetObjectKind().GroupVersionKind()
		gvk.Kind = strings.TrimSuffix(gvk.Kind, "List")
		ul.Items = byGVK[gvk]
		return nil
	}
}

// setup initializes the Generator with schemas and a fake client
// returning objects through the provided list function.
func setup(
	listFn func(context.Context, ctrlruntimeclient.ObjectList, ...ctrlruntimeclient.ListOption) error,
	schemas ...*spec.Schema,
) *SchemaGenerator {
	client := testfakes.NewClient(
		listFn,
		nil,
	)

	resolver := resolver.New(client, nil)
	definitions := make(map[string]*spec.Schema)
	for i, v := range schemas {
		key := fmt.Sprintf("def-%d", i)
		definitions[key] = v
	}

	return New(
		definitions,
		resolver,
		nil,
	)
}

func TestGroupByAPIGroup(t *testing.T) {
	tests := []struct {
		name      string
		resources []*Resource
		want      map[string]map[string][]string // group -> version -> resource keys
	}{
		{
			name:      "empty input",
			resources: nil,
			want:      map[string]map[string][]string{},
		},
		{
			name: "single resource",
			resources: []*Resource{
				{Key: "pod", GVK: schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}, SanitizedGroup: ""},
			},
			want: map[string]map[string][]string{
				"": {"v1": {"pod"}},
			},
		},
		{
			name: "multiple resources same group and version",
			resources: []*Resource{
				{Key: "pod", GVK: schema.GroupVersionKind{Version: "v1", Kind: "Pod"}, SanitizedGroup: ""},
				{Key: "service", GVK: schema.GroupVersionKind{Version: "v1", Kind: "Service"}, SanitizedGroup: ""},
			},
			want: map[string]map[string][]string{
				"": {"v1": {"pod", "service"}},
			},
		},
		{
			name: "multiple groups and versions",
			resources: []*Resource{
				{Key: "pod", GVK: schema.GroupVersionKind{Version: "v1", Kind: "Pod"}, SanitizedGroup: ""},
				{Key: "deployment", GVK: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, SanitizedGroup: "apps"},
				{Key: "daemonset-beta", GVK: schema.GroupVersionKind{Group: "apps", Version: "v1beta1", Kind: "DaemonSet"}, SanitizedGroup: "apps"},
			},
			want: map[string]map[string][]string{
				"":     {"v1": {"pod"}},
				"apps": {"v1": {"deployment"}, "v1beta1": {"daemonset-beta"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := groupByAPIGroup(tt.resources)

			// Convert to comparable format (keys only, since Resource pointers differ)
			gotKeys := make(map[string]map[string][]string)
			for group, versions := range got {
				gotKeys[group] = make(map[string][]string)
				for version, resources := range versions {
					for _, r := range resources {
						gotKeys[group][version] = append(gotKeys[group][version], r.Key)
					}
				}
			}

			assert.Equal(t, tt.want, gotKeys)
		})
	}
}

func TestCreateGroupType(t *testing.T) {
	tests := []struct {
		name     string
		group    string
		suffix   string
		wantName string
	}{
		{
			name:     "simple group",
			group:    "apps",
			suffix:   "Query",
			wantName: "AppsQuery",
		},
		{
			name:     "group with dots sanitized",
			group:    "networking_k8s_io",
			suffix:   "Mutation",
			wantName: "NetworkingK8sIoMutation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := createGroupType(tt.group, tt.suffix)

			assert.Equal(t, tt.wantName, got.Name())
			assert.Empty(t, got.Fields())
		})
	}
}

func TestCreateVersionType(t *testing.T) {
	tests := []struct {
		name     string
		group    string
		version  string
		suffix   string
		wantName string
	}{
		{
			name:     "core group",
			group:    "",
			version:  "v1",
			suffix:   "Query",
			wantName: "V1Query",
		},
		{
			name:     "apps group",
			group:    "apps",
			version:  "v1",
			suffix:   "Query",
			wantName: "AppsV1Query",
		},
		{
			name:     "beta version",
			group:    "apps",
			version:  "v1beta1",
			suffix:   "Mutation",
			wantName: "AppsV1beta1Mutation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := createVersionType(tt.group, tt.version, tt.suffix)

			assert.Equal(t, tt.wantName, got.Name())
			assert.Empty(t, got.Fields())
		})
	}
}

type expectedResource struct {
	key            string
	gvk            schema.GroupVersionKind
	scope          apiextensionsv1.ResourceScope
	singularName   string
	pluralName     string
	sanitizedGroup string
}

func TestParseResources(t *testing.T) {
	tests := []struct {
		name        string
		definitions map[string]*spec.Schema
		want        []expectedResource
	}{
		{
			name:        "empty definitions",
			definitions: map[string]*spec.Schema{},
			want:        nil,
		},
		{
			name: "schema without GVK extension is skipped",
			definitions: map[string]*spec.Schema{
				"io.k8s.api.core.v1.PodSpec": {},
			},
			want: nil,
		},
		{
			name: "schema without scope extension is skipped",
			definitions: map[string]*spec.Schema{
				"io.k8s.api.core.v1.Pod": schemaWithGVK("", "v1", "Pod"),
			},
			want: nil,
		},
		{
			name: "List kinds are skipped",
			definitions: map[string]*spec.Schema{
				"io.k8s.api.core.v1.PodList": schemaWithGVKAndScope("", "v1", "PodList", apiextensionsv1.NamespaceScoped),
			},
			want: nil,
		},
		{
			name: "core API resource (empty group)",
			definitions: map[string]*spec.Schema{
				"io.k8s.api.core.v1.Pod": schemaWithGVKAndScope("", "v1", "Pod", apiextensionsv1.NamespaceScoped),
			},
			want: []expectedResource{{
				key:            "io.k8s.api.core.v1.Pod",
				gvk:            schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
				scope:          apiextensionsv1.NamespaceScoped,
				singularName:   "Pod",
				pluralName:     "Pods",
				sanitizedGroup: "",
			}},
		},
		{
			name: "apps group resource",
			definitions: map[string]*spec.Schema{
				"io.k8s.api.apps.v1.Deployment": schemaWithGVKAndScope("apps", "v1", "Deployment", apiextensionsv1.NamespaceScoped),
			},
			want: []expectedResource{{
				key:            "io.k8s.api.apps.v1.Deployment",
				gvk:            schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
				scope:          apiextensionsv1.NamespaceScoped,
				singularName:   "Deployment",
				pluralName:     "Deployments",
				sanitizedGroup: "apps",
			}},
		},
		{
			name: "group with dots gets sanitized",
			definitions: map[string]*spec.Schema{
				"io.k8s.api.networking.v1.Ingress": schemaWithGVKAndScope("networking.k8s.io", "v1", "Ingress", apiextensionsv1.NamespaceScoped),
			},
			want: []expectedResource{{
				key:            "io.k8s.api.networking.v1.Ingress",
				gvk:            schema.GroupVersionKind{Group: "networking.k8s.io", Version: "v1", Kind: "Ingress"},
				scope:          apiextensionsv1.NamespaceScoped,
				singularName:   "Ingress",
				pluralName:     "Ingresses",
				sanitizedGroup: "networking_k8s_io",
			}},
		},
		{
			name: "cluster-scoped resource",
			definitions: map[string]*spec.Schema{
				"io.k8s.api.core.v1.Namespace": schemaWithGVKAndScope("", "v1", "Namespace", apiextensionsv1.ClusterScoped),
			},
			want: []expectedResource{{
				key:            "io.k8s.api.core.v1.Namespace",
				gvk:            schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"},
				scope:          apiextensionsv1.ClusterScoped,
				singularName:   "Namespace",
				pluralName:     "Namespaces",
				sanitizedGroup: "",
			}},
		},
		{
			name: "group starting with number gets underscore prefix",
			definitions: map[string]*spec.Schema{
				"io.example.1password.v1.Secret": schemaWithGVKAndScope("1password.com", "v1", "Secret", apiextensionsv1.NamespaceScoped),
			},
			want: []expectedResource{{
				key:            "io.example.1password.v1.Secret",
				gvk:            schema.GroupVersionKind{Group: "1password.com", Version: "v1", Kind: "Secret"},
				scope:          apiextensionsv1.NamespaceScoped,
				singularName:   "Secret",
				pluralName:     "Secrets",
				sanitizedGroup: "_1password_com",
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &SchemaGenerator{definitions: tt.definitions}
			got := g.parseResources()

			require.Len(t, got, len(tt.want))
			for i, want := range tt.want {
				assert.Equal(t, want.key, got[i].Key)
				assert.Equal(t, want.gvk, got[i].GVK)
				assert.Equal(t, want.scope, got[i].Scope)
				assert.Equal(t, want.singularName, got[i].SingularName)
				assert.Equal(t, want.pluralName, got[i].PluralName)
				assert.Equal(t, want.sanitizedGroup, got[i].SanitizedGroup)
			}
		})
	}
}

// schemaWithGVK creates a schema with GVK extension only.
func schemaWithGVK(group, version, kind string) *spec.Schema {
	return &spec.Schema{
		VendorExtensible: spec.VendorExtensible{
			Extensions: spec.Extensions{
				pmgateway.GVKExtensionKey: []any{
					map[string]any{"group": group, "version": version, "kind": kind},
				},
			},
		},
	}
}

// schemaWithGVKAndScope creates a schema with both GVK and scope extensions.
func schemaWithGVKAndScope(group, version, kind string, scope apiextensionsv1.ResourceScope) *spec.Schema {
	return &spec.Schema{
		VendorExtensible: spec.VendorExtensible{
			Extensions: spec.Extensions{
				pmgateway.GVKExtensionKey:   []any{map[string]any{"group": group, "version": version, "kind": kind}},
				pmgateway.ScopeExtensionKey: string(scope),
			},
		},
	}
}

func schemaWithCategory(
	group, version, kind string,
	scope apiextensionsv1.ResourceScope,
	categories ...string,
) *spec.Schema {
	cat := make([]any, 0, len(categories))
	for _, v := range categories {
		cat = append(cat, v)
	}

	return &spec.Schema{
		VendorExtensible: spec.VendorExtensible{
			Extensions: spec.Extensions{
				pmgateway.GVKExtensionKey:        []any{map[string]any{"group": group, "version": version, "kind": kind}},
				pmgateway.ScopeExtensionKey:      scope,
				pmgateway.CategoriesExtensionKey: cat,
			},
		},
		SchemaProps: spec.SchemaProps{
			Type: spec.StringOrArray{"object"},
			Properties: map[string]spec.Schema{
				"metadata": {
					SchemaProps: spec.SchemaProps{
						Type: spec.StringOrArray{"object"},
						Properties: map[string]spec.Schema{
							"name": {
								SchemaProps: spec.SchemaProps{
									Type: spec.StringOrArray{"string"},
								},
							},
						},
					},
				},
			},
		},
	}
}
