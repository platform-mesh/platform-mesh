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

package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pmbrokerv1alpha1 "go.platform-mesh.io/apis/broker/v1alpha1"
	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"
	examplev1alpha1 "go.platform-mesh.io/resource-broker/api/example/v1alpha1"
	"go.platform-mesh.io/resource-broker/pkg/controller/coordbroker/migration"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

// publishRelatedConfigMap creates a ConfigMap in the staging workspace and
// publishes it as a related resource of the staging VM.
func publishRelatedConfigMap(t *testing.T, stagingClient ctrlruntimeclient.Client, nn types.NamespacedName) types.NamespacedName {
	t.Helper()

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "related-configmap",
			Namespace: nn.Namespace,
		},
		Data: map[string]string{"hello": "world"},
	}
	require.NoError(t, stagingClient.Create(t.Context(), cm))

	require.Eventually(t, func() bool {
		vm := &examplev1alpha1.VM{}
		if err := stagingClient.Get(t.Context(), nn, vm); err != nil {
			t.Logf("getting staging vm: %v", err)
			return false
		}
		vm.Status.RelatedResources = pmbrokerv1alpha1.RelatedResources{
			"configmap": {
				Namespace: cm.Namespace,
				Name:      cm.Name,
				GVK:       metav1.GroupVersionKind{Version: "v1", Kind: "ConfigMap"},
			},
		}
		if err := stagingClient.Status().Update(t.Context(), vm); err != nil {
			t.Logf("updating staging vm status: %v", err)
			return false
		}
		return true
	}, wait.ForeverTestTimeout, time.Second)

	return types.NamespacedName{Namespace: cm.Namespace, Name: cm.Name}
}

// TestConsumerDeletionCleansUp verifies that deleting a consumer resource
// cleans up everything created for it.
func TestConsumerDeletionCleansUp(t *testing.T) {
	t.Parallel()

	frame := NewFrame(t)
	provider := frame.NewProvider(t, "provider")
	createAcceptAPI(t, provider, "accept-vms", "vms")

	frame.StartBroker(t)

	consumer := frame.NewConsumer(t, "consumer")
	vm := &examplev1alpha1.VM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vm",
			Namespace: "default",
		},
		Spec: examplev1alpha1.VMSpec{
			Arch:   "x86_64",
			Memory: 512,
		},
	}
	require.NoError(t, consumer.Client.Create(t.Context(), vm))
	nn := types.NamespacedName{Namespace: vm.Namespace, Name: vm.Name}

	staging := frame.StagingClient(t, provider)
	waitForVM(t, staging, nn)

	cmName := publishRelatedConfigMap(t, staging, nn)
	require.Eventually(t, func() bool {
		return consumer.Client.Get(t.Context(), cmName, &corev1.ConfigMap{}) == nil
	}, wait.ForeverTestTimeout, time.Second, "related configmap should be copied into the consumer workspace")

	require.NoError(t, consumer.Client.Delete(t.Context(), vm))

	require.Eventually(t, func() bool {
		err := consumer.Client.Get(t.Context(), nn, &examplev1alpha1.VM{})
		return apierrors.IsNotFound(err)
	}, wait.ForeverTestTimeout, time.Second, "consumer vm should be deleted")

	require.Eventually(t, func() bool {
		err := consumer.Client.Get(t.Context(), cmName, &corev1.ConfigMap{})
		return apierrors.IsNotFound(err)
	}, wait.ForeverTestTimeout, time.Second, "related configmap copy should be deleted")

	require.Eventually(t, func() bool {
		return len(frame.listAssignments(t)) == 0
	}, wait.ForeverTestTimeout, time.Second, "assignment should be deleted")
	require.Eventually(t, func() bool {
		return len(frame.listStagingWorkspaces(t)) == 0
	}, wait.ForeverTestTimeout, time.Second, "staging workspace should be released")
}

