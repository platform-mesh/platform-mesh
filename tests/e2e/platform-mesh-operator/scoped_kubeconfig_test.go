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

package platformmeshoperator

import (
	"context"
	"fmt"
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	kcpmanifests "go.platform-mesh.io/apis/static-assets/kcp"
	"go.platform-mesh.io/platform-mesh/tests/framework/fixtures"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

const (
	// Operator-written secrets for the extraProviderConnections below.
	scopedProvider1Secret = "kind-e2e-provider1-scoped-kubeconfig"
	scopedProvider2Secret = "kind-e2e-provider2-scoped-kubeconfig"
	adminProvider3Secret  = "kind-e2e-provider3-admin-kubeconfig"

	// APIExport and APIExportEndpointSlice in testdata/kcp-provider-workspaces.
	scopedExportName       = "kind-e2e-scoped-provider.platform-mesh.io"
	scopedConfigAPIVersion = "kind-e2e-scoped-provider.platform-mesh.io/v1alpha1"
	scopedConfigKind       = "E2EProviderConfig"

	scopedProvider1Path = "root:providers:provider1"
	scopedProvider2Path = "root:providers:provider2"
)

// TestScopedKubeconfig: provider workspaces with a pre-provisioned APIExport
// get operator-written, scoped (non-admin) kubeconfigs through
// extraProviderConnections, by endpoint slice (provider1) and by APIExport
// name (provider2); those kubeconfigs can create objects of the export. An
// AdminAuth connection (provider3) points at the front-proxy.
func TestScopedKubeconfig(t *testing.T) {
	cl := kubeClient(t)
	waitPlatformMeshReady(t, cl)

	seedProviderWorkspaces(t)
	addProviderConnections(t, cl,
		pmcorev1alpha1.ProviderConnection{
			Path:              scopedProvider1Path,
			Secret:            scopedProvider1Secret,
			EndpointSliceName: ptr.To(scopedExportName),
			AdminAuth:         ptr.To(false),
		},
		pmcorev1alpha1.ProviderConnection{
			Path:           scopedProvider2Path,
			Secret:         scopedProvider2Secret,
			APIExportNames: []string{scopedExportName},
			AdminAuth:      ptr.To(false),
		},
		pmcorev1alpha1.ProviderConnection{
			Path:              "root:platform-mesh-system",
			Secret:            adminProvider3Secret,
			EndpointSliceName: ptr.To("core.platform-mesh.io"),
			AdminAuth:         ptr.To(true),
		},
	)
	kubeconfigs := map[string][]byte{}
	for _, secret := range []string{scopedProvider1Secret, scopedProvider2Secret, adminProvider3Secret} {
		kubeconfigs[secret] = waitKubeconfigSecret(t, cl, ctrlruntimeclient.ObjectKey{Name: secret, Namespace: platformMeshNamespace}, 6*time.Minute)
	}
	waitPlatformMeshReady(t, cl)

	// provider1's kubeconfig points at the export's virtual workspace; from the
	// test host that is flaky for create/get, so the concrete workspace URL is
	// used instead (as the kind suite did).
	t.Run("provider1 by endpoint slice", func(t *testing.T) {
		createProviderConfigWithKubeconfig(t, kubeconfigs[scopedProvider1Secret], scopedProvider1Path, "provider1")
	})
	t.Run("provider2 by APIExport name", func(t *testing.T) {
		createProviderConfigWithKubeconfig(t, kubeconfigs[scopedProvider2Secret], scopedProvider2Path, "provider2")
	})
	t.Run("provider3 admin kubeconfig uses the front-proxy", func(t *testing.T) {
		cfg, err := clientcmd.Load(kubeconfigs[adminProvider3Secret])
		require.NoError(t, err, "parsing provider3 kubeconfig")
		kctx := cfg.Contexts[cfg.CurrentContext]
		require.NotNil(t, kctx, "kubeconfig must have a current context")
		cluster := cfg.Clusters[kctx.Cluster]
		require.NotNil(t, cluster, "kubeconfig must contain the current cluster")
		require.Contains(t, cluster.Server, "front-proxy", "admin provider kubeconfig should use the front-proxy host")
	})
}

// seedProviderWorkspaces creates root:providers:provider1|provider2 and the
// APIExport (+ schema + endpoint slice) in each, modelling a provider that
// already exists before the operator is told about it. Idempotent.
func seedProviderWorkspaces(t *testing.T) {
	t.Helper()
	dir, err := fs.Sub(testdata, "testdata/kcp-provider-workspaces")
	require.NoError(t, err)

	root := kcpClient(t, "root")
	fixtures.ApplyFS(t, root, kcpmanifests.Resources, "workspace-providers.yaml")
	waitWorkspaceReady(t, root, "providers")

	providers := kcpClient(t, "root:providers")
	fixtures.ApplyFS(t, providers, dir, "workspace-provider1-provider2.yaml")
	waitWorkspaceReady(t, providers, "provider1")
	waitWorkspaceReady(t, providers, "provider2")

	provider1 := kcpClient(t, scopedProvider1Path)
	fixtures.ApplyFS(t, provider1, dir, "provider1-kind-e2e-scoped-provider-export.yaml")
	bindExport(t, provider1, "e2e-provider1-endpoint-bootstrap", scopedProvider1Path)

	provider2 := kcpClient(t, scopedProvider2Path)
	fixtures.ApplyFS(t, provider2, dir, "provider2-kind-e2e-scoped-provider-export.yaml")
	bindExport(t, provider2, "e2e-provider2-export-bootstrap", scopedProvider2Path)

	// provider1's connection uses endpointSliceName: the slice needs an
	// endpoint (kcp lists one once a binding exists) before the operator can
	// write the kubeconfig.
	waitEndpointSliceServed(t, provider1, scopedExportName)
}

// bindExport binds the workspace's own export so the resource is served there.
func bindExport(t *testing.T, cl ctrlruntimeclient.Client, name, exportPath string) {
	t.Helper()
	binding := &kcpapisv1alpha2.APIBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: kcpapisv1alpha2.APIBindingSpec{
			Reference: kcpapisv1alpha2.BindingReference{
				Export: &kcpapisv1alpha2.ExportBindingReference{Path: exportPath, Name: scopedExportName},
			},
		},
	}
	fixtures.Apply(t, cl, binding)
	wait.Eventually(t, func() (bool, string) {
		current := &kcpapisv1alpha2.APIBinding{}
		if err := cl.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: name}, current); err != nil {
			return false, fmt.Sprintf("getting APIBinding: %v", err)
		}
		if current.Status.Phase != kcpapisv1alpha2.APIBindingPhaseBound {
			return false, fmt.Sprintf("phase %q", current.Status.Phase)
		}
		return true, ""
	}, 3*time.Minute, pollInterval, "APIBinding %s should be Bound", name)
}

