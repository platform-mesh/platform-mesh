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

package accessprovider

import (
	"context"
	"reflect"
	"testing"

	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/graph"
)

func TestGraphAdapter_NarrowsToTheScopeOfTheIdentity(t *testing.T) {
	t.Parallel()

	const (
		clusterNameKey = "authentication.kcp.io/cluster-name"
		scopesKey      = "authentication.kcp.io/scopes"
		sa             = "system:serviceaccount:default:default"
	)

	g := graph.New()
	for _, cluster := range []graph.LogicalCluster{"one", "two", "three"} {
		// Every workspace binds its own default/default, a user and a group
		// of the same name.
		g.Grant(graph.User(sa), cluster, "https://kcp/clusters/"+string(cluster))
		g.Grant(graph.User("alice"), cluster, "https://kcp/clusters/"+string(cluster))
		g.Grant(graph.Group("devs"), cluster, "https://kcp/clusters/"+string(cluster))
	}

	tests := []struct {
		name string
		id   Identity
		want []string
	}{
		{
			name: "global user",
			id:   Identity{Name: "alice"},
			want: []string{"one", "three", "two"},
		},
		{
			name: "global user by group",
			id:   Identity{Name: "bob", Groups: []string{"devs"}},
			want: []string{"one", "three", "two"},
		},
		{
			name: "service account in its own cluster only",
			id:   Identity{Name: sa, Extra: map[string][]string{clusterNameKey: {"two"}}},
			want: []string{"two"},
		},
		{
			name: "service account of a cluster that is not indexed",
			id:   Identity{Name: sa, Extra: map[string][]string{clusterNameKey: {"other"}}},
			want: []string{},
		},
		{
			name: "service account without a cluster",
			id:   Identity{Name: sa},
			want: []string{},
		},
		{
			name: "user scoped to one cluster",
			id:   Identity{Name: "alice", Extra: map[string][]string{scopesKey: {"cluster:one"}}},
			want: []string{"one"},
		},
		{
			name: "user scoped to one of two clusters",
			id:   Identity{Name: "alice", Extra: map[string][]string{scopesKey: {"cluster:one,cluster:three"}}},
			want: []string{"one", "three"},
		},
		{
			name: "scopes are intersected",
			id:   Identity{Name: "alice", Groups: []string{"devs"}, Extra: map[string][]string{scopesKey: {"cluster:one,cluster:three", "cluster:three"}}},
			want: []string{"three"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clusters, err := GraphAdapter{Graph: g}.ClustersFor(context.Background(), tc.id, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got := make([]string, 0, len(clusters))
			for _, c := range clusters {
				got = append(got, c.ClusterName)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("clusters = %v, want %v", got, tc.want)
			}
		})
	}
}
