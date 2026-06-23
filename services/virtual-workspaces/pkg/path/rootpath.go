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

package path

import (
	"context"
	"strings"

	"github.com/kcp-dev/kcp/pkg/server/filters"
	"github.com/kcp-dev/logicalcluster/v3"
	"github.com/kcp-dev/virtual-workspace-framework/framework"
	"go.platform-mesh.io/virtual-workspaces/pkg/authorization"
	"go.platform-mesh.io/virtual-workspaces/pkg/proxy"
	"go.platform-mesh.io/virtual-workspaces/pkg/storage"

	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/klog/v2"
)

func NewPathResolver(clusterResolver proxy.ClusterResolver, virtualWorkspaceBaseURL string) framework.RootPathResolverFunc {
	return func(urlPath string, requestContext context.Context) (accepted bool, prefixToStrip string, completedContext context.Context) {
		// Supported URLs look like this: /services/service/clusters/<cluster>/api…
		klog.V(8).InfoS("handling request in pathResolver", "urlPath", urlPath)

		// Ignore anything that does not target this virtual workspace.
		if !strings.HasPrefix(urlPath, virtualWorkspaceBaseURL) {
			return false, "", requestContext
		}

		// trim the common prefix ("/services/service")
		relPath := strings.TrimPrefix(urlPath, virtualWorkspaceBaseURL)

		// URL should now be /clusters/<path>/api…

		// begin to find cluster name
		clustersPrefix := "/clusters/"
		if !strings.HasPrefix(relPath, clustersPrefix) {
			return false, "", requestContext
		}

		relPath = strings.TrimPrefix(relPath, clustersPrefix)

		// URL should now be <cluster>/api…

		// extract cluster name
		parts := strings.SplitN(relPath, "/", 2)
		path := logicalcluster.NewPath(parts[0])

		// determine cluster-local request URL
		realPath := "/"
		if len(parts) > 1 {
			realPath += parts[1]
		}

		// setup cluster in completed context
		var cluster genericapirequest.Cluster
		if path == logicalcluster.Wildcard {
			cluster = genericapirequest.Cluster{
				PartialMetadataRequest: filters.IsPartialMetadataRequest(requestContext),
				Wildcard:               true,
			}
		} else {
			var err error
			cluster, err = clusterResolver(requestContext, path)
			if err != nil {
				return false, "", requestContext
			}

		}

		completedContext = storage.WithClusterPath(requestContext, path)
		completedContext = genericapirequest.WithCluster(completedContext, cluster)

		// Inject a dummy object into the context which later is filled with real
		// data during the authorization process; this allows two function side-by-side
		// to share data in the same context.
		completedContext = authorization.WithAttributeHolder(completedContext)

		return true, strings.TrimSuffix(urlPath, realPath), completedContext
	}
}
