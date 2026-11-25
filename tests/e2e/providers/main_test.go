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

// Package providers tests the Provider object (providers.platform-mesh.io)
// against a deployed Platform Mesh, through KUBECONFIG and ADMIN_KUBECONFIG.
package providers

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/platform-mesh/tests/framework/pm"
	"go.platform-mesh.io/platform-mesh/tests/framework/wait"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

const (
	providerTimeout = 5 * time.Minute
	pollInterval    = 3 * time.Second
)

// kcpClient is an admin client for the kcp workspace path or logical cluster.
func kcpClient(t testing.TB, path string) ctrlruntimeclient.Client {
	t.Helper()
	cl, err := pm.NewClient(pm.KCP(t, path))
	require.NoError(t, err)
	return cl
}

// newProviderWorkspace creates a workspace that can hold Provider objects: it binds
// providers.platform-mesh.io and accepts the claims the Provider controller needs.
func newProviderWorkspace(t *testing.T) ctrlruntimeclient.Client {
	t.Helper()
	ctx := t.Context()
	name := fmt.Sprintf("e2e-provider-%06d", rand.IntN(1000000))

	root := kcpClient(t, "root")
	ws := &kcptenancyv1alpha1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       kcptenancyv1alpha1.WorkspaceSpec{Type: &kcptenancyv1alpha1.WorkspaceTypeReference{Name: "universal", Path: "root"}},
	}
	require.NoError(t, root.Create(ctx, ws))
	t.Cleanup(func() {
		_ = root.Delete(context.Background(), ws)
	})
	wait.WorkspaceReadyWithin(t, root, name, 2*time.Minute, pollInterval)

	cl := kcpClient(t, "root:"+name)
	claim := func(group, resource string) kcpapisv1alpha2.AcceptablePermissionClaim {
		return kcpapisv1alpha2.AcceptablePermissionClaim{
			ScopedPermissionClaim: kcpapisv1alpha2.ScopedPermissionClaim{
				PermissionClaim: kcpapisv1alpha2.PermissionClaim{GroupResource: kcpapisv1alpha2.GroupResource{Group: group, Resource: resource}, Verbs: []string{"*"}},
				Selector:        kcpapisv1alpha2.PermissionClaimSelector{MatchAll: true},
			},
			State: kcpapisv1alpha2.ClaimAccepted,
		}
	}
	binding := &kcpapisv1alpha2.APIBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "providers.platform-mesh.io"},
		Spec: kcpapisv1alpha2.APIBindingSpec{
			Reference: kcpapisv1alpha2.BindingReference{Export: &kcpapisv1alpha2.ExportBindingReference{Path: "root:platform-mesh-system", Name: "providers.platform-mesh.io"}},
			PermissionClaims: []kcpapisv1alpha2.AcceptablePermissionClaim{
				claim("", "namespaces"), claim("", "serviceaccounts"), claim("", "secrets"),
				claim("rbac.authorization.k8s.io", "roles"), claim("rbac.authorization.k8s.io", "rolebindings"),
			},
		},
	}
	require.NoError(t, cl.Create(ctx, binding))
	wait.APIBindingBound(t, cl, binding.Name)
	return cl
}

// createReadyProvider creates the Provider and returns it once it is Ready.
func createReadyProvider(t *testing.T, cl ctrlruntimeclient.Client, provider *pmprovidersv1alpha1.Provider) *pmprovidersv1alpha1.Provider {
	t.Helper()
	require.NoError(t, cl.Create(t.Context(), provider))
	t.Cleanup(func() {
		_ = cl.Delete(context.Background(), provider)
	})
	return wait.For(t, providerTimeout, pollInterval, "Provider "+provider.Name+" Ready", func() (*pmprovidersv1alpha1.Provider, error) {
		current := &pmprovidersv1alpha1.Provider{}
		if err := cl.Get(t.Context(), ctrlruntimeclient.ObjectKeyFromObject(provider), current); err != nil {
			return nil, err
		}
		if current.Status.Phase != pmprovidersv1alpha1.ProviderPhaseReady {
			return nil, fmt.Errorf("%w: phase is %q", wait.ErrNotReady, current.Status.Phase)
		}
		return current, nil
	})
}

// kubeconfigSecret returns the Secret the Provider controller wrote for the provider.
func kubeconfigSecret(t *testing.T, cl ctrlruntimeclient.Client, provider *pmprovidersv1alpha1.Provider) *corev1.Secret {
	t.Helper()
	require.NotNil(t, provider.Status.ProviderKubeconfigSecretRef)
	secret := &corev1.Secret{}
	require.NoError(t, cl.Get(t.Context(), ctrlruntimeclient.ObjectKey{
		Name:      provider.Status.ProviderKubeconfigSecretRef.Name,
		Namespace: provider.Status.ProviderKubeconfigSecretRef.Namespace,
	}, secret))
	return secret
}

// providerCluster is the logical cluster of the provider workspace: the end of the server URL.
func providerCluster(t *testing.T, kubeconfig []byte) string {
	t.Helper()
	cfg, err := clientcmd.Load(kubeconfig)
	require.NoError(t, err)
	server := cfg.Clusters[cfg.Contexts[cfg.CurrentContext].Cluster].Server
	_, cluster, ok := strings.Cut(server, "/clusters/")
	require.True(t, ok, "server %q has no /clusters/ part", server)
	return cluster
}
