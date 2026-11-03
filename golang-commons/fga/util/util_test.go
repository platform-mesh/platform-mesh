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

package util

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	// Identifier rules of the OpenFGA DSL plus the API length limits.
	relationPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]{0,49}$`)
	typePattern     = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,254}$`)
)

func TestConvertToTypeName(t *testing.T) {
	tests := []struct {
		group    string
		singular string
		expected string
	}{
		{"apps", "deployment", "apps_deployment"},
		{"", "pod", "core_pod"},
		{"", "Namespace", "core_namespace"},
		{"networking.k8s.io", "ingress", "networking_k8s_io_ingress"},
		{"Apps", "Deployment", "apps_deployment"},
		{"group-with-dashes", "kind", "group-with-dashes_kind"},
		{"", "", "core_"},
		{"foo.bar.baz", "resource", "foo_bar_baz_resource"},
		// Shortened by the previous algorithm, never truncated now.
		{"rbac.authorization.k8s.io", "clusterrolebinding", "rbac_authorization_k8s_io_clusterrolebinding"},
		{"authorization.k8s.io", "selfsubjectaccessreview", "authorization_k8s_io_selfsubjectaccessreview"},
		{"authorization.k8s.io", "selfsubjectrulesreview", "authorization_k8s_io_selfsubjectrulesreview"},
		{"authorization.k8s.io", "localsubjectaccessreview", "authorization_k8s_io_localsubjectaccessreview"},
		{"alpha.inventory.platform-mesh.example.io", "widget", "alpha_inventory_platform-mesh_example_io_widget"},
		{"bravo.inventory.platform-mesh.example.io", "widget", "bravo_inventory_platform-mesh_example_io_widget"},
		{"security.a-long-provider-name.example.io", "Policy", "security_a-long-provider-name_example_io_policy"},
		{"security.a-long-provider-name.example.io", "sheep", "security_a-long-provider-name_example_io_sheep"},
		{"generators.external-secrets.io", "beyondtrustworkloadcredentialsdynamicsecret", "generators_external-secrets_io_beyondtrustworkloadcredentialsdynamicsecret"},
	}

	for _, tt := range tests {
		t.Run(tt.group+"/"+tt.singular, func(t *testing.T) {
			assert.Equal(t, tt.expected, ConvertToTypeName(tt.group, tt.singular))
		})
	}
}

func TestConvertToTypeNameAboveLimit(t *testing.T) {
	group := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 61)
	singular := strings.Repeat("s", 63)

	name := ConvertToTypeName(group, singular)
	assert.Len(t, name, MaxTypeLength)
	assert.Regexp(t, typePattern, name)
	assert.True(t, strings.HasPrefix(name, strings.Repeat("a", 63)+"_"))
	assert.Equal(t, name, ConvertToTypeName(group, singular))
	assert.NotEqual(t, name, ConvertToTypeName(group, singular[:62]+"t"))
}

