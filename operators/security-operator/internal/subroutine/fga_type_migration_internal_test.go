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

	language "github.com/openfga/language/pkg/go/transformer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestLegacyTypeName(t *testing.T) {
	tests := []struct {
		group, plural, singular string
		want                    string
		ok                      bool
	}{
		// Shortened types found in every org store before #436.
		{"rbac.authorization.k8s.io", "clusterrolebindings", "clusterrolebinding", "ac_authorization_k8s_io_clusterrolebinding", true},
		{"authorization.k8s.io", "selfsubjectaccessreviews", "selfsubjectaccessreview", "thorization_k8s_io_selfsubjectaccessreview", true},
		{"authorization.k8s.io", "selfsubjectrulesreviews", "selfsubjectrulesreview", "uthorization_k8s_io_selfsubjectrulesreview", true},
		{"authorization.k8s.io", "localsubjectaccessreviews", "localsubjectaccessreview", "horization_k8s_io_localsubjectaccessreview", true},
		{"alpha.inventory.platform-mesh.example.io", "widgets", "widget", "_inventory_platform-mesh_example_io_widget", true},
		{"bravo.inventory.platform-mesh.example.io", "widgets", "widget", "_inventory_platform-mesh_example_io_widget", true},
		{"security.a-long-provider-name.example.io", "policies", "policy", "ty_a-long-provider-name_example_io_policy", true},
		{"rbac.authorization.k8s.io", "clusterroles", "clusterrole", "rbac_authorization_k8s_io_clusterrole", true},
		{"", "pods", "pod", "core_pod", true},
		// The previous algorithm panicked on these.
		{"generators.external-secrets.io", "beyondtrustworkloadcredentialsdynamicsecrets", "beyondtrustworkloadcredentialsdynamicsecret", "", false},
		{"example.io", "abcdefghijabcdefghijabcdefghijabcdefghijabcde", "abcdefghijabcdefghijabcdefghijabcdefghijabcd", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.group+"/"+tt.plural, func(t *testing.T) {
			got, ok := legacyTypeName(tt.group, tt.plural, tt.singular)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Names of every resource that fit before #436 are unchanged, so only the
// shortened ones are renamed.
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
			legacyGroup := strings.ReplaceAll(group, ".", "_")
			assert.Equal(t, legacyGroup+"_"+tt.singular, in.TypeName)
			assert.Equal(t, "create_"+legacyGroup+"_"+tt.plural, in.CreateRelation)
			assert.Equal(t, "list_"+legacyGroup+"_"+tt.plural, in.ListRelation)
			assert.Equal(t, "watch_"+legacyGroup+"_"+tt.plural, in.WatchRelation)

			_, renamed := legacyRename(tt.group, tt.plural, tt.singular)
			assert.False(t, renamed)
		})
	}
}

