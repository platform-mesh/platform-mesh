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

// Package framework_test is the smoke test of tests/framework.
package framework_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	accountresources "go.platform-mesh.io/apis/static-assets/resources/account-operator"
	"go.platform-mesh.io/platform-mesh/tests/framework/fixtures"
	"go.platform-mesh.io/platform-mesh/tests/framework/kcp"
	"go.platform-mesh.io/platform-mesh/tests/framework/scheme"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/discovery"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcptc "github.com/kcp-dev/kcp-testcontainers"
	"github.com/kcp-dev/logicalcluster/v3"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	"github.com/kcp-dev/sdk/apis/core"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const widgetCRD = `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.framework.test.platform-mesh.io
spec:
  group: framework.test.platform-mesh.io
  scope: Namespaced
  names:
    kind: Widget
    listKind: WidgetList
    plural: widgets
    singular: widget
  versions:
  - name: v1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
        properties:
          spec:
            type: object
            properties:
              size:
                type: integer
`

// workspace creates a workspace under root with a generated name, served
// through the front-proxy on return.
func workspace(t *testing.T, instance *kcptc.Instance, prefix string) (logicalcluster.Path, ctrlruntimeclient.Client) {
	t.Helper()
	path, err := instance.CreateWorkspace(t.Context(), core.RootCluster.String()+":"+prefix+"-", kcptc.WaitForReachable())
	require.NoError(t, err)
	cl, err := instance.Client(path.String(), ctrlruntimeclient.Options{Scheme: scheme.New(t)})
	require.NoError(t, err)
	return path, cl
}

func TestWorkspace(t *testing.T) {
	instance, err := kcp.Default(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, instance.ShardNames(), "kcp should report its shards")
	t.Logf("shards: %v", instance.ShardNames())

	path, cl := workspace(t, instance, "framework")
	parent, ok := path.Parent()
	require.True(t, ok)
	require.Equal(t, core.RootCluster.Path(), parent)

	lc := &kcpcorev1alpha1.LogicalCluster{}
	require.NoError(t, cl.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: kcpcorev1alpha1.LogicalClusterName}, lc))
	require.Equal(t, kcpcorev1alpha1.LogicalClusterPhaseReady, lc.Status.Phase)
}

func TestCRDRoundTrip(t *testing.T) {
	instance, err := kcp.Default(t.Context())
	require.NoError(t, err)
	_, cl := workspace(t, instance, "crd")

	objs := fixtures.Decode(t, []byte(widgetCRD))
	require.Len(t, objs, 1)
	fixtures.Apply(t, cl, objs[0])
	wait.CRDEstablished(t, cl, "widgets.framework.test.platform-mesh.io")

	widget := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "framework.test.platform-mesh.io/v1",
		"kind":       "Widget",
		"metadata":   map[string]any{"name": "w1", "namespace": "default"},
		"spec":       map[string]any{"size": int64(3)},
	}}
	// The REST mapping can lag behind Established.
	wait.Eventually(t, func() (bool, string) {
		if err := cl.Create(t.Context(), widget.DeepCopy()); err != nil {
			return false, err.Error()
		}
		return true, ""
	}, wait.Timeout, wait.Tick, "creating Widget")

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(widget.GroupVersionKind())
	require.NoError(t, cl.Get(t.Context(), ctrlruntimeclient.ObjectKey{Namespace: "default", Name: "w1"}, got))
	size, found, err := unstructured.NestedInt64(got.Object, "spec", "size")
	require.NoError(t, err)
	require.True(t, found, "spec.size should round-trip")
	require.Equal(t, int64(3), size)
}

func TestInstallPlatform(t *testing.T) {
	instance, err := kcp.Default(t.Context())
	require.NoError(t, err)
	p := fixtures.Install(t, instance,
		fixtures.WithResources(accountresources.Resources, "*.yaml"),
		fixtures.WithBindings("core.platform-mesh.io"),
		fixtures.WithOrgs(),
	)
	require.Equal(t, core.RootCluster.Path().Join(fixtures.SystemWorkspace), p.System)
	require.Equal(t, core.RootCluster.Path().Join(fixtures.OrgsWorkspace), p.Orgs)

	binding := &kcpapisv1alpha2.APIBinding{}
	require.NoError(t, p.SystemClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "core.platform-mesh.io"}, binding))
	require.Equal(t, kcpapisv1alpha2.APIBindingPhaseBound, binding.Status.Phase)

	for _, name := range []string{"org", "orgs", "account"} {
		wst := &kcptenancyv1alpha1.WorkspaceType{}
		require.NoError(t, p.RootClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: name}, wst), "WorkspaceType %s", name)
	}

	orgs := &kcptenancyv1alpha1.Workspace{}
	require.NoError(t, p.RootClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: fixtures.OrgsWorkspace}, orgs))
	require.Equal(t, kcpcorev1alpha1.LogicalClusterPhaseReady, orgs.Status.Phase)

	// Install is idempotent: a second call reuses the tree.
	again := fixtures.Install(t, instance,
		fixtures.WithResources(accountresources.Resources, "*.yaml"),
		fixtures.WithBindings("core.platform-mesh.io"),
		fixtures.WithOrgs(),
	)
	require.Equal(t, p.Orgs, again.Orgs)
}

// Every virtual workspace URL kcp advertises must answer from the host.
func TestVirtualWorkspaceReachable(t *testing.T) {
	instance, err := kcp.Default(t.Context())
	require.NoError(t, err)
	p := fixtures.Install(t, instance,
		fixtures.WithResources(accountresources.Resources, "*.yaml"),
		fixtures.WithBindings("core.platform-mesh.io"),
	)
	slice := &kcpapisv1alpha1.APIExportEndpointSlice{}
	require.NoError(t, p.SystemClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "core.platform-mesh.io"}, slice))
	require.NotEmpty(t, slice.Status.APIExportEndpoints, "the endpoint slice should advertise virtual workspace URLs")

	for _, endpoint := range slice.Status.APIExportEndpoints {
		cfg := instance.BaseConfig()
		cfg.Host = endpoint.URL
		dc, err := discovery.NewDiscoveryClientForConfig(cfg)
		require.NoError(t, err)
		wait.Eventually(t, func() (bool, string) {
			_, err := dc.RESTClient().Get().AbsPath("clusters", "*", "apis", "core.platform-mesh.io").DoRaw(t.Context())
			if err != nil {
				return false, fmt.Sprintf("discovery on %s: %v", endpoint.URL, err)
			}
			return true, ""
		}, wait.Timeout, wait.Tick, "virtual workspace %s should serve discovery", endpoint.URL)
		t.Logf("virtual workspace %s reachable", endpoint.URL)
	}
}
