//go:build e2e

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

package deployer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/platform-mesh-deployer/pkg/names"
	"go.platform-mesh.io/tests/e2e/deployer/suite"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kcp-dev/kcp-operator/sdk/apis/operator/v1alpha1"
)

// TestInCluster builds a kcp nobody outside the cluster can reach: no ingress
// stack and no exposure anywhere. It is the shape an installer produces from an
// otherwise empty PlatformMesh, and the one a kind cluster without an ingress
// can actually run.
func TestInCluster(t *testing.T) {
	t.Parallel()
	env := suite.Start(t, 0)
	env.EngageWorkload(t, "customer-a", env.Config, "rootshard", "frontproxy", "shards-default")

	createInClusterPlatformMesh(t, env.Config.Client, env.EtcdEndpoint())

	// Every component advertises the Service kcp-operator gave it, so nothing
	// had to invent a hostname or back one with an alias.
	ns := suite.ProviderNamespace
	rootName := names.RootShard(suite.PlatformMeshName, "root", env.Config.NodeIP)
	fpName := names.FrontProxy(suite.PlatformMeshName, "fp", env.Config.NodeIP)

	var rs operatorv1alpha1.RootShard
	require.Eventually(t, func() bool {
		return env.Config.Client.Get(t.Context(), ctrlruntimeclient.ObjectKey{Namespace: ns, Name: rootName}, &rs) == nil
	}, 5*time.Minute, 2*time.Second, "deployer did not create the RootShard admin CR")
	require.Equal(t, fpName+"-front-proxy."+ns+".svc", rs.Spec.External.Hostname)
	require.Equal(t, "https://"+rootName+"-kcp."+ns+".svc:6443", rs.Spec.ShardBaseURL)

	env.VerifyKcp(t, env.Config, env.Config, 2)
}
