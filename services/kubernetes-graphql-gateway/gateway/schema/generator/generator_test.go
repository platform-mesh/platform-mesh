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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmgateway "go.platform-mesh.io/apis/gateway"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

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