// TestConsumerDeletionDuringMigration verifies that deleting a consumer
// resource mid-migration cleans up everything created for it.
func TestConsumerDeletionDuringMigration(t *testing.T) {
	t.Parallel()

	frame := NewFrame(t)

	x86 := frame.NewProvider(t, "x86")
	arm := frame.NewProvider(t, "arm64")
	createVMAcceptAPI(t, x86, "accept-x86", "x86_64")
	createVMAcceptAPI(t, arm, "accept-arm64", "arm64")

	config := &pmcoordbrokerv1alpha1.MigrationConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "migrate-vm",
		},
		Spec: pmcoordbrokerv1alpha1.MigrationConfigurationSpec{
			From: vmGVK,
			To:   vmGVK,
		},
	}
	require.NoError(t, frame.CoordinationClient.Create(t.Context(), config))

	frame.StartBroker(t)

	consumer := frame.NewConsumer(t, "consumer")
	vm := &examplev1alpha1.VM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vm",
			Namespace: "default",
		},
		Spec: examplev1alpha1.VMSpec{
			Arch:   "x86_64",
			Memory: 512,
		},
	}
	require.NoError(t, consumer.Client.Create(t.Context(), vm))
	nn := types.NamespacedName{Namespace: vm.Namespace, Name: vm.Name}

	x86Staging := frame.StagingClient(t, x86)
	waitForVM(t, x86Staging, nn)

	// Trigger a migration and leave it unfinished.
	updateVM(t, consumer.Client, nn, func(vm *examplev1alpha1.VM) {
		vm.Spec.Arch = "arm64"
	})
	waitForMigration(t, frame)

	require.NoError(t, consumer.Client.Delete(t.Context(), vm))

	require.Eventually(t, func() bool {
		err := consumer.Client.Get(t.Context(), nn, &examplev1alpha1.VM{})
		return apierrors.IsNotFound(err)
	}, wait.ForeverTestTimeout, time.Second, "consumer vm should be deleted")
	require.Eventually(t, func() bool {
		return len(frame.listMigrations(t)) == 0
	}, wait.ForeverTestTimeout, time.Second, "migration should be deleted")
	require.Eventually(t, func() bool {
		return len(frame.listAssignments(t)) == 0
	}, wait.ForeverTestTimeout, time.Second, "assignment should be deleted")
	require.Eventually(t, func() bool {
		return len(frame.listStagingWorkspaces(t)) == 0
	}, wait.ForeverTestTimeout, time.Second, "both staging workspaces should be released")
}

// TestAcceptAPIDeletionReleasesVerificationWorkspace verifies that deleting
// an AcceptAPI releases and deletes its verification workspace.
func TestAcceptAPIDeletionReleasesVerificationWorkspace(t *testing.T) {
	t.Parallel()

	frame := NewFrame(t)
	provider := frame.NewProvider(t, "provider")
	createAcceptAPI(t, provider, "accept-vms", "vms")

	frame.StartBroker(t)

	require.Eventually(t, func() bool {
		return len(frame.verificationWorkspaces(t)) == 1
	}, wait.ForeverTestTimeout, time.Second, "verification workspace should be created")

	acceptAPI := &pmbrokerv1alpha1.AcceptAPI{}
	require.Eventually(t, func() bool {
		if err := provider.Client.Get(t.Context(), types.NamespacedName{Name: "accept-vms"}, acceptAPI); err != nil {
			t.Logf("getting acceptapi: %v", err)
			return false
		}
		cond := meta.FindStatusCondition(acceptAPI.Status.Conditions, pmbrokerv1alpha1.AcceptAPIConditionBindingVerified)
		return cond != nil && cond.Status == metav1.ConditionTrue
	}, wait.ForeverTestTimeout, time.Second, "BindingVerified condition should become true")

	require.NoError(t, provider.Client.Delete(t.Context(), acceptAPI))

	require.Eventually(t, func() bool {
		err := provider.Client.Get(t.Context(), types.NamespacedName{Name: "accept-vms"}, &pmbrokerv1alpha1.AcceptAPI{})
		return apierrors.IsNotFound(err)
	}, wait.ForeverTestTimeout, time.Second, "acceptapi should be deleted")
	require.Eventually(t, func() bool {
		return len(frame.verificationWorkspaces(t)) == 0
	}, wait.ForeverTestTimeout, time.Second, "verification workspace should be deleted")
}

