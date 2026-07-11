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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

// TestSpecUpdateWithoutMigration verifies that spec updates still matching
// the AcceptAPI are synced without triggering a migration.
func TestSpecUpdateWithoutMigration(t *testing.T) {
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

	updateVM(t, consumer.Client, nn, func(vm *examplev1alpha1.VM) {
		vm.Spec.Memory = 1024
	})

	require.Eventually(t, func() bool {
		current := &examplev1alpha1.VM{}
		if err := staging.Get(t.Context(), nn, current); err != nil {
			t.Logf("getting staging vm: %v", err)
			return false
		}
		return current.Spec.Memory == 1024
	}, wait.ForeverTestTimeout, time.Second, "staging copy should receive the spec update")

	require.Empty(t, frame.listMigrations(t))
}

// TestNonDefaultNamespace verifies that the broker creates non-default
// namespaces in the staging workspace for namespaced resources.
func TestNonDefaultNamespace(t *testing.T) {
	t.Parallel()

	frame := NewFrame(t)
	provider := frame.NewProvider(t, "provider")
	createAcceptAPI(t, provider, "accept-vms", "vms")

	frame.StartBroker(t)

	consumer := frame.NewConsumer(t, "consumer")
	require.NoError(t, consumer.Client.Create(t.Context(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "apps"},
	}))

	vm := &examplev1alpha1.VM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vm",
			Namespace: "apps",
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
}

// TestRelatedResourceRemovalKeepsConsumerCopy documents current behavior:
// removing a related resource from the staging copy's status orphans the
// copy in the consumer workspace instead of deleting it.
func TestRelatedResourceRemovalKeepsConsumerCopy(t *testing.T) {
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

	require.Eventually(t, func() bool {
		current := &examplev1alpha1.VM{}
		if err := staging.Get(t.Context(), nn, current); err != nil {
			t.Logf("getting staging vm: %v", err)
			return false
		}
		current.Status.RelatedResources = nil
		if err := staging.Status().Update(t.Context(), current); err != nil {
			t.Logf("updating staging vm status: %v", err)
			return false
		}
		return true
	}, wait.ForeverTestTimeout, time.Second)

	require.Never(t, func() bool {
		err := consumer.Client.Get(t.Context(), cmName, &corev1.ConfigMap{})
		return apierrors.IsNotFound(err)
	}, 10*time.Second, time.Second, "the consumer copy currently stays around after removal from the status")
}

// TestPermissionClaimMirroring verifies that permission claims of the
// provider's export are mirrored as accepted claims on the staging binding.
func TestPermissionClaimMirroring(t *testing.T) {
	t.Parallel()

	frame := NewFrame(t)
	provider := frame.NewProvider(t, "provider", configMapsClaim())
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

	binding := &kcpapisv1alpha2.APIBinding{}
	require.NoError(t, staging.Get(t.Context(), types.NamespacedName{Name: exampleExportName}, binding))

	found := false
	for _, claim := range binding.Spec.PermissionClaims {
		if claim.Resource == "configmaps" && claim.Group == "" {
			require.Equal(t, kcpapisv1alpha2.ClaimAccepted, claim.State)
			found = true
		}
	}
	require.True(t, found, "staging binding should mirror the provider's configmaps claim, has: %s", toYAML(t, binding.Spec.PermissionClaims))
}

// TestClusterScopedResource verifies that cluster-scoped resources are
// brokered like namespaced ones.
func TestClusterScopedResource(t *testing.T) {
	t.Parallel()

	frame := NewFrame(t)
	provider := frame.NewProvider(t, "provider")
	createAcceptAPI(t, provider, "accept-dnszones", "dnszones")

	frame.StartBroker(t)

	consumer := frame.NewConsumer(t, "consumer")
	zone := &examplev1alpha1.DNSZone{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-zone",
		},
		Spec: examplev1alpha1.DNSZoneSpec{
			Domain: "example.com",
			TTL:    300,
		},
	}
	require.NoError(t, consumer.Client.Create(t.Context(), zone))
	nn := types.NamespacedName{Name: zone.Name}

	staging := frame.StagingClient(t, provider)
	require.Eventually(t, func() bool {
		current := &examplev1alpha1.DNSZone{}
		if err := staging.Get(t.Context(), nn, current); err != nil {
			t.Logf("getting staging dnszone: %v", err)
			return false
		}
		return current.Spec.Domain == "example.com" && current.Spec.TTL == 300
	}, wait.ForeverTestTimeout, time.Second, "staging copy of the cluster-scoped resource should exist")

	require.Eventually(t, func() bool {
		current := &examplev1alpha1.DNSZone{}
		if err := staging.Get(t.Context(), nn, current); err != nil {
			t.Logf("getting staging dnszone: %v", err)
			return false
		}
		current.Status.Status = pmbrokerv1alpha1.StatusAvailable
		if err := staging.Status().Update(t.Context(), current); err != nil {
			t.Logf("updating staging dnszone status: %v", err)
			return false
		}
		return true
	}, wait.ForeverTestTimeout, time.Second)

	require.Eventually(t, func() bool {
		current := &examplev1alpha1.DNSZone{}
		if err := consumer.Client.Get(t.Context(), nn, current); err != nil {
			t.Logf("getting consumer dnszone: %v", err)
			return false
		}
		return current.Status.Status == pmbrokerv1alpha1.StatusAvailable
	}, wait.ForeverTestTimeout, time.Second, "consumer dnszone should reflect the provider status")

	require.NoError(t, consumer.Client.Delete(t.Context(), zone))

	require.Eventually(t, func() bool {
		err := consumer.Client.Get(t.Context(), nn, &examplev1alpha1.DNSZone{})
		return apierrors.IsNotFound(err)
	}, wait.ForeverTestTimeout, time.Second, "consumer dnszone should be deleted")

	require.Eventually(t, func() bool {
		return len(frame.listAssignments(t)) == 0 && len(frame.listStagingWorkspaces(t)) == 0
	}, wait.ForeverTestTimeout, time.Second, "assignment and staging workspace should be cleaned up")
}
