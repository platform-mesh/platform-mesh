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
	"go.platform-mesh.io/resource-broker/pkg/controller/brokeredresource"
	"go.platform-mesh.io/resource-broker/pkg/controller/coordbroker/migration"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var vmGVK = metav1.GroupVersionKind{
	Group:   "example.platform-mesh.io",
	Version: "v1alpha1",
	Kind:    "VM",
}

// createVMAcceptAPI creates an AcceptAPI for VMs limited to the given
// architecture in the provider's workspace.
func createVMAcceptAPI(t *testing.T, provider *ControlPlane, name, arch string) {
	t.Helper()

	acceptAPI := &pmbrokerv1alpha1.AcceptAPI{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: pmbrokerv1alpha1.AcceptAPISpec{
			GVR: metav1.GroupVersionResource{
				Group:    "example.platform-mesh.io",
				Version:  "v1alpha1",
				Resource: "vms",
			},
			APIExportName: exampleExportName,
			Filters: []pmbrokerv1alpha1.Filter{
				{Key: "arch", ValueIn: []string{arch}},
			},
		},
	}
	require.NoError(t, provider.Client.Create(t.Context(), acceptAPI))
}

// waitForVM waits until the VM is visible through the given client and
// returns it.
func waitForVM(t *testing.T, cl ctrlruntimeclient.Client, nn types.NamespacedName) *examplev1alpha1.VM {
	t.Helper()

	vm := &examplev1alpha1.VM{}
	require.Eventually(t, func() bool {
		if err := cl.Get(t.Context(), nn, vm); err != nil {
			t.Logf("getting vm: %v", err)
			return false
		}
		return true
	}, wait.ForeverTestTimeout, time.Second)
	return vm
}

// waitForMigration waits until a Migration exists in the coordination
// workspace and returns it.
func waitForMigration(t *testing.T, frame *Frame) *pmcoordbrokerv1alpha1.Migration {
	t.Helper()

	migrations := &pmcoordbrokerv1alpha1.MigrationList{}
	require.Eventually(t, func() bool {
		if err := frame.CoordinationClient.List(t.Context(), migrations); err != nil {
			t.Logf("listing migrations: %v", err)
			return false
		}
		return len(migrations.Items) > 0
	}, wait.ForeverTestTimeout, time.Second)
	return &migrations.Items[0]
}

// markVMAvailable waits for the VM to appear with the expected architecture
// in the staging workspace and marks it available, simulating the provider.
func markVMAvailable(t *testing.T, stagingClient ctrlruntimeclient.Client, nn types.NamespacedName, arch string) {
	t.Helper()

	require.Eventually(t, func() bool {
		vm := &examplev1alpha1.VM{}
		if err := stagingClient.Get(t.Context(), nn, vm); err != nil {
			t.Logf("getting staging vm: %v", err)
			return false
		}
		if vm.Spec.Arch != arch {
			t.Logf("staging vm arch is %q, want %q", vm.Spec.Arch, arch)
			return false
		}
		if vm.Status.Status == pmbrokerv1alpha1.StatusAvailable {
			return true
		}
		vm.Status.Status = pmbrokerv1alpha1.StatusAvailable
		if err := stagingClient.Status().Update(t.Context(), vm); err != nil {
			t.Logf("updating staging vm status: %v", err)
			return false
		}
		return true
	}, wait.ForeverTestTimeout, time.Second)
}

// waitForMigrationFinished waits until the migration is cleaned up and the
// old provider's staging workspace is released.
func waitForMigrationFinished(t *testing.T, frame *Frame, oldProvider *ControlPlane) {
	t.Helper()

	// The migration is deleted after the cutover once the old staging copy
	// is gone.
	require.Eventually(t, func() bool {
		migrations := &pmcoordbrokerv1alpha1.MigrationList{}
		if err := frame.CoordinationClient.List(t.Context(), migrations); err != nil {
			t.Logf("listing migrations: %v", err)
			return false
		}
		return len(migrations.Items) == 0
	}, wait.ForeverTestTimeout, time.Second)

	// The old provider's staging workspace is unreferenced afterwards and
	// gets garbage collected.
	require.Eventually(t, func() bool {
		stagingWorkspaces := &pmcoordbrokerv1alpha1.StagingWorkspaceList{}
		if err := frame.CoordinationClient.List(t.Context(), stagingWorkspaces); err != nil {
			t.Logf("listing staging workspaces: %v", err)
			return false
		}
		for _, sw := range stagingWorkspaces.Items {
			if sw.Spec.ProviderCluster == oldProvider.ClusterName {
				t.Logf("staging workspace %s still references old provider", sw.Name)
				return false
			}
		}
		return true
	}, wait.ForeverTestTimeout, time.Second)
}

func TestMigrationNoStages(t *testing.T) {
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

	// Changing the architecture makes the x86 AcceptAPI reject the VM and
	// triggers a migration to the arm64 provider.
	require.Eventually(t, func() bool {
		current := &examplev1alpha1.VM{}
		if err := consumer.Client.Get(t.Context(), nn, current); err != nil {
			t.Logf("getting vm: %v", err)
			return false
		}
		current.Spec.Arch = "arm64"
		if err := consumer.Client.Update(t.Context(), current); err != nil {
			t.Logf("updating vm: %v", err)
			return false
		}
		return true
	}, wait.ForeverTestTimeout, time.Second)

	waitForMigration(t, frame)

	armStaging := frame.StagingClient(t, arm)
	markVMAvailable(t, armStaging, nn, "arm64")

	waitForMigrationFinished(t, frame, x86)
}