// TestAcceptAPISpecChangeReleasesVerificationWorkspace verifies that
// changing an AcceptAPI's APIExportName moves verification to a new
// workspace and releases the previous one.
func TestAcceptAPISpecChangeReleasesVerificationWorkspace(t *testing.T) {
	t.Parallel()

	frame := NewFrame(t)
	provider := frame.NewProvider(t, "provider")
	createAcceptAPI(t, provider, "accept-vms", "vms")
	createSecondExport(t, provider, "example-copy")

	frame.StartBroker(t)

	acceptAPI := &pmbrokerv1alpha1.AcceptAPI{}
	var oldWorkspace string
	require.Eventually(t, func() bool {
		if err := provider.Client.Get(t.Context(), types.NamespacedName{Name: "accept-vms"}, acceptAPI); err != nil {
			t.Logf("getting acceptapi: %v", err)
			return false
		}
		cond := meta.FindStatusCondition(acceptAPI.Status.Conditions, pmbrokerv1alpha1.AcceptAPIConditionBindingVerified)
		if cond == nil || cond.Status != metav1.ConditionTrue {
			return false
		}
		oldWorkspace = acceptAPI.Status.VerificationWorkspace
		return oldWorkspace != ""
	}, wait.ForeverTestTimeout, time.Second, "initial verification should complete")

	require.Eventually(t, func() bool {
		current := &pmbrokerv1alpha1.AcceptAPI{}
		if err := provider.Client.Get(t.Context(), types.NamespacedName{Name: "accept-vms"}, current); err != nil {
			t.Logf("getting acceptapi: %v", err)
			return false
		}
		current.Spec.APIExportName = "example-copy"
		if err := provider.Client.Update(t.Context(), current); err != nil {
			t.Logf("updating acceptapi: %v", err)
			return false
		}
		return true
	}, wait.ForeverTestTimeout, time.Second)

	require.Eventually(t, func() bool {
		current := &pmbrokerv1alpha1.AcceptAPI{}
		if err := provider.Client.Get(t.Context(), types.NamespacedName{Name: "accept-vms"}, current); err != nil {
			t.Logf("getting acceptapi: %v", err)
			return false
		}
		return current.Status.VerificationWorkspace != "" && current.Status.VerificationWorkspace != oldWorkspace
	}, wait.ForeverTestTimeout, time.Second, "verification should move to a new workspace")

	require.Eventually(t, func() bool {
		workspaces := frame.verificationWorkspaces(t)
		if len(workspaces) != 1 {
			t.Logf("want exactly one verification workspace, have %d", len(workspaces))
			return false
		}
		return workspaces[0].Name != oldWorkspace
	}, wait.ForeverTestTimeout, time.Second, "old verification workspace should be released")
}

// TestSharedVerificationWorkspace verifies that AcceptAPIs for the same
// export share a verification workspace until the last reference goes away.
func TestSharedVerificationWorkspace(t *testing.T) {
	t.Parallel()

	frame := NewFrame(t)
	provider := frame.NewProvider(t, "provider")
	createAcceptAPI(t, provider, "accept-certificates", "certificates")
	createAcceptAPI(t, provider, "accept-vms", "vms")

	frame.StartBroker(t)

	for _, name := range []string{"accept-certificates", "accept-vms"} {
		require.Eventually(t, func() bool {
			acceptAPI := &pmbrokerv1alpha1.AcceptAPI{}
			if err := provider.Client.Get(t.Context(), types.NamespacedName{Name: name}, acceptAPI); err != nil {
				t.Logf("getting acceptapi %s: %v", name, err)
				return false
			}
			cond := meta.FindStatusCondition(acceptAPI.Status.Conditions, pmbrokerv1alpha1.AcceptAPIConditionBindingVerified)
			return cond != nil && cond.Status == metav1.ConditionTrue
		}, wait.ForeverTestTimeout, time.Second, "AcceptAPI %s should be verified", name)
	}

	// Both AcceptAPIs reference the same export.
	require.Len(t, frame.verificationWorkspaces(t), 1)

	require.NoError(t, provider.Client.Delete(t.Context(), &pmbrokerv1alpha1.AcceptAPI{
		ObjectMeta: metav1.ObjectMeta{Name: "accept-certificates"},
	}))
	require.Eventually(t, func() bool {
		err := provider.Client.Get(t.Context(), types.NamespacedName{Name: "accept-certificates"}, &pmbrokerv1alpha1.AcceptAPI{})
		return apierrors.IsNotFound(err)
	}, wait.ForeverTestTimeout, time.Second, "first acceptapi should be deleted")
	require.Never(t, func() bool {
		return len(frame.verificationWorkspaces(t)) == 0
	}, 5*time.Second, time.Second, "shared verification workspace must survive while referenced")

	require.NoError(t, provider.Client.Delete(t.Context(), &pmbrokerv1alpha1.AcceptAPI{
		ObjectMeta: metav1.ObjectMeta{Name: "accept-vms"},
	}))
	require.Eventually(t, func() bool {
		return len(frame.verificationWorkspaces(t)) == 0
	}, wait.ForeverTestTimeout, time.Second, "verification workspace should be deleted with the last reference")
}

