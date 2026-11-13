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

// Package platformmeshoperator holds black-box e2e scenarios of the platform-mesh-operator
// against an already deployed Platform Mesh (see tests/framework/pm for how
// it is found).
package platformmeshoperator

import (
	"context"
	"embed"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/platform-mesh/tests/framework/pm"
	"go.platform-mesh.io/platform-mesh/tests/framework/scheme"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const (
	platformMeshNamespace = "platform-mesh-system"
	platformMeshName      = "platform-mesh"

	// readyTimeout bounds the wait for PlatformMesh Ready. Tilt (`task tilt:ci`)
	// already waits for the stack, so this mostly covers reconciles triggered
	// by a previous scenario's spec change.
	readyTimeout = 15 * time.Minute
	pollInterval = 5 * time.Second
)

//go:embed testdata
var testdata embed.FS

func newScheme(t testing.TB) *runtime.Scheme {
	t.Helper()
	return scheme.New(t, pmcorev1alpha1.AddToScheme, pmprovidersv1alpha1.AddToScheme)
}

// kubeClient is a client for the runtime cluster where PlatformMesh lives.
func kubeClient(t testing.TB) ctrlruntimeclient.Client {
	t.Helper()
	return newClient(t, pm.Kube(t))
}

// kcpClient is a kcp admin client scoped to the logical cluster path.
func kcpClient(t testing.TB, path string) ctrlruntimeclient.Client {
	t.Helper()
	return newClient(t, pm.KCP(t, path))
}

// kubeconfigClient builds a client from an operator-written kubeconfig,
// made reachable from the test host (see pm.Kubeconfig).
func kubeconfigClient(t testing.TB, kubeconfig []byte, fallbackPath string) ctrlruntimeclient.Client {
	t.Helper()
	return newClient(t, pm.Kubeconfig(t, kubeconfig, fallbackPath))
}

func newClient(t testing.TB, cfg *rest.Config) ctrlruntimeclient.Client {
	t.Helper()
	cfg = rest.CopyConfig(cfg)
	cfg.Timeout = 60 * time.Second
	cl, err := ctrlruntimeclient.New(cfg, ctrlruntimeclient.Options{Scheme: newScheme(t)})
	require.NoError(t, err, "creating client for %s", cfg.Host)
	return cl
}

func platformMeshKey() ctrlruntimeclient.ObjectKey {
	return ctrlruntimeclient.ObjectKey{Name: platformMeshName, Namespace: platformMeshNamespace}
}

// waitPlatformMeshReady waits until the PlatformMesh Ready condition is True.
func waitPlatformMeshReady(t testing.TB, cl ctrlruntimeclient.Client) {
	t.Helper()
	wait.Eventually(t, func() (bool, string) {
		pm := &pmcorev1alpha1.PlatformMesh{}
		if err := cl.Get(t.Context(), platformMeshKey(), pm); err != nil {
			return false, fmt.Sprintf("getting PlatformMesh: %v", err)
		}
		for _, c := range pm.Status.Conditions {
			if c.Type == "Ready" && c.Status == metav1.ConditionTrue {
				return true, ""
			}
		}
		return false, "not Ready:\n" + conditionsString(pm.Status.Conditions)
	}, readyTimeout, pollInterval, "PlatformMesh %s/%s should be Ready", platformMeshNamespace, platformMeshName)
}

func conditionsString(conds []metav1.Condition) string {
	if len(conds) == 0 {
		return "status.conditions is empty"
	}
	lines := make([]string, 0, len(conds))
	for _, c := range conds {
		lines = append(lines, fmt.Sprintf("type=%s status=%s reason=%s message=%s", c.Type, c.Status, c.Reason, c.Message))
	}
	return strings.Join(lines, "\n")
}

// updatePlatformMesh applies mutate to the live PlatformMesh, retrying on conflicts.
func updatePlatformMesh(ctx context.Context, cl ctrlruntimeclient.Client, mutate func(*pmcorev1alpha1.PlatformMesh), opts ...ctrlruntimeclient.UpdateOption) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		pm := &pmcorev1alpha1.PlatformMesh{}
		if err := cl.Get(ctx, platformMeshKey(), pm); err != nil {
			return err
		}
		mutate(pm)
		return cl.Update(ctx, pm, opts...)
	})
}