func TestMigrationWithStages(t *testing.T) {
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

	require.Eventually(t, func() bool {
		current := &examplev1alpha1.VM{}
		if err := consumer.Client.Get(t.Context(), nn, current); err != nil {
			t.Logf("getting vm: %v", err)
			return false
		}
		current.Spec.Arch = "arm64"
		if err := consumer.Client.Update(t.Context(), current); err != nil {
			t.Logf("updating vm: %v", err)
			return false
		}
		return true
	}, wait.ForeverTestTimeout, time.Second)

	migrationCR := waitForMigration(t, frame)

	// The stage deploys the template into the compute workspace and waits
	// for its success conditions.
	cmName := types.NamespacedName{
		Namespace: migration.DefaultStageNamespace,
		Name:      migrationCR.Name + "-dummy",
	}
	stageCM := &corev1.ConfigMap{}
	require.Eventually(t, func() bool {
		if err := frame.ComputeClient.Get(t.Context(), cmName, stageCM); err != nil {
			t.Logf("getting stage configmap: %v", err)
			return false
		}
		return true
	}, wait.ForeverTestTimeout, time.Second)
	require.Equal(t, "copy-data", stageCM.Labels[migration.MigrationStageLabel])
	require.Equal(t, migrationCR.Name, stageCM.Labels[migration.MigrationNameLabel])
	require.Equal(t, "value", stageCM.Data["key"])

	// Completing the stage's work unblocks the migration.
	stageCM.Data["key"] = "done"
	require.NoError(t, frame.ComputeClient.Update(t.Context(), stageCM))

	armStaging := frame.StagingClient(t, arm)
	markVMAvailable(t, armStaging, nn, "arm64")

	waitForMigrationFinished(t, frame, x86)

	// The stage resources are cleaned up after the stage succeeded.
	require.Eventually(t, func() bool {
		err := frame.ComputeClient.Get(t.Context(), cmName, &corev1.ConfigMap{})
		return err != nil
	}, wait.ForeverTestTimeout, time.Second)
}

func TestMigrationFreezesOrigin(t *testing.T) {
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

	// Wait for the origin staging copy to settle: the broker annotates the
	// copy after creating it.
	settled := &examplev1alpha1.VM{}
	require.Eventually(t, func() bool {
		if err := x86Staging.Get(t.Context(), nn, settled); err != nil {
			t.Logf("getting staging vm: %v", err)
			return false
		}
		anns := settled.GetAnnotations()
		return anns[brokeredresource.ConsumerClusterAnnotation] != "" &&
			anns[brokeredresource.ConsumerNameAnnotation] != ""
	}, wait.ForeverTestTimeout, time.Second)

	// Watch the origin staging copy: during the migration nothing but its
	// deletion may come through. Starting from the settled resource version
	// avoids the synthetic initial ADDED event.
	watcher, err := frame.StagingWatchClient(t, x86).Watch(t.Context(), &examplev1alpha1.VMList{},
		ctrlruntimeclient.InNamespace(nn.Namespace),
		ctrlruntimeclient.MatchingFields{"metadata.name": nn.Name},
		&ctrlruntimeclient.ListOptions{Raw: &metav1.ListOptions{ResourceVersion: settled.ResourceVersion}},
	)
	require.NoError(t, err)
	t.Cleanup(watcher.Stop)

	events := make(chan watch.Event, 64)
	go func() {
		defer close(events)
		for event := range watcher.ResultChan() {
			events <- event
		}
	}()

	// Changing the architecture makes the x86 AcceptAPI reject the VM and
	// triggers a migration to the arm64 provider.
	require.Eventually(t, func() bool {
		current := &examplev1alpha1.VM{}
		if err := consumer.Client.Get(t.Context(), nn, current); err != nil {
			t.Logf("getting vm: %v", err)
			return false
		}
		current.Spec.Arch = "arm64"
		if err := consumer.Client.Update(t.Context(), current); err != nil {
			t.Logf("updating vm: %v", err)
			return false
		}
		return true
	}, wait.ForeverTestTimeout, time.Second)

	waitForMigration(t, frame)

	armStaging := frame.StagingClient(t, arm)
	markVMAvailable(t, armStaging, nn, "arm64")

	waitForMigrationFinished(t, frame, x86)

	watcher.Stop()

	sawDelete := false
	for event := range events {
		switch event.Type {
		case watch.Deleted:
			sawDelete = true
		case watch.Error:
			// The origin staging workspace is torn down at the end of
			// the migration, which may terminate the watch.
			t.Logf("watch error event: %v", event.Object)
		default:
			require.Failf(t, "origin staging copy changed during migration",
				"event %s: %v", event.Type, event.Object)
		}
	}
	t.Logf("saw deletion of origin staging copy: %v", sawDelete)
}
