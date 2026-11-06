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

package suite

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/platform-mesh-deployer/pkg/names"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kcp-dev/kcp-operator/sdk/apis/operator/v1alpha1"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

func kcpScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(kcptenancyv1alpha1.AddToScheme(s))
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(s))
	utilruntime.Must(kcpcorev1alpha1.AddToScheme(s))
	return s
}

// VerifyKcp asserts a kcp instance works end to end by creating a workspace on every shard and binding a shim APIExport.
func (e *Env) VerifyKcp(t *testing.T, root, frontProxy *Cluster, expectedShards int) {
	t.Helper()

	waitDeploymentReady(t, frontProxy, ProviderNamespace, names.FrontProxy(PlatformMeshName, "fp", frontProxy.NodeIP)+"-front-proxy")
	waitDeploymentReady(t, root, ProviderNamespace, names.RootShard(PlatformMeshName, "root", root.NodeIP)+"-kcp")

	base := e.mintAdminConfig(t, root, frontProxy)
	scheme := kcpScheme(t)
	rootClient := clusterClient(t, base, "root", scheme)

	shards := waitForShards(t, rootClient, expectedShards)
	shardNames := make([]string, 0, len(shards))
	for i := range shards {
		shardNames = append(shardNames, shards[i].Name)
	}
	sort.Strings(shardNames)
	t.Logf("kcp shards: %v", shardNames)

	// One workspace per shard, pinned to it via the shard's name label.
	wsByShard := map[string]string{}
	for _, shard := range shardNames {
		name := "ws-" + strings.ReplaceAll(shard, ":", "-")
		createWorkspace(t, rootClient, name, shard)
		wsByShard[shard] = name
	}
	for _, shard := range shardNames {
		waitWorkspaceReady(t, rootClient, wsByShard[shard])
	}

	// Export a shim API in the first shard's workspace.
	providerPath := "root:" + wsByShard[shardNames[0]]
	createShimExport(t, clusterClient(t, base, providerPath, scheme))

	// Bind it from a workspace on every shard (same-shard and cross-shard).
	for _, shard := range shardNames {
		consumer := clusterClient(t, base, "root:"+wsByShard[shard], scheme)
		bindShimExport(t, consumer, providerPath)
		t.Logf("APIBinding bound: shard=%s workspace=%s", shard, wsByShard[shard])
	}
}

func (e *Env) mintAdminConfig(t *testing.T, root, frontProxy *Cluster) *rest.Config {
	t.Helper()
	kc := &operatorv1alpha1.Kubeconfig{
		ObjectMeta: metav1.ObjectMeta{Name: "e2e-admin", Namespace: ProviderNamespace},
		Spec: operatorv1alpha1.KubeconfigSpec{
			Target:          operatorv1alpha1.KubeconfigTarget{FrontProxyRef: &corev1.LocalObjectReference{Name: names.FrontProxy(PlatformMeshName, "fp", frontProxy.NodeIP)}},
			TargetWorkspace: "root",
			Username:        "e2e-admin",
			Groups:          []string{"system:kcp:admin"},
			Validity:        metav1.Duration{Duration: 24 * time.Hour},
			SecretRef:       corev1.LocalObjectReference{Name: "e2e-admin-kubeconfig"},
		},
	}
	if err := e.Config.Client.Create(t.Context(), kc); err != nil && !apierrors.IsAlreadyExists(err) {
		require.NoError(t, err)
	}
	waitForSecret(t, e.Config, ProviderNamespace, "e2e-admin-kubeconfig")

	cfg, err := clientcmd.RESTConfigFromKubeConfig(e.secret(t, "e2e-admin-kubeconfig").Data["kubeconfig"])
	require.NoError(t, err)
	// Strip the default context's .../clusters/<TargetWorkspace> back
	// to the bare origin because later functions modify the host.
	cfg.Host, _, _ = strings.Cut(cfg.Host, "/clusters/")
	// TLS still verifies against the front proxy's sslip.io hostname in
	// cfg.Host; only the connection is redirected.
	cfg.Dial = FrontProxyDialer(t, frontProxy)
	return cfg
}

func (e *Env) secret(t *testing.T, name string) *corev1.Secret {
	t.Helper()
	s := &corev1.Secret{}
	require.NoError(t, e.Config.Client.Get(t.Context(), ctrlruntimeclient.ObjectKey{Namespace: ProviderNamespace, Name: name}, s))
	return s
}

func clusterClient(t *testing.T, base *rest.Config, path string, scheme *runtime.Scheme) ctrlruntimeclient.Client {
	t.Helper()
	cfg := rest.CopyConfig(base)
	cfg.Host = base.Host + "/clusters/" + path
	cl, err := ctrlruntimeclient.New(cfg, ctrlruntimeclient.Options{Scheme: scheme})
	require.NoError(t, err)
	return cl
}

func waitForShards(t *testing.T, rootClient ctrlruntimeclient.Client, expected int) []kcpcorev1alpha1.Shard {
	t.Helper()
	var shards []kcpcorev1alpha1.Shard
	require.Eventuallyf(t, func() bool {
		list := &kcpcorev1alpha1.ShardList{}
		if err := rootClient.List(t.Context(), list); err != nil {
			t.Logf("listing shards: %v", err)
			return false
		}
		shards = list.Items
		return len(shards) >= expected
	}, 5*time.Minute, 5*time.Second, "expected %d kcp shards to register", expected)
	return shards
}

