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

	appsv1 "k8s.io/api/apps/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	deployv1alpha1 "github.com/kcp-dev/kcp-operator/sdk/apis/deploy/v1alpha1"
	operatorv1alpha1 "github.com/kcp-dev/kcp-operator/sdk/apis/operator/v1alpha1"
)

// TestSingleCluster runs the full pipeline on one cluster.
func TestSingleCluster(t *testing.T) {
	t.Parallel()
	env := suite.Start(t, 0)
	env.EngageWorkload(t, "customer-a", env.Config, "rootshard", "frontproxy", "shards-default")

	createPlatformMesh(t, env.Config.Client, env.EtcdEndpoint())

	rootName := names.RootShard(suite.PlatformMeshName, "root", env.Config.NodeIP)
	rootShard := ctrlruntimeclient.ObjectKey{Namespace: suite.ProviderNamespace, Name: rootName}

	require.Eventually(t, func() bool {
		return env.Config.Client.Get(t.Context(), rootShard, &operatorv1alpha1.RootShard{}) == nil
	}, 5*time.Minute, 2*time.Second, "deployer did not create the RootShard admin CR")

	require.Eventually(t, func() bool {
		return env.Config.Client.Get(t.Context(), rootShard, &deployv1alpha1.CompiledRootShard{}) == nil
	}, 8*time.Minute, 5*time.Second, "config kcp-operator did not compile the RootShard")
	require.Eventually(t, func() bool {
		key := ctrlruntimeclient.ObjectKey{Namespace: suite.ProviderNamespace, Name: rootName + "-kcp"}
		return env.Config.Client.Get(t.Context(), key, &appsv1.Deployment{}) == nil
	}, 3*time.Minute, 5*time.Second, "workload kcp-operator did not render the root shard Deployment")

	env.VerifyKcp(t, env.Config, env.Config, 2)
}
