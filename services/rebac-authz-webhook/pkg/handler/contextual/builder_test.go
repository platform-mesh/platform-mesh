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

package contextual_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/rebac-authz-webhook/pkg/clustercache"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/handler/contextual"

	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// rbacMapper returns a RESTMapper for a resource in rbac.authorization.k8s.io.
// RBAC names are validated as path segments rather than DNS subdomains, so
// they may legitimately contain colons -- ClusterRole "system:controller:foo"
// and Role "system::leader-locking-kube-controller-manager" are both real.
func rbacMapper(kind, plural, singular string, scope meta.RESTScope) meta.RESTMapper {
	rm := meta.NewDefaultRESTMapper([]schema.GroupVersion{})
	gv := schema.GroupVersion{Group: "rbac.authorization.k8s.io", Version: "v1"}
	rm.AddSpecific(
		gv.WithKind(kind),
		gv.WithResource(plural),
		gv.WithResource(singular),
		scope,
	)
	return rm
}

// clusterRoleMapper knows about ClusterRole, which is cluster scoped.
func clusterRoleMapper() meta.RESTMapper {
	return rbacMapper("ClusterRole", "clusterroles", "clusterrole", meta.RESTScopeRoot)
}

// roleMapper knows about Role, which is namespaced.
func roleMapper() meta.RESTMapper {
	return rbacMapper("Role", "roles", "role", meta.RESTScopeNamespace)
}

func testClusterInfo(rm meta.RESTMapper) clustercache.ClusterInfo {
	return clustercache.ClusterInfo{
		StoreID:         "store-id",
		RESTMapper:      rm,
		AccountName:     "origin-account",
		ParentClusterID: "origin",
	}
}

// assertSingleColon encodes OpenFGA's rule for object keys: an object must
// contain exactly one colon, the separator between type and identifier.
// A resource name that leaks a raw colon into the key makes OpenFGA reject
// the whole Check request.
func assertSingleColon(t *testing.T, object string) {
	t.Helper()
	assert.Equalf(t, 1, strings.Count(object, ":"),
		"object %q must contain exactly one colon to be accepted by OpenFGA", object)
}

func TestBuildCheckInput_ColonInResourceName(t *testing.T) {
	t.Run("cluster scoped object encodes the colon", func(t *testing.T) {
		attrs := &authorizationv1.ResourceAttributes{
			Group:    "rbac.authorization.k8s.io",
			Version:  "v1",
			Resource: "clusterroles",
			Verb:     "get",
			Name:     "system:controller:foo",
		}

		in, err := contextual.BuildCheckInput(attrs, "alice", "cluster-a", testClusterInfo(clusterRoleMapper()))
		require.NoError(t, err)

		assert.Equal(t,
			"rbac_authorization_k8s_io_clusterrole:cluster-a/system%3Acontroller%3Afoo",
			in.Object)
		assertSingleColon(t, in.Object)

		require.Len(t, in.ContextualTuples, 1)
		assert.Equal(t,
			"rbac_authorization_k8s_io_clusterrole:cluster-a/system%3Acontroller%3Afoo",
			in.ContextualTuples[0].Object)
		assertSingleColon(t, in.ContextualTuples[0].Object)
		assert.Equal(t, "core_platform-mesh_io_account:origin/origin-account", in.ContextualTuples[0].User)
	})

	t.Run("namespaced object encodes the colon in its parent tuple", func(t *testing.T) {
		attrs := &authorizationv1.ResourceAttributes{
			Group:     "rbac.authorization.k8s.io",
			Version:   "v1",
			Resource:  "roles",
			Verb:      "get",
			Name:      "system::leader-locking-kube-controller-manager",
			Namespace: "kube-system",
		}

		in, err := contextual.BuildCheckInput(attrs, "alice", "cluster-a", testClusterInfo(roleMapper()))
		require.NoError(t, err)

		const wantObject = "rbac_authorization_k8s_io_role:cluster-a/system%3A%3Aleader-locking-kube-controller-manager"

		assert.Equal(t, wantObject, in.Object)
		assertSingleColon(t, in.Object)

		// The namespace is parented to the account, and the role to the
		// namespace. Only the second tuple carries the resource name.
		require.Len(t, in.ContextualTuples, 2)

		assert.Equal(t, "core_namespace:cluster-a/kube-system", in.ContextualTuples[0].Object)
		assert.Equal(t, "core_platform-mesh_io_account:origin/origin-account", in.ContextualTuples[0].User)

		assert.Equal(t, wantObject, in.ContextualTuples[1].Object)
		assert.Equal(t, "core_namespace:cluster-a/kube-system", in.ContextualTuples[1].User)
		assertSingleColon(t, in.ContextualTuples[1].Object)
	})

	t.Run("colon free name is unchanged", func(t *testing.T) {
		attrs := &authorizationv1.ResourceAttributes{
			Group:    "rbac.authorization.k8s.io",
			Version:  "v1",
			Resource: "clusterroles",
			Verb:     "get",
			Name:     "cluster-admin",
		}

		in, err := contextual.BuildCheckInput(attrs, "alice", "cluster-a", testClusterInfo(clusterRoleMapper()))
		require.NoError(t, err)

		assert.Equal(t,
			"rbac_authorization_k8s_io_clusterrole:cluster-a/cluster-admin",
			in.Object)
	})
}

