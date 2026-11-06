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

package operator

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/tests/framework/wait"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// TestPlatformMeshReady: the deployed PlatformMesh reaches Ready.
func TestPlatformMeshReady(t *testing.T) {
	waitPlatformMeshReady(t, kubeClient(t))
}

// TestExtraWorkspaces: an extra workspace and an external AdminAuth provider
// connection to it are reconciled, the operator writes the provider secret
// and every condition stays True.
func TestExtraWorkspaces(t *testing.T) {
	const (
		extraPath   = "root:orgs:extra1"
		extraSecret = "extra1-kubeconfig"
	)
	cl := kubeClient(t)
	waitPlatformMeshReady(t, cl)

	extraWorkspace := pmcorev1alpha1.WorkspaceDeclaration{
		Path: extraPath,
		Type: pmcorev1alpha1.WorkspaceTypeReference{Name: "org", Path: "root"},
	}
	byPath := func(w pmcorev1alpha1.WorkspaceDeclaration) bool { return w.Path == extraPath }
	require.NoError(t, updatePlatformMesh(t.Context(), cl, func(pm *pmcorev1alpha1.PlatformMesh) {
		if !slices.ContainsFunc(pm.Spec.Kcp.ExtraWorkspaces, byPath) {
			pm.Spec.Kcp.ExtraWorkspaces = append(pm.Spec.Kcp.ExtraWorkspaces, extraWorkspace)
		}
	}), "adding extra workspace to PlatformMesh")
	t.Cleanup(func() {
		// The workspace itself is left in place: the operator does not delete
		// extra workspaces, and a re-run reuses it.
		err := updatePlatformMesh(context.Background(), cl, func(pm *pmcorev1alpha1.PlatformMesh) {
			pm.Spec.Kcp.ExtraWorkspaces = slices.DeleteFunc(pm.Spec.Kcp.ExtraWorkspaces, byPath)
		})
		if err != nil {
			t.Logf("cleanup: removing extra workspace: %v", err)
		}
	})
	addProviderConnections(t, cl, pmcorev1alpha1.ProviderConnection{
		Path:      extraPath,
		Secret:    extraSecret,
		External:  true,
		AdminAuth: ptr.To(true),
	})

	wait.Eventually(t, func() (bool, string) {
		pm := &pmcorev1alpha1.PlatformMesh{}
		if err := cl.Get(t.Context(), platformMeshKey(), pm); err != nil {
			return false, fmt.Sprintf("getting PlatformMesh: %v", err)
		}
		for _, c := range pm.Status.Conditions {
			if c.Status != metav1.ConditionTrue {
				return false, "not all conditions True:\n" + conditionsString(pm.Status.Conditions)
			}
		}
		if err := cl.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: extraSecret, Namespace: platformMeshNamespace}, &corev1.Secret{}); err != nil {
			return false, fmt.Sprintf("getting secret %s: %v", extraSecret, err)
		}
		return true, ""
	}, 20*time.Minute, pollInterval, "PlatformMesh should reconcile extra workspace %s and write secret %s", extraPath, extraSecret)
}