func TestModelInputNames(t *testing.T) {
	tests := []struct {
		group, plural, singular string
		typeName, create        string
		legacy                  string
	}{
		{"alpha.inventory.platform-mesh.example.io", "widgets", "widget",
			"alpha_inventory_platform-mesh_example_io_widget", "create_alpha_inventory_p_widgets__5b19f5b12c13bbb9", "_inventory_platform-mesh_example_io_widget"},
		{"bravo.inventory.platform-mesh.example.io", "widgets", "widget",
			"bravo_inventory_platform-mesh_example_io_widget", "create_bravo_inventory_p_widgets__ec8debda4e234e1a", "_inventory_platform-mesh_example_io_widget"},
		{"security.a-long-provider-name.example.io", "policies", "policy",
			"security_a-long-provider-name_example_io_policy", "create_security_a-long_policies__46cd28905cdbba96", "ty_a-long-provider-name_example_io_policy"},
		{"generators.external-secrets.io", "beyondtrustworkloadcredentialsdynamicsecrets", "beyondtrustworkloadcredentialsdynamicsecret",
			"generators_external-secrets_io_beyondtrustworkloadcredentialsdynamicsecret", "create_generato_beyondtrustworkl__ef326e0d811ac608", ""},
		{"rbac.authorization.k8s.io", "clusterrolebindings", "clusterrolebinding",
			"rbac_authorization_k8s_io_clusterrolebinding", "create_rbac_aut_clusterrolebindi__30f40171639198c5", "ac_authorization_k8s_io_clusterrolebinding"},
		{"authorization.k8s.io", "selfsubjectaccessreviews", "selfsubjectaccessreview",
			"authorization_k8s_io_selfsubjectaccessreview", "create_authoriz_selfsubjectacces__bbffa37626ea5bb0", "thorization_k8s_io_selfsubjectaccessreview"},
		{"authorization.k8s.io", "selfsubjectrulesreviews", "selfsubjectrulesreview",
			"authorization_k8s_io_selfsubjectrulesreview", "create_authoriz_selfsubjectrules__6bfa51ea938fdaae", "uthorization_k8s_io_selfsubjectrulesreview"},
		{"authorization.k8s.io", "localsubjectaccessreviews", "localsubjectaccessreview",
			"authorization_k8s_io_localsubjectaccessreview", "create_authoriz_localsubjectacce__f9b8e0bd09e04636", "horization_k8s_io_localsubjectaccessreview"},
	}

	for _, tt := range tests {
		t.Run(tt.group+"/"+tt.plural, func(t *testing.T) {
			in, err := newModelInput(tt.group, tt.plural, tt.singular, "Cluster")
			require.NoError(t, err)
			assert.Equal(t, tt.typeName, in.TypeName)
			assert.Equal(t, tt.create, in.CreateRelation)

			legacy, renamed := legacyRename(tt.group, tt.plural, tt.singular)
			assert.Equal(t, tt.legacy, legacy)
			assert.Equal(t, tt.legacy != "", renamed)
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

			var buf bytes.Buffer
			require.NoError(t, modelTpl.Execute(&buf, in))
			modules = append(modules, language.ModuleFile{Name: fmt.Sprintf("%s-%s.fga", group, scope), Contents: buf.String()})
		}
	}

	require.NoError(t, checkModuleConflicts(modules))
	_, err := language.TransformModuleFilesToModel(modules, schemaVersion)
	require.NoError(t, err)
}

func TestCheckModuleConflicts(t *testing.T) {
	widget := func(group string) string {
		in, err := newModelInput(group, "widgets", "widget", "Cluster")
		require.NoError(t, err)
		var buf bytes.Buffer
		require.NoError(t, modelTpl.Execute(&buf, in))
		return buf.String()
	}

	err := checkModuleConflicts([]language.ModuleFile{
		{Name: "core.fga", Contents: namesTestCoreModule},
		{Name: "a.fga", Contents: widget("example.io")},
		{Name: "b.fga", Contents: widget("example.io")},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "type example_io_widget is defined by both a.fga and b.fga")
	assert.Contains(t, err.Error(), "relation create_example_io_widgets on type core_platform-mesh_io_account is defined by both a.fga and b.fga")
}

func TestTypeRenamesFromAuthorizationModel(t *testing.T) {
	in, err := newModelInput("alpha.inventory.platform-mesh.example.io", "widgets", "widget", "Cluster")
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, modelTpl.Execute(&buf, in))

	model := pmcorev1alpha1.AuthorizationModel{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "alpha",
			Annotations: map[string]string{legacyTypeAnnotation: "_inventory_platform-mesh_example_io_widget"},
		},
		Spec: pmcorev1alpha1.AuthorizationModelSpec{Model: buf.String()},
	}

	renames := typeRenames{}
	require.NoError(t, renames.addAuthorizationModel(model))
	assert.Equal(t, typeRenames{"_inventory_platform-mesh_example_io_widget": {"alpha_inventory_platform-mesh_example_io_widget"}}, renames)

	model.Annotations = nil
	renames = typeRenames{}
	require.NoError(t, renames.addAuthorizationModel(model))
	assert.Empty(t, renames)
}

func TestRenameKey(t *testing.T) {
	renames := typeRenames{"ac_authorization_k8s_io_clusterrolebinding": {"rbac_authorization_k8s_io_clusterrolebinding"}}

	tests := map[string]string{
		"ac_authorization_k8s_io_clusterrolebinding:c/crb":                     "rbac_authorization_k8s_io_clusterrolebinding:c/crb",
		"role:ac_authorization_k8s_io_clusterrolebinding/c/crb/owner":          "role:rbac_authorization_k8s_io_clusterrolebinding/c/crb/owner",
		"role:ac_authorization_k8s_io_clusterrolebinding/c/crb/owner#assignee": "role:rbac_authorization_k8s_io_clusterrolebinding/c/crb/owner#assignee",
		"ac_authorization_k8s_io_clusterrolebinding:c/crb#owner":               "rbac_authorization_k8s_io_clusterrolebinding:c/crb#owner",
		"rbac_authorization_k8s_io_clusterrolebinding:c/crb":                   "rbac_authorization_k8s_io_clusterrolebinding:c/crb",
		"role:core_platform-mesh_io_account/c/acc/owner#assignee":              "role:core_platform-mesh_io_account/c/acc/owner#assignee",
		"role:authenticated#assignee":                                          "role:authenticated#assignee",
		"user:ac_authorization_k8s_io_clusterrolebinding":                      "user:ac_authorization_k8s_io_clusterrolebinding",
	}
	for in, want := range tests {
		assert.Equal(t, want, renameKey(in, renames), in)
	}
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
