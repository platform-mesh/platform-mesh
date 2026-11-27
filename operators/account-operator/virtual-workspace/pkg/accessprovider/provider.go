/*
Copyright 2026 The kcp Authors.

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

// Package accessprovider defines the seams between the access virtual
// workspace and the sources of authorization data: AccessProvider, the query
// side a review is answered through, and GraphPopulator, the push side that
// keeps the in-memory access graph up to date.
package accessprovider

import (
	"context"

	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/graph"

	"k8s.io/apiserver/pkg/authentication/user"
	rbacregistryvalidation "k8s.io/kubernetes/pkg/registry/rbac/validation"

	"github.com/kcp-dev/logicalcluster/v3"
)

// GraphPopulator fills the shared access graph from an authorization source
// whose grants can be watched and enumerated, like the RBAC bindings of kcp.
type GraphPopulator interface {
	Start(ctx context.Context, g *graph.Graph) error
}

// AccessProvider answers the access question per request: the logical
// clusters an identity can access. A source that keeps the access graph up
// to date is served through GraphAdapter; a source that has to be asked per
// request (OpenFGA, other ReBAC systems) implements AccessProvider itself.
// Union merges several.
type AccessProvider interface {
	// Ready reports whether ClustersFor can be answered yet.
	Ready() bool

	// ClustersFor returns the clusters the identity can access, sorted by
	// cluster name. cluster is the workspace (path or logical cluster name)
	// the request was scoped to by its URL,
	// /services/access/clusters/<cluster>, and empty for a request that was
	// not scoped.
	//
	// Errors are returned to the caller as they are, so implementations
	// return API status errors.
	ClustersFor(ctx context.Context, id Identity, cluster string) ([]graph.AccessEndpointSlice, error)
}

// Identity is the caller a review is answered for.
type Identity struct {
	// Name is the username, as kcp authenticated it.
	Name string
	// Groups the user is a member of.
	Groups []string
	// Extra is what the authenticator knows about the user beyond that, e.g.
	// claims of the token; the front proxy forwards it with the identity.
	Extra map[string][]string
}

// IdentityOf returns the identity of an authenticated user.
func IdentityOf(u user.Info) Identity {
	return Identity{Name: u.GetName(), Groups: u.GetGroups(), Extra: u.GetExtra()}
}

// GraphAdapter answers from the in-memory access graph.
//
// The graph spans every indexed cluster and has no notion of a scope, so the
// cluster a request was scoped to does not narrow the answer. What narrows it
// is the identity: the graph is keyed on bare names, while an identity of kcp
// is not always global, see inScope.
type GraphAdapter struct {
	Graph *graph.Graph
}

var _ AccessProvider = GraphAdapter{}

// Ready reports whether the graph completed its initial sync.
func (a GraphAdapter) Ready() bool {
	return a.Graph.Ready()
}

// ClustersFor returns the clusters the user, or one of its groups, was
// granted access to, as far as the identity is valid there.
func (a GraphAdapter) ClustersFor(_ context.Context, id Identity, _ string) ([]graph.AccessEndpointSlice, error) {
	clusters := a.Graph.ClustersFor(id.Name, id.Groups)

	out := clusters[:0]
	for _, c := range clusters {
		if inScope(id, c.ClusterName) {
			out = append(out, c)
		}
	}

	return out, nil
}

// inScope reports whether the identity is the one a binding in the cluster
// names, by the rules kcp authorizes with:
//
//   - a service account exists in one logical cluster, named by the extra
//     authentication.kcp.io/cluster-name. The same namespace and name in
//     another cluster is somebody else. kcp takes a service account without
//     that extra for a local one; here there is no "local", so it is in scope
//     nowhere.
//   - an identity with the extra authentication.kcp.io/scopes, like a user
//     authenticated by the authenticators of one workspace, is valid in the
//     clusters its scopes name only.
func inScope(id Identity, cluster string) bool {
	u := &user.DefaultInfo{Name: id.Name, Groups: id.Groups, Extra: id.Extra}

	if rbacregistryvalidation.IsServiceAccount(u) && len(id.Extra[rbacregistryvalidation.ClusterExtraKey]) == 0 {
		return false
	}

	return rbacregistryvalidation.IsInScope(u, logicalcluster.Name(cluster))
}
