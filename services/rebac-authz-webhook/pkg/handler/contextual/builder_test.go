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

func TestBuildCheckInputMatchesModelObjectType(t *testing.T) {
	tests := []struct{ group, plural, singular, objectType string }{
		{"generators.external-secrets.io", "beyondtrustworkloadcredentialsdynamicsecrets", "beyondtrustworkloadcredentialsdynamicsecret", "_beyondtrustworkloadcredentialsdynamicsecret"},
		{"rbac.authorization.k8s.io", "policies", "policy", "rbac_authorization_k8s_io_policy"},
		{"", "pods", "pod", "core_pod"},
	}
	for _, test := range tests {
		t.Run(test.plural, func(t *testing.T) {
			gv := schema.GroupVersion{Group: test.group, Version: "v1"}
			mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{gv})
			mapper.AddSpecific(gv.WithKind("Example"), gv.WithResource(test.plural), gv.WithResource(test.singular), meta.RESTScopeRoot)
			_, objectType := contextual.BuildObjectType(gv.WithResource(test.plural), test.singular)
			require.Equal(t, test.objectType, objectType)
			for _, verb := range []string{"get", "update", "delete", "patch", "bind"} {
				attrs := &authorizationv1.ResourceAttributes{Group: test.group, Version: "v1", Resource: test.plural, Verb: verb, Name: "example"}
				input, err := contextual.BuildCheckInput(attrs, "alice", "tenant", testClusterInfo(mapper))
				require.NoError(t, err)
				require.Equal(t, test.objectType+":tenant/example", input.Object)
				require.Equal(t, verb, input.Relation)
				require.Equal(t, input.Object, input.ContextualTuples[0].Object)
			}
			for _, verb := range []string{"create", "list", "watch"} {
				attrs := &authorizationv1.ResourceAttributes{Group: test.group, Version: "v1", Resource: test.plural, Verb: verb}
				input, err := contextual.BuildCheckInput(attrs, "alice", "tenant", testClusterInfo(mapper))
				require.NoError(t, err)
				require.LessOrEqual(t, len(input.Relation), 50)
			}
		})
	}
}