// createWorkspace creates a workspace below parent. An empty shardName lets
// kcp schedule it anywhere.
func createWorkspace(t *testing.T, parent ctrlruntimeclient.Client, name, shardName string) {
	t.Helper()
	ws := &kcptenancyv1alpha1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: kcptenancyv1alpha1.WorkspaceSpec{
			// universal has no cross-shard initializers, so it schedules onto any shard.
			Type: &kcptenancyv1alpha1.WorkspaceTypeReference{Name: "universal"},
		},
	}
	if shardName != "" {
		ws.Spec.Location = &kcptenancyv1alpha1.WorkspaceLocation{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": shardName}},
		}
	}
	if err := parent.Create(t.Context(), ws); err != nil && !apierrors.IsAlreadyExists(err) {
		require.NoError(t, err)
	}
}

// WorkspaceClient returns a client scoped to a kcp workspace path. The
// workspace must already exist; the deployer's provisioner creates it.
func (e *Env) WorkspaceClient(t *testing.T, root, frontProxy *Cluster, path string) ctrlruntimeclient.Client {
	t.Helper()
	base := e.mintAdminConfig(t, root, frontProxy)
	return clusterClient(t, base, path, kcpScheme(t))
}

// WaitWorkspace blocks until the workspace at path is usable.
func (e *Env) WaitWorkspace(t *testing.T, root, frontProxy *Cluster, parent, name string) {
	t.Helper()
	base := e.mintAdminConfig(t, root, frontProxy)
	waitWorkspaceReady(t, clusterClient(t, base, parent, kcpScheme(t)), name)
}

func waitWorkspaceReady(t *testing.T, rootClient ctrlruntimeclient.Client, name string) {
	t.Helper()
	require.Eventuallyf(t, func() bool {
		ws := &kcptenancyv1alpha1.Workspace{}
		if err := rootClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: name}, ws); err != nil {
			return false
		}
		return ws.Status.Phase == kcpcorev1alpha1.LogicalClusterPhaseReady
	}, 3*time.Minute, 3*time.Second, "workspace %q not Ready", name)
}

// shimSchema is a trivial cluster-scoped resource used to validate API binding.
const shimSchema = `{"type":"object","properties":{"spec":{"type":"object","properties":{"value":{"type":"string"}}}}}`

func createShimExport(t *testing.T, provider ctrlruntimeclient.Client) {
	t.Helper()
	schema := &kcpapisv1alpha1.APIResourceSchema{
		ObjectMeta: metav1.ObjectMeta{Name: "v1.shims.example.io"},
		Spec: kcpapisv1alpha1.APIResourceSchemaSpec{
			Group: "example.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{Plural: "shims", Singular: "shim", Kind: "Shim", ListKind: "ShimList"},
			Scope: apiextensionsv1.ClusterScoped,
			Versions: []kcpapisv1alpha1.APIResourceVersion{{
				Name: "v1", Served: true, Storage: true, Schema: runtime.RawExtension{Raw: []byte(shimSchema)},
			}},
		},
	}
	if err := provider.Create(t.Context(), schema); err != nil && !apierrors.IsAlreadyExists(err) {
		require.NoError(t, err)
	}
	export := &kcpapisv1alpha1.APIExport{
		ObjectMeta: metav1.ObjectMeta{Name: "shim"},
		Spec:       kcpapisv1alpha1.APIExportSpec{LatestResourceSchemas: []string{"v1.shims.example.io"}},
	}
	if err := provider.Create(t.Context(), export); err != nil && !apierrors.IsAlreadyExists(err) {
		require.NoError(t, err)
	}
}

func bindShimExport(t *testing.T, consumer ctrlruntimeclient.Client, providerPath string) {
	t.Helper()
	binding := &kcpapisv1alpha1.APIBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "shim"},
		Spec: kcpapisv1alpha1.APIBindingSpec{
			Reference: kcpapisv1alpha1.BindingReference{
				Export: &kcpapisv1alpha1.ExportBindingReference{Path: providerPath, Name: "shim"},
			},
		},
	}
	if err := consumer.Create(t.Context(), binding); err != nil && !apierrors.IsAlreadyExists(err) {
		require.NoError(t, err)
	}
	require.Eventually(t, func() bool {
		b := &kcpapisv1alpha1.APIBinding{}
		if err := consumer.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "shim"}, b); err != nil {
			return false
		}
		return b.Status.Phase == kcpapisv1alpha1.APIBindingPhaseBound
	}, 3*time.Minute, 3*time.Second, "APIBinding did not become Bound")
}

func waitDeploymentReady(t *testing.T, c *Cluster, namespace, name string) {
	t.Helper()
	require.Eventuallyf(t, func() bool {
		dep := &appsv1.Deployment{}
		if err := c.Client.Get(t.Context(), ctrlruntimeclient.ObjectKey{Namespace: namespace, Name: name}, dep); err != nil {
			return false
		}
		return dep.Status.ReadyReplicas > 0 && dep.Status.ReadyReplicas == dep.Status.Replicas
	}, 5*time.Minute, 5*time.Second, "deployment %s/%s not ready", namespace, name)
}
