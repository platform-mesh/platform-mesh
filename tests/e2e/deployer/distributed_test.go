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

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	deployv1alpha1 "github.com/kcp-dev/kcp-operator/sdk/apis/deploy/v1alpha1"
	operatorv1alpha1 "github.com/kcp-dev/kcp-operator/sdk/apis/operator/v1alpha1"
)

// TestDistributedClusters deploys each component onto its own workload cluster.
func TestDistributedClusters(t *testing.T) {
	t.Parallel()
	env := suite.Start(t, 4)
	rs, sh1, sh2, fp := env.Workloads[0], env.Workloads[1], env.Workloads[2], env.Workloads[3]

	env.EngageWorkload(t, "customer-a", rs, "rootshard")
	env.EngageWorkload(t, "customer-a", sh1, "shards-default")
	env.EngageWorkload(t, "customer-a", sh2, "shards-default")
	env.EngageWorkload(t, "customer-a", fp, "frontproxy")
	env.CopyEtcdClientCert(t, rs)
	env.CopyEtcdClientCert(t, sh1)
	env.CopyEtcdClientCert(t, sh2)
	env.CopyEtcdClientCert(t, fp)

	createPlatformMesh(t, env.Config.Client, env.EtcdEndpoint())

	cases := []struct {
		kind    string
		name    string
		cluster *suite.Cluster
	}{
		{"CompiledRootShard", names.RootShard(suite.PlatformMeshName, "root", rs.NodeIP), rs},
		{"CompiledShard", names.Shard(suite.PlatformMeshName, "default", sh1.NodeIP), sh1},
		{"CompiledShard", names.Shard(suite.PlatformMeshName, "default", sh2.NodeIP), sh2},
		{"CompiledFrontProxy", names.FrontProxy(suite.PlatformMeshName, "fp", fp.NodeIP), fp},
	}
	for _, c := range cases {
		require.Eventuallyf(t, func() bool {
			return compiledExists(t, env.Config.Client, c.kind, c.name)
		}, 15*time.Minute, 5*time.Second, "config kcp-operator did not compile %s %q", c.kind, c.name)

		require.Eventuallyf(t, func() bool {
			return compiledExists(t, c.cluster.Client, c.kind, c.name)
		}, 5*time.Minute, 5*time.Second, "deployer did not copy %s %q to its workload cluster", c.kind, c.name)
	}

	env.VerifyKcp(t, rs, fp, 3)

	verifyKubeconfigRBAC(t, env, rs, fp)
	verifyShardTeardown(t, env, sh2)
}

// verifyKubeconfigRBAC asserts the deployer provisions RBAC inside kcp.
//
// It is the only case that reaches a running kcp from a cluster hosting none of
// it: the Kubeconfigs the other cases mint leave spec.authorization unset, which
// skips this path entirely.
func verifyKubeconfigRBAC(t *testing.T, env *suite.Env, rootShard, frontProxy *suite.Cluster) {
	t.Helper()

	kc := &operatorv1alpha1.Kubeconfig{
		ObjectMeta: metav1.ObjectMeta{Name: "e2e-rbac", Namespace: suite.ProviderNamespace},
		Spec: operatorv1alpha1.KubeconfigSpec{
			Target:          operatorv1alpha1.KubeconfigTarget{FrontProxyRef: &corev1.LocalObjectReference{Name: names.FrontProxy(suite.PlatformMeshName, "fp", frontProxy.NodeIP)}},
			TargetWorkspace: "root",
			Username:        "e2e-rbac",
			Validity:        metav1.Duration{Duration: time.Hour},
			SecretRef:       corev1.LocalObjectReference{Name: "e2e-rbac-kubeconfig"},
			Authorization: &operatorv1alpha1.KubeconfigAuthorization{
				ClusterRoleBindings: operatorv1alpha1.KubeconfigClusterRoleBindings{
					ClusterRoles: []string{"cluster-admin"},
				},
			},
		},
	}
	require.NoError(t, env.Config.Client.Create(t.Context(), kc))

	root := env.WorkspaceClient(t, rootShard, frontProxy, "root")
	require.Eventually(t, func() bool {
		list := &rbacv1.ClusterRoleBindingList{}
		if err := root.List(t.Context(), list, ctrlruntimeclient.MatchingLabels{"operator.kcp.io/kubeconfig": string(kc.UID)}); err != nil {
			t.Logf("listing ClusterRoleBindings in root: %v", err)
			return false
		}
		return len(list.Items) == 1
	}, 5*time.Minute, 5*time.Second, "deployer did not provision the Kubeconfig's ClusterRoleBinding inside kcp")
}

// verifyShardTeardown asserts a shard whose cluster is disengaged is deleted.
//
// Deletion deregisters the shard from the root shard, so an unreachable root
// shard strands the cleanup finalizer and the Shard never goes away. The wait is
// generous: it happens on the next PlatformMesh pass, which backs off on
// transient front-proxy errors.
func verifyShardTeardown(t *testing.T, env *suite.Env, shard *suite.Cluster) {
	t.Helper()

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name:      suite.PlatformMeshName + "--" + shard.NodeIP,
		Namespace: suite.ProviderNamespace,
	}}
	require.NoError(t, env.Config.Client.Delete(t.Context(), secret))

	key := ctrlruntimeclient.ObjectKey{
		Namespace: suite.ProviderNamespace,
		Name:      names.Shard(suite.PlatformMeshName, "default", shard.NodeIP),
	}
	require.Eventually(t, func() bool {
		err := env.Config.Client.Get(t.Context(), key, &operatorv1alpha1.Shard{})
		return apierrors.IsNotFound(err)
	}, 10*time.Minute, 5*time.Second, "Shard of the disengaged cluster was not deleted")
}

func compiledExists(t *testing.T, cl ctrlruntimeclient.Client, kind, name string) bool {
	t.Helper()
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(deployv1alpha1.SchemeGroupVersion.WithKind(kind))
	return cl.Get(t.Context(), ctrlruntimeclient.ObjectKey{Namespace: suite.ProviderNamespace, Name: name}, obj) == nil
}