// The expected names are shared with golang-commons, the security-operator and
// iam-service tests (platform-mesh#436).
func TestBuildCheckInput_Names(t *testing.T) {
	tests := []struct {
		group, kind, plural, singular, verb string
		wantObject, wantRelation            string
	}{
		{"alpha.inventory.platform-mesh.example.io", "Widget", "widgets", "widget", "get",
			"alpha_inventory_platform-mesh_example_io_widget:cluster-a/foo", "get"},
		{"alpha.inventory.platform-mesh.example.io", "Widget", "widgets", "widget", "create",
			"core_platform-mesh_io_account:origin/origin-account", "create_alpha_inventory_p_widgets__5b19f5b12c13bbb9"},
		{"bravo.inventory.platform-mesh.example.io", "Widget", "widgets", "widget", "get",
			"bravo_inventory_platform-mesh_example_io_widget:cluster-a/foo", "get"},
		{"bravo.inventory.platform-mesh.example.io", "Widget", "widgets", "widget", "list",
			"core_platform-mesh_io_account:origin/origin-account", "list_bravo_inventory_p_widgets__ec8debda4e234e1a"},
		{"security.a-long-provider-name.example.io", "Policy", "policies", "policy", "get",
			"security_a-long-provider-name_example_io_policy:cluster-a/foo", "get"},
		{"security.a-long-provider-name.example.io", "Sheep", "sheep", "sheep", "update",
			"security_a-long-provider-name_example_io_sheep:cluster-a/foo", "update"},
		{"generators.external-secrets.io", "BeyondtrustWorkloadCredentialsDynamicSecret", "beyondtrustworkloadcredentialsdynamicsecrets", "beyondtrustworkloadcredentialsdynamicsecret", "watch",
			"core_platform-mesh_io_account:origin/origin-account", "watch_generato_beyondtrustworkl__ef326e0d811ac608"},
		{"rbac.authorization.k8s.io", "ClusterRoleBinding", "clusterrolebindings", "clusterrolebinding", "delete",
			"rbac_authorization_k8s_io_clusterrolebinding:cluster-a/foo", "delete"},
		{"rbac.authorization.k8s.io", "ClusterRoleBinding", "clusterrolebindings", "clusterrolebinding", "create",
			"core_platform-mesh_io_account:origin/origin-account", "create_rbac_aut_clusterrolebindi__30f40171639198c5"},
		{"rbac.authorization.k8s.io", "ClusterRole", "clusterroles", "clusterrole", "watch",
			"core_platform-mesh_io_account:origin/origin-account", "watch_rbac_authorization_k8s_io_clusterroles"},
		{"authorization.k8s.io", "SelfSubjectAccessReview", "selfsubjectaccessreviews", "selfsubjectaccessreview", "create",
			"core_platform-mesh_io_account:origin/origin-account", "create_authoriz_selfsubjectacces__bbffa37626ea5bb0"},
		{"authorization.k8s.io", "SelfSubjectRulesReview", "selfsubjectrulesreviews", "selfsubjectrulesreview", "create",
			"core_platform-mesh_io_account:origin/origin-account", "create_authoriz_selfsubjectrules__6bfa51ea938fdaae"},
		{"authorization.k8s.io", "LocalSubjectAccessReview", "localsubjectaccessreviews", "localsubjectaccessreview", "create",
			"core_platform-mesh_io_account:origin/origin-account", "create_authoriz_localsubjectacce__f9b8e0bd09e04636"},
	}

	for _, tt := range tests {
		t.Run(tt.verb+"/"+tt.group+"/"+tt.plural, func(t *testing.T) {
			rm := meta.NewDefaultRESTMapper([]schema.GroupVersion{})
			gv := schema.GroupVersion{Group: tt.group, Version: "v1"}
			rm.AddSpecific(gv.WithKind(tt.kind), gv.WithResource(tt.plural), gv.WithResource(tt.singular), meta.RESTScopeRoot)

			attrs := &authorizationv1.ResourceAttributes{
				Group:    tt.group,
				Version:  "v1",
				Resource: tt.plural,
				Verb:     tt.verb,
				Name:     "foo",
			}

			in, err := contextual.BuildCheckInput(attrs, "alice", "cluster-a", testClusterInfo(rm))
			require.NoError(t, err)
			assert.Equal(t, tt.wantObject, in.Object)
			assert.Equal(t, tt.wantRelation, in.Relation)
		})
	}
}