// Every relation that fits today must stay byte-identical.
func TestConvertToRelationNameUnchangedWhenItFits(t *testing.T) {
	tests := []struct {
		group  string
		plural string
	}{
		{"", "pods"},
		{"", "namespaces"},
		{"", "configmaps"},
		{"", "serviceaccounts"},
		{"apps", "deployments"},
		{"rbac.authorization.k8s.io", "clusterroles"},
		{"rbac.authorization.k8s.io", "roles"},
		{"rbac.authorization.k8s.io", "rolebindings"},
		{"authentication.k8s.io", "tokenreviews"},
		{"authentication.k8s.io", "selfsubjectreviews"},
		{"authorization.k8s.io", "subjectaccessreviews"},
		{"apis.kcp.io", "apibindings"},
		{"apis.kcp.io", "apiexports"},
		{"core.platform-mesh.io", "accounts"},
		{"ui.platform-mesh.io", "contentconfigurations"},
		{"orchestrate.platform-mesh.io", "httpbins"},
		{"inventory.platform-mesh.example.io", "widgets"},
	}

	for _, tt := range tests {
		group := tt.group
		if group == "" {
			group = "core"
		}
		legacySuffix := strings.ReplaceAll(group, ".", "_") + "_" + tt.plural
		for _, verb := range []string{"create", "list", "watch", "get"} {
			t.Run(verb+"/"+tt.group+"/"+tt.plural, func(t *testing.T) {
				relation, err := ConvertToRelationName(verb, tt.group, tt.plural)
				require.NoError(t, err)
				assert.Equal(t, verb+"_"+legacySuffix, relation)
			})
		}
	}

	// Literal golden values for the built-in relations found in every org store.
	golden := map[[3]string]string{
		{"create", "", "pods"}:                                     "create_core_pods",
		{"watch", "rbac.authorization.k8s.io", "clusterroles"}:     "watch_rbac_authorization_k8s_io_clusterroles",
		{"create", "authorization.k8s.io", "subjectaccessreviews"}: "create_authorization_k8s_io_subjectaccessreviews",
		{"list", "core.platform-mesh.io", "accounts"}:              "list_core_platform-mesh_io_accounts",
	}
	for in, want := range golden {
		got, err := ConvertToRelationName(in[0], in[1], in[2])
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestConvertToRelationNameHashed(t *testing.T) {
	tests := []struct {
		group    string
		plural   string
		expected string
	}{
		{"alpha.inventory.platform-mesh.example.io", "widgets", "create_alpha_inventory_p_widgets__5b19f5b12c13bbb9"},
		{"bravo.inventory.platform-mesh.example.io", "widgets", "create_bravo_inventory_p_widgets__ec8debda4e234e1a"},
		{"security.a-long-provider-name.example.io", "policies", "create_security_a-long_policies__46cd28905cdbba96"},
		{"generators.external-secrets.io", "beyondtrustworkloadcredentialsdynamicsecrets", "create_generato_beyondtrustworkl__ef326e0d811ac608"},
		{"rbac.authorization.k8s.io", "clusterrolebindings", "create_rbac_aut_clusterrolebindi__30f40171639198c5"},
		{"authorization.k8s.io", "selfsubjectaccessreviews", "create_authoriz_selfsubjectacces__bbffa37626ea5bb0"},
	}

	for _, tt := range tests {
		t.Run(tt.group+"/"+tt.plural, func(t *testing.T) {
			create, err := ConvertToRelationName("create", tt.group, tt.plural)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, create)

			suffix := strings.TrimPrefix(create, "create_")
			for _, verb := range []string{"create", "list", "watch"} {
				relation, err := ConvertToRelationName(verb, tt.group, tt.plural)
				require.NoError(t, err)
				assert.Regexp(t, relationPattern, relation)
				assert.Equal(t, verb+"_"+suffix, relation)
			}
		})
	}
}

func TestConvertToRelationNameDistinct(t *testing.T) {
	inputs := [][2]string{
		{"alpha.inventory.platform-mesh.example.io", "widgets"},
		{"bravo.inventory.platform-mesh.example.io", "widgets"},
		{"alpha.inventory.platform-mesh.example.io", "gadgets"},
		{"security.a-long-provider-name.example.io", "policies"},
		{"security.a-long-provider-name.example.io", "sheep"},
		{"authorization.k8s.io", "selfsubjectaccessreviews"},
		{"authorization.k8s.io", "selfsubjectrulesreviews"},
		{"authorization.k8s.io", "localsubjectaccessreviews"},
		{"authorization.k8s.io", "subjectaccessreviews"},
		{"rbac.authorization.k8s.io", "clusterrolebindings"},
		{"rbac.authorization.k8s.io", "clusterroles"},
	}

	seen := map[string][2]string{}
	for _, in := range inputs {
		relation, err := ConvertToRelationName("create", in[0], in[1])
		require.NoError(t, err)
		other, dup := seen[relation]
		assert.False(t, dup, "%v and %v both map to %q", in, other, relation)
		seen[relation] = in
	}
}

func TestConvertToRelationNameHashedNeverEqualsUnshortened(t *testing.T) {
	// A valid group and plural chosen so that the unshortened relation spells
	// the shortened relation of another resource without the separator.
	shortened, err := ConvertToRelationName("create", "bravo.inventory.platform-mesh.example.io", "widgets")
	require.NoError(t, err)
	lookalike, err := ConvertToRelationName("create", "bravo.inventory.p.widgets", "ec8debda4e234e1a")
	require.NoError(t, err)
	assert.Equal(t, "create_bravo_inventory_p_widgets_ec8debda4e234e1a", lookalike)
	assert.NotEqual(t, shortened, lookalike)

	hashed := regexp.MustCompile(`__[0-9a-f]{16}$`)
	inputs := [][2]string{
		{"", "pods"},
		{"apps", "deployments"},
		{"inventory.platform-mesh.example.io", "widgets"},
		{"bravo.inventory.p.widgets", "ec8debda4e234e1a"},
		{"bravo.inventory.platform-mesh.example.io", "widgets"},
		{"security.a-long-provider-name.example.io", "sheep"},
		{"generators.external-secrets.io", "beyondtrustworkloadcredentialsdynamicsecrets"},
		{"rbac.authorization.k8s.io", "clusterrolebindings"},
	}
	for _, in := range inputs {
		group := in[0]
		if group == "" {
			group = "core"
		}
		unshortened := "create_" + strings.ReplaceAll(group, ".", "_") + "_" + in[1]
		relation, err := ConvertToRelationName("create", in[0], in[1])
		require.NoError(t, err)
		if relation == unshortened {
			assert.NotContains(t, relation, hashSeparator, "%v", in)
		} else {
			assert.Regexp(t, hashed, relation, "%v", in)
			assert.Equal(t, 1, strings.Count(relation, hashSeparator), "%v", in)
		}
	}
}

func TestConvertToRelationNameLongInputs(t *testing.T) {
	tests := []struct {
		name   string
		group  string
		plural string
	}{
		{"44 character plural", "generators.external-secrets.io", "beyondtrustworkloadcredentialsdynamicsecrets"},
		{"45 character plural on a short group", "example.io", strings.Repeat("abcdefghi", 5)},
		{"60 character plural", "example.io", strings.Repeat("abcdefghij", 6)},
		{"63 character plural", "example.io", strings.Repeat("p", 63)},
		{"253 character group", strings.Repeat(strings.Repeat("g", 63)+".", 3) + strings.Repeat("g", 61), "things"},
		{"empty group", "", strings.Repeat("p", 50)},
		{"empty plural", strings.Repeat("g", 60), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, verb := range []string{"create", "list", "watch"} {
				relation, err := ConvertToRelationName(verb, tt.group, tt.plural)
				require.NoError(t, err)
				assert.Regexp(t, relationPattern, relation)
			}
		})
	}
}

func TestConvertToRelationNameTooLongVerb(t *testing.T) {
	_, err := ConvertToRelationName("deletecollection", "generators.external-secrets.io", "beyondtrustworkloadcredentialsdynamicsecrets")
	assert.ErrorContains(t, err, "exceeds 50 characters")

	relation, err := ConvertToRelationName("deletecollection", "core.platform-mesh.io", "accounts")
	require.NoError(t, err)
	assert.Equal(t, "deletecollection_core_platform-mesh_io_accounts", relation)
}
