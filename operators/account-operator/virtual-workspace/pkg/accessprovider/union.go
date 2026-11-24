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

package accessprovider

import (
	"context"
	"sort"

	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/graph"
)

// Union answers from several providers and merges what they return: a
// cluster any of them lists is listed, once.
//
// It fails closed. If one provider is not ready or returns an error, so does
// the union; a partial answer would make workspaces disappear silently. That
// includes a provider that refuses a request for its shape: with a provider
// that needs a scoped request among them, every request has to be scoped.
type Union []AccessProvider

var _ AccessProvider = Union{}

// Ready reports whether every provider is ready.
func (u Union) Ready() bool {
	for _, p := range u {
		if !p.Ready() {
			return false
		}
	}

	return true
}

// ClustersFor returns the clusters of all providers, sorted by cluster name.
// Where two providers list the same cluster, the entry of the first one
// stands, completed with the account if only a later one knows it.
func (u Union) ClustersFor(ctx context.Context, id Identity, cluster string) ([]graph.AccessEndpointSlice, error) {
	index := map[string]int{}
	out := []graph.AccessEndpointSlice{}

	for _, p := range u {
		clusters, err := p.ClustersFor(ctx, id, cluster)
		if err != nil {
			return nil, err
		}

		for _, c := range clusters {
			i, seen := index[c.ClusterName]
			if !seen {
				index[c.ClusterName] = len(out)
				out = append(out, c)
				continue
			}
			if out[i].Account == nil {
				out[i].Account = c.Account
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ClusterName < out[j].ClusterName })

	return out, nil
}