// TestStagingWorkspaceDeletion verifies that deleting a StagingWorkspace CR
// tears down its backing kcp workspace.
func TestStagingWorkspaceDeletion(t *testing.T) {
	t.Parallel()

	frame := NewFrame(t)
	provider := frame.NewProvider(t, "provider")

	frame.StartBroker(t)

	sw := &pmcoordbrokerv1alpha1.StagingWorkspace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "staging-deletion-test",
		},
		Spec: pmcoordbrokerv1alpha1.StagingWorkspaceSpec{
			ConsumerCluster: "unused",
			ProviderCluster: provider.ClusterName,
			APIExportName:   exampleExportName,
		},
	}
	require.NoError(t, frame.CoordinationClient.Create(t.Context(), sw))

	swKey := types.NamespacedName{Name: sw.Name}
	require.Eventually(t, func() bool {
		current := &pmcoordbrokerv1alpha1.StagingWorkspace{}
		if err := frame.CoordinationClient.Get(t.Context(), swKey, current); err != nil {
			t.Logf("getting staging workspace: %v", err)
			return false
		}
		return current.Status.Phase == pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady
	}, wait.ForeverTestTimeout, time.Second, "staging workspace should become ready")

	backing := &kcptenancyv1alpha1.Workspace{}
	require.NoError(t, frame.HomeClient.Get(t.Context(), swKey, backing))

	require.NoError(t, frame.CoordinationClient.Delete(t.Context(), sw))

	require.Eventually(t, func() bool {
		err := frame.HomeClient.Get(t.Context(), swKey, &kcptenancyv1alpha1.Workspace{})
		return apierrors.IsNotFound(err)
	}, wait.ForeverTestTimeout, time.Second, "backing workspace should be deleted")
	require.Eventually(t, func() bool {
		err := frame.CoordinationClient.Get(t.Context(), swKey, &pmcoordbrokerv1alpha1.StagingWorkspace{})
		return apierrors.IsNotFound(err)
	}, wait.ForeverTestTimeout, time.Second, "staging workspace CR should be deleted")
}

// TestMigrationAbortCleansStageResources verifies that aborting a migration
// cleans up deployed stage templates.
func TestMigrationAbortCleansStageResources(t *testing.T) {
	t.Parallel()

	frame := NewFrame(t)

	x86 := frame.NewProvider(t, "x86")
	arm := frame.NewProvider(t, "arm64")
	createVMAcceptAPI(t, x86, "accept-x86", "x86_64")
	createVMAcceptAPI(t, arm, "accept-arm64", "arm64")

	config := &pmcoordbrokerv1alpha1.MigrationConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "migrate-vm",
		},
		Spec: pmcoordbrokerv1alpha1.MigrationConfigurationSpec{
			From: vmGVK,
			To:   vmGVK,
			Stages: []pmcoordbrokerv1alpha1.MigrationStage{
				{
					Name: "copy-data",
					Templates: map[string]runtime.RawExtension{
						"dummy": {
							Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","data":{"key":"value"}}`),
						},
					},
					SuccessConditions: []string{
						`dummy.data.key == "done"`,
					},
				},
			},
		},
	}
	require.NoError(t, frame.CoordinationClient.Create(t.Context(), config))

	frame.StartBroker(t)

	consumer := frame.NewConsumer(t, "consumer")
	vm := &examplev1alpha1.VM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vm",
			Namespace: "default",
		},
		Spec: examplev1alpha1.VMSpec{
			Arch:   "x86_64",
			Memory: 512,
		},
	}
	require.NoError(t, consumer.Client.Create(t.Context(), vm))
	nn := types.NamespacedName{Namespace: vm.Namespace, Name: vm.Name}

	x86Staging := frame.StagingClient(t, x86)
	waitForVM(t, x86Staging, nn)

	updateVM(t, consumer.Client, nn, func(vm *examplev1alpha1.VM) {
		vm.Spec.Arch = "arm64"
	})

	migrationCR := waitForMigration(t, frame)

	cmName := types.NamespacedName{
		Namespace: migration.DefaultStageNamespace,
		Name:      migrationCR.Name + "-dummy",
	}
	require.Eventually(t, func() bool {
		return frame.ComputeClient.Get(t.Context(), cmName, &corev1.ConfigMap{}) == nil
	}, wait.ForeverTestTimeout, time.Second, "stage configmap should be deployed")

	require.NoError(t, consumer.Client.Delete(t.Context(), vm))

	require.Eventually(t, func() bool {
		return len(frame.listMigrations(t)) == 0
	}, wait.ForeverTestTimeout, time.Second, "migration should be deleted")
	require.Eventually(t, func() bool {
		err := frame.ComputeClient.Get(t.Context(), cmName, &corev1.ConfigMap{})
		return apierrors.IsNotFound(err)
	}, wait.ForeverTestTimeout, time.Second, "stage configmap should be cleaned up")
	require.Eventually(t, func() bool {
		err := consumer.Client.Get(t.Context(), nn, &examplev1alpha1.VM{})
		return apierrors.IsNotFound(err)
	}, wait.ForeverTestTimeout, time.Second, "consumer vm should be deleted")
}

// createSecondExport clones the provider's example export under a new name.
func createSecondExport(t *testing.T, provider *ControlPlane, name string) {
	t.Helper()

	existing := &kcpapisv1alpha2.APIExport{}
	require.NoError(t, provider.Client.Get(t.Context(), types.NamespacedName{Name: exampleExportName}, existing))

	export := &kcpapisv1alpha2.APIExport{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: *existing.Spec.DeepCopy(),
	}
	require.NoError(t, provider.Client.Create(t.Context(), export))
}
