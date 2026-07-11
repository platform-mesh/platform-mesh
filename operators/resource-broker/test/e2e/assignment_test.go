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
	examplev1alpha1 "go.platform-mesh.io/resource-broker/api/example/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

// providerByCluster returns the provider with the given cluster name.
func providerByCluster(t *testing.T, frame *Frame, clusterName string) *ControlPlane {
	t.Helper()

	for _, cp := range frame.Providers {
		if cp.ClusterName == clusterName {
			return cp
		}
	}
	require.Failf(t, "unknown provider cluster", "no provider with cluster name %s", clusterName)
	return nil
}

// TestProviderWithdrawalTriggersMigration verifies that deleting the bound
// AcceptAPI migrates the resource to another matching provider.
func TestProviderWithdrawalTriggersMigration(t *testing.T) {
	t.Parallel()

	frame := NewFrame(t)

	one := frame.NewProvider(t, "p-one")
	two := frame.NewProvider(t, "p-two")
	createAcceptAPI(t, one, "accept-vms", "vms")
	createAcceptAPI(t, two, "accept-vms", "vms")

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

	assignment := waitForBoundAssignment(t, frame)
	origin := providerByCluster(t, frame, assignment.Status.ProviderCluster)
	other := one
	if origin == one {
		other = two
	}

	waitForVM(t, frame.StagingClient(t, origin), nn)

	require.NoError(t, origin.Client.Delete(t.Context(), &pmbrokerv1alpha1.AcceptAPI{
		ObjectMeta: metav1.ObjectMeta{Name: "accept-vms"},
	}))

	waitForMigration(t, frame)
	markVMAvailable(t, frame.StagingClient(t, other), nn, "x86_64")
	waitForMigrationFinished(t, frame, origin)

	rebound := waitForBoundAssignment(t, frame)
	require.Equal(t, other.ClusterName, rebound.Status.ProviderCluster)
}

// TestNoMatchingAcceptAPIStaysPending verifies that a resource matched by
// no AcceptAPI gets neither an Assignment nor a staging workspace.
func TestNoMatchingAcceptAPIStaysPending(t *testing.T) {
	t.Parallel()

	frame := NewFrame(t)
	provider := frame.NewProvider(t, "provider")
	createVMAcceptAPI(t, provider, "accept-x86", "x86_64")

	frame.StartBroker(t)

	consumer := frame.NewConsumer(t, "consumer")
	vm := &examplev1alpha1.VM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vm",
			Namespace: "default",
		},
		Spec: examplev1alpha1.VMSpec{
			Arch:   "arm64",
			Memory: 512,
		},
	}
	require.NoError(t, consumer.Client.Create(t.Context(), vm))

	require.Never(t, func() bool {
		return len(frame.listAssignments(t)) > 0 || len(frame.listStagingWorkspaces(t)) > 0
	}, 10*time.Second, time.Second, "no assignment or staging workspace may be created without a matching AcceptAPI")
}

// TestNoMigrationTargetKeepsAssignment verifies that a resource stays bound
// to its provider when no other AcceptAPI can take it over.
func TestNoMigrationTargetKeepsAssignment(t *testing.T) {
	t.Parallel()

	frame := NewFrame(t)
	provider := frame.NewProvider(t, "provider")
	createVMAcceptAPI(t, provider, "accept-x86", "x86_64")

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

	waitForVM(t, frame.StagingClient(t, provider), nn)
	waitForBoundAssignment(t, frame)

	// No other AcceptAPI matches arm64: nowhere to migrate to.
	updateVM(t, consumer.Client, nn, func(vm *examplev1alpha1.VM) {
		vm.Spec.Arch = "arm64"
	})

	require.Never(t, func() bool {
		return len(frame.listMigrations(t)) > 0
	}, 10*time.Second, time.Second, "no migration may be created without a target AcceptAPI")

	assignments := frame.listAssignments(t)
	require.Len(t, assignments, 1)
	require.Equal(t, provider.ClusterName, assignments[0].Status.ProviderCluster)
}

// TestMultipleMatchingAcceptAPIs verifies that a resource matched by
// several AcceptAPIs is assigned to exactly one of them.
func TestMultipleMatchingAcceptAPIs(t *testing.T) {
	t.Parallel()

	frame := NewFrame(t)

	one := frame.NewProvider(t, "p-one")
	two := frame.NewProvider(t, "p-two")
	createAcceptAPI(t, one, "accept-vms", "vms")
	createAcceptAPI(t, two, "accept-vms", "vms")

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

	assignment := waitForBoundAssignment(t, frame)
	require.Contains(t,
		[]string{one.ClusterName, two.ClusterName},
		assignment.Status.ProviderCluster,
		"assignment must pick one of the matching providers",
	)

	chosen := providerByCluster(t, frame, assignment.Status.ProviderCluster)
	waitForVM(t, frame.StagingClient(t, chosen), nn)
}

// TestStagingWorkspaceRefcounting verifies that a shared staging workspace
// is only released once the last resource using it is deleted.
func TestStagingWorkspaceRefcounting(t *testing.T) {
	t.Parallel()

	frame := NewFrame(t)
	provider := frame.NewProvider(t, "provider")
	createAcceptAPI(t, provider, "accept-vms", "vms")

	frame.StartBroker(t)

	consumer := frame.NewConsumer(t, "consumer")
	newVM := func(name string) *examplev1alpha1.VM {
		return &examplev1alpha1.VM{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
			},
			Spec: examplev1alpha1.VMSpec{
				Arch:   "x86_64",
				Memory: 512,
			},
		}
	}
	vm1 := newVM("test-vm-1")
	vm2 := newVM("test-vm-2")
	require.NoError(t, consumer.Client.Create(t.Context(), vm1))
	require.NoError(t, consumer.Client.Create(t.Context(), vm2))

	staging := frame.StagingClient(t, provider)
	waitForVM(t, staging, types.NamespacedName{Namespace: "default", Name: vm1.Name})
	waitForVM(t, staging, types.NamespacedName{Namespace: "default", Name: vm2.Name})

	// Both resources share one staging workspace.
	require.Len(t, frame.listStagingWorkspaces(t), 1)

	require.NoError(t, consumer.Client.Delete(t.Context(), vm1))
	require.Eventually(t, func() bool {
		err := consumer.Client.Get(t.Context(), types.NamespacedName{Namespace: "default", Name: vm1.Name}, &examplev1alpha1.VM{})
		return apierrors.IsNotFound(err)
	}, wait.ForeverTestTimeout, time.Second, "first vm should be deleted")
	require.Never(t, func() bool {
		return len(frame.listStagingWorkspaces(t)) == 0
	}, 5*time.Second, time.Second, "staging workspace must survive while still referenced")

	require.NoError(t, consumer.Client.Delete(t.Context(), vm2))
	require.Eventually(t, func() bool {
		return len(frame.listStagingWorkspaces(t)) == 0
	}, wait.ForeverTestTimeout, time.Second, "staging workspace should be released with the last resource")

	require.Eventually(t, func() bool {
		return len(frame.listAssignments(t)) == 0
	}, wait.ForeverTestTimeout, time.Second, "assignments should be deleted")
}