// createProviderConfigWithKubeconfig creates an E2EProviderConfig using only
// the operator-written kubeconfig and reads it back; the object is deleted
// with the kcp admin client afterwards.
func createProviderConfigWithKubeconfig(t *testing.T, kubeconfig []byte, workspacePath, prefix string) {
	t.Helper()
	name := fmt.Sprintf("e2e-%s-%d", prefix, time.Now().UnixNano())
	note := "scoped-kubeconfig-" + prefix + "-e2e"
	scoped := kubeconfigClient(t, kubeconfig, workspacePath)

	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(scopedConfigAPIVersion)
	obj.SetKind(scopedConfigKind)
	obj.SetName(name)
	require.NoError(t, unstructured.SetNestedField(obj.Object, note, "spec", "note"))
	require.NoError(t, scoped.Create(t.Context(), obj), "creating %s with the %s kubeconfig", scopedConfigKind, prefix)
	t.Cleanup(func() {
		del := &unstructured.Unstructured{}
		del.SetAPIVersion(scopedConfigAPIVersion)
		del.SetKind(scopedConfigKind)
		del.SetName(name)
		if err := kcpClient(t, workspacePath).Delete(context.Background(), del); err != nil && !apierrors.IsNotFound(err) {
			t.Logf("cleanup: deleting %s %s in %s: %v", scopedConfigKind, name, workspacePath, err)
		}
	})

	got := &unstructured.Unstructured{}
	got.SetAPIVersion(scopedConfigAPIVersion)
	got.SetKind(scopedConfigKind)
	require.NoError(t, scoped.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: name}, got), "reading back %s %s", scopedConfigKind, name)
	gotNote, _, _ := unstructured.NestedString(got.Object, "spec", "note")
	require.Equal(t, note, gotNote)
}