// addProviderConnections adds (or replaces, keyed by secret) extra provider
// connections on the PlatformMesh and removes them again when t finishes, so
// the shared cluster is left as the scenario found it.
func addProviderConnections(t *testing.T, cl ctrlruntimeclient.Client, conns ...pmcorev1alpha1.ProviderConnection) {
	t.Helper()
	require.NoError(t, updatePlatformMesh(t.Context(), cl, func(pm *pmcorev1alpha1.PlatformMesh) {
		for _, c := range conns {
			if i := slices.IndexFunc(pm.Spec.Kcp.ExtraProviderConnections, bySecret(c.Secret)); i >= 0 {
				pm.Spec.Kcp.ExtraProviderConnections[i] = c
				continue
			}
			pm.Spec.Kcp.ExtraProviderConnections = append(pm.Spec.Kcp.ExtraProviderConnections, c)
		}
	}), "adding extraProviderConnections to PlatformMesh")
	t.Cleanup(func() {
		err := updatePlatformMesh(context.Background(), cl, func(pm *pmcorev1alpha1.PlatformMesh) {
			for _, c := range conns {
				pm.Spec.Kcp.ExtraProviderConnections = slices.DeleteFunc(pm.Spec.Kcp.ExtraProviderConnections, bySecret(c.Secret))
			}
		})
		if err != nil {
			t.Logf("cleanup: removing extraProviderConnections: %v", err)
		}
	})
}

func bySecret(secret string) func(pmcorev1alpha1.ProviderConnection) bool {
	return func(c pmcorev1alpha1.ProviderConnection) bool { return c.Secret == secret }
}

// waitKubeconfigSecret waits until the operator wrote a non-empty kubeconfig
// into the secret and returns it.
func waitKubeconfigSecret(t testing.TB, cl ctrlruntimeclient.Client, key ctrlruntimeclient.ObjectKey, timeout time.Duration) []byte {
	t.Helper()
	var kubeconfig []byte
	wait.Eventually(t, func() (bool, string) {
		sec := &corev1.Secret{}
		if err := cl.Get(t.Context(), key, sec); err != nil {
			return false, fmt.Sprintf("getting secret: %v", err)
		}
		kubeconfig = sec.Data["kubeconfig"]
		if len(kubeconfig) == 0 {
			return false, "secret has no kubeconfig key"
		}
		return true, ""
	}, timeout, pollInterval, "secret %s should contain a kubeconfig", key)
	return kubeconfig
}

// waitWorkspaceReady waits until the Workspace name in cl's workspace is Ready.
func waitWorkspaceReady(t testing.TB, cl ctrlruntimeclient.Client, name string) {
	t.Helper()
	wait.Eventually(t, func() (bool, string) {
		ws := &kcptenancyv1alpha1.Workspace{}
		if err := cl.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: name}, ws); err != nil {
			return false, fmt.Sprintf("getting workspace: %v", err)
		}
		if ws.Status.Phase != "Ready" {
			return false, fmt.Sprintf("phase %q, conditions %+v", ws.Status.Phase, ws.Status.Conditions)
		}
		return true, ""
	}, 5*time.Minute, pollInterval, "workspace %s should be Ready", name)
}

// waitEndpointSliceServed waits until the APIExportEndpointSlice lists an
// endpoint (status.endpoints or the older status.apiExportEndpoints).
func waitEndpointSliceServed(t testing.TB, cl ctrlruntimeclient.Client, name string) {
	t.Helper()
	wait.Eventually(t, func() (bool, string) {
		slice := &unstructured.Unstructured{}
		slice.SetAPIVersion("apis.kcp.io/v1alpha1")
		slice.SetKind("APIExportEndpointSlice")
		if err := cl.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: name}, slice); err != nil {
			return false, fmt.Sprintf("getting APIExportEndpointSlice: %v", err)
		}
		endpoints, _, _ := unstructured.NestedSlice(slice.Object, "status", "endpoints")
		legacy, _, _ := unstructured.NestedSlice(slice.Object, "status", "apiExportEndpoints")
		if len(endpoints) == 0 && len(legacy) == 0 {
			return false, "no endpoints yet"
		}
		return true, ""
	}, 6*time.Minute, pollInterval, "APIExportEndpointSlice %s should have endpoints", name)
}
