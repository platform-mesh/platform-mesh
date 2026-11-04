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

package subroutine

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"text/template"

	language "github.com/openfga/language/pkg/go/transformer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
)

func render(t *testing.T, tpl *template.Template, in modelInput) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, tpl.Execute(&buf, in))
	return buf.String()
}

// Names that fit the relation length limit are not shortened.
func TestModelInputUnchangedWhenItFits(t *testing.T) {
	tests := []struct {
		group, plural, singular string
	}{
		{"", "namespaces", "namespace"},
		{"", "configmaps", "configmap"},
		{"", "serviceaccounts", "serviceaccount"},
		{"rbac.authorization.k8s.io", "clusterroles", "clusterrole"},
		{"rbac.authorization.k8s.io", "roles", "role"},
		{"rbac.authorization.k8s.io", "rolebindings", "rolebinding"},
		{"authentication.k8s.io", "tokenreviews", "tokenreview"},
		{"authentication.k8s.io", "selfsubjectreviews", "selfsubjectreview"},
		{"authorization.k8s.io", "subjectaccessreviews", "subjectaccessreview"},
		{"apis.kcp.io", "apibindings", "apibinding"},
		{"apis.kcp.io", "apiexports", "apiexport"},
		{"ui.platform-mesh.io", "contentconfigurations", "contentconfiguration"},
		{"orchestrate.platform-mesh.io", "httpbins", "httpbin"},
	}

	for _, tt := range tests {
		t.Run(tt.group+"/"+tt.plural, func(t *testing.T) {
			in, err := newModelInput(tt.group, tt.plural, tt.singular, "Cluster")
			require.NoError(t, err)

			group := tt.group
			if group == "" {
				group = "core"
			}
			g := strings.ReplaceAll(group, ".", "_")
			assert.Equal(t, g+"_"+tt.singular, in.TypeName)
			assert.Equal(t, "create_"+g+"_"+tt.plural, in.CreateRelation)
			assert.Equal(t, "list_"+g+"_"+tt.plural, in.ListRelation)
			assert.Equal(t, "watch_"+g+"_"+tt.plural, in.WatchRelation)
		})
	}
}

func TestModelInputNames(t *testing.T) {
	tests := []struct {
		group, plural, singular string
		typeName, create        string
	}{
		{"alpha.inventory.platform-mesh.example.io", "widgets", "widget",
			"alpha_inventory_platform-mesh_example_io_widget", "create_alpha_inventory_p_widgets__5b19f5b12c13bbb9"},
		{"bravo.inventory.platform-mesh.example.io", "widgets", "widget",
			"bravo_inventory_platform-mesh_example_io_widget", "create_bravo_inventory_p_widgets__ec8debda4e234e1a"},
		{"security.a-long-provider-name.example.io", "policies", "policy",
			"security_a-long-provider-name_example_io_policy", "create_security_a-long_policies__46cd28905cdbba96"},
		{"generators.external-secrets.io", "beyondtrustworkloadcredentialsdynamicsecrets", "beyondtrustworkloadcredentialsdynamicsecret",
			"generators_external-secrets_io_beyondtrustworkloadcredentialsdynamicsecret", "create_generato_beyondtrustworkl__ef326e0d811ac608"},
		{"rbac.authorization.k8s.io", "clusterrolebindings", "clusterrolebinding",
			"rbac_authorization_k8s_io_clusterrolebinding", "create_rbac_aut_clusterrolebindi__30f40171639198c5"},
		{"authorization.k8s.io", "selfsubjectaccessreviews", "selfsubjectaccessreview",
			"authorization_k8s_io_selfsubjectaccessreview", "create_authoriz_selfsubjectacces__bbffa37626ea5bb0"},
		{"authorization.k8s.io", "selfsubjectrulesreviews", "selfsubjectrulesreview",
			"authorization_k8s_io_selfsubjectrulesreview", "create_authoriz_selfsubjectrules__6bfa51ea938fdaae"},
		{"authorization.k8s.io", "localsubjectaccessreviews", "localsubjectaccessreview",
			"authorization_k8s_io_localsubjectaccessreview", "create_authoriz_localsubjectacce__f9b8e0bd09e04636"},
	}

	for _, tt := range tests {
		t.Run(tt.group+"/"+tt.plural, func(t *testing.T) {
			in, err := newModelInput(tt.group, tt.plural, tt.singular, "Cluster")
			require.NoError(t, err)
			assert.Equal(t, tt.typeName, in.TypeName)
			assert.Equal(t, tt.create, in.CreateRelation)
		})
	}
}

const namesTestCoreModule = `module core

type user

type role
  relations
    define assignee: [user, user:*]

type core_platform-mesh_io_account
  relations
    define owner: [role#assignee]
    define member: [role#assignee] or owner

type core_namespace
  relations
    define parent: [core_platform-mesh_io_account]
    define owner: [role#assignee] or owner from parent
    define member: [role#assignee] or owner or member from parent
`

func TestCollidingProvidersBuildOneModel(t *testing.T) {
	modules := make([]language.ModuleFile, 0, 5)
	modules = append(modules, language.ModuleFile{Name: "core.fga", Contents: namesTestCoreModule})
	for _, group := range []string{"alpha.inventory.platform-mesh.example.io", "bravo.inventory.platform-mesh.example.io"} {
		for _, scope := range []string{"Cluster", "Namespaced"} {
			in, err := newModelInput(group, "widgets"+scope, "widget"+scope, scope)
			require.NoError(t, err)
			in.Watch = "owner"
			modules = append(modules, language.ModuleFile{Name: fmt.Sprintf("%s-%s.fga", group, scope), Contents: render(t, modelTpl, in)})
		}
	}

	_, err := transformModules(modules)
	require.NoError(t, err)
}

func TestTransformModulesNamesConflictingFile(t *testing.T) {
	in, err := newModelInput("example.io", "widgets", "widget", "Cluster")
	require.NoError(t, err)
	widget := render(t, modelTpl, in)

	_, err = transformModules([]language.ModuleFile{
		{Name: "core.fga", Contents: namesTestCoreModule},
		{Name: "a.fga", Contents: widget},
		{Name: "b.fga", Contents: widget},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "b.fga: duplicate type definition example_io_widget")
	// Which of the two extensions is reported depends on map order.
	assert.Regexp(t, `[ab]\.fga: relation create_example_io_widgets already exists on type core_platform-mesh_io_account`, err.Error())
}

func TestCheckRelationNames(t *testing.T) {
	long := strings.Repeat("r", 51)

	require.NoError(t, checkRelationNames(modelInput{
		AdditionalPermissions: map[string]string{strings.Repeat("r", 50): "owner"},
		Roles:                 []pmprovidersv1alpha1.RoleDefinition{{ID: "viewer"}},
	}))
	assert.ErrorContains(t, checkRelationNames(modelInput{AdditionalPermissions: map[string]string{long: "owner"}}), "exceeds 50 characters")
	assert.ErrorContains(t, checkRelationNames(modelInput{Roles: []pmprovidersv1alpha1.RoleDefinition{{ID: long}}}), "exceeds 50 characters")
}
