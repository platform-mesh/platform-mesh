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

package proxy

import (
	"context"
	"fmt"

	"github.com/kcp-dev/logicalcluster/v3"

	"github.com/kcp-dev/kcp/pkg/server/filters"
	kcpclientset "github.com/kcp-dev/sdk/client/clientset/versioned/cluster"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/endpoints/request"
)

// ClusterResolvers are capable of resolving a workspace path like "root:foo:bar"
// into the logicalcluster name (e.g. "23jgh234hgf44").
type ClusterResolver func(ctx context.Context, path logicalcluster.Path) (request.Cluster, error)

// NewClusterResolver returns a new resolver that doesn't do any work itself, but
// instead delegates to the kcp server (shard) to perform the resolving, by
// fetching the workspace's "cluster" logicalcluster. Effectively this makes
// kcp's localproxy do the resolving for us.
func NewClusterResolver(clusterClient kcpclientset.ClusterInterface) ClusterResolver {
	return func(ctx context.Context, path logicalcluster.Path) (request.Cluster, error) {
		cluster := request.Cluster{
			PartialMetadataRequest: filters.IsPartialMetadataRequest(ctx),
			Wildcard:               path == logicalcluster.Wildcard,
		}

		if path.Empty() || cluster.Wildcard {
			return cluster, nil
		}

		if !path.IsValid() {
			return cluster, fmt.Errorf("invalid cluster: %q does not match the regex", path)
		}

		if name, isName := path.Name(); isName {
			cluster.Name = name
			return cluster, nil
		}

		lc, err := clusterClient.CoreV1alpha1().LogicalClusters().Cluster(path).Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil {
			return request.Cluster{}, err
		}

		cluster.Name = logicalcluster.From(lc)

		return cluster, nil
	}
}
