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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

// TestStatusReflection verifies that provider status updates on the staging
// copy reach the consumer resource.
func TestStatusReflection(t *testing.T) {
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

	// The provider marks the staging copy available.
	markVMAvailable(t, staging, nn, "x86_64")

	require.Eventually(t, func() bool {
		current := &examplev1alpha1.VM{}
		if err := consumer.Client.Get(t.Context(), nn, current); err != nil {
			t.Logf("getting consumer vm: %v", err)
			return false
		}
		if current.Status.Status != pmbrokerv1alpha1.StatusAvailable {
			t.Logf("consumer vm status is %q", current.Status.Status)
			return false
		}
		return true
	}, wait.ForeverTestTimeout, time.Second, "consumer vm status should become available")
}

// TestConditionsSurfaced verifies the conditions and phases on the
// AcceptAPI, the Assignment, and the StagingWorkspace.
func TestConditionsSurfaced(t *testing.T) {
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

	require.Eventually(t, func() bool {
		acceptAPI := &pmbrokerv1alpha1.AcceptAPI{}
		if err := provider.Client.Get(t.Context(), types.NamespacedName{Name: "accept-vms"}, acceptAPI); err != nil {
			t.Logf("getting acceptapi: %v", err)
			return false
		}
		for _, condType := range []string{
			pmbrokerv1alpha1.AcceptAPIConditionBindingVerified,
			pmbrokerv1alpha1.AcceptAPIConditionReady,
		} {
			cond := meta.FindStatusCondition(acceptAPI.Status.Conditions, condType)
			if cond == nil || cond.Status != metav1.ConditionTrue {
				t.Logf("acceptapi condition %s: %+v", condType, cond)
				return false
			}
		}
		return true
	}, wait.ForeverTestTimeout, time.Second, "acceptapi conditions should become true")

	assignment := waitForBoundAssignment(t, frame)
	require.Equal(t, provider.ClusterName, assignment.Status.ProviderCluster)
	require.Equal(t, "accept-vms", assignment.Status.AcceptAPIName)
	require.NotEmpty(t, assignment.Status.StagingWorkspace)

	require.Eventually(t, func() bool {
		workspaces := frame.listStagingWorkspaces(t)
		if len(workspaces) != 1 {
			t.Logf("want exactly one staging workspace, have %d", len(workspaces))
			return false
		}
		sw := workspaces[0]
		if sw.Status.Phase != pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady {
			t.Logf("staging workspace phase is %q", sw.Status.Phase)
			return false
		}
		for _, condType := range []string{
			pmcoordbrokerv1alpha1.StagingWorkspaceConditionWorkspaceReady,
			pmcoordbrokerv1alpha1.StagingWorkspaceConditionBindingReady,
		} {
			cond := meta.FindStatusCondition(sw.Status.Conditions, condType)
			if cond == nil || cond.Status != metav1.ConditionTrue {
				t.Logf("staging workspace condition %s: %+v", condType, cond)
				return false
			}
		}
		return true
	}, wait.ForeverTestTimeout, time.Second, "staging workspace conditions should become true")
}

// TestVerificationBindingNeverBound verifies that an AcceptAPI referencing
// an export that cannot be bound stays unverified.
func TestVerificationBindingNeverBound(t *testing.T) {
	t.Parallel()

	frame := NewFrame(t)
	provider := frame.NewProvider(t, "provider")

	acceptAPI := &pmbrokerv1alpha1.AcceptAPI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "accept-unbindable",
		},
		Spec: pmbrokerv1alpha1.AcceptAPISpec{
			GVR: metav1.GroupVersionResource{
				Group:    "example.platform-mesh.io",
				Version:  "v1alpha1",
				Resource: "vms",
			},
			APIExportName: "does-not-exist",
		},
	}
	require.NoError(t, provider.Client.Create(t.Context(), acceptAPI))

	frame.StartBroker(t)

	require.Eventually(t, func() bool {
		return len(frame.verificationWorkspaces(t)) == 1
	}, wait.ForeverTestTimeout, time.Second, "verification workspace should be created")

	require.Eventually(t, func() bool {
		current := &pmbrokerv1alpha1.AcceptAPI{}
		if err := provider.Client.Get(t.Context(), types.NamespacedName{Name: "accept-unbindable"}, current); err != nil {
			t.Logf("getting acceptapi: %v", err)
			return false
		}
		cond := meta.FindStatusCondition(current.Status.Conditions, pmbrokerv1alpha1.AcceptAPIConditionBindingVerified)
		return cond != nil && cond.Status != metav1.ConditionTrue
	}, wait.ForeverTestTimeout, time.Second, "BindingVerified condition should be reported without becoming true")

	require.Never(t, func() bool {
		current := &pmbrokerv1alpha1.AcceptAPI{}
		if err := provider.Client.Get(t.Context(), types.NamespacedName{Name: "accept-unbindable"}, current); err != nil {
			t.Logf("getting acceptapi: %v", err)
			return false
		}
		cond := meta.FindStatusCondition(current.Status.Conditions, pmbrokerv1alpha1.AcceptAPIConditionBindingVerified)
		return cond != nil && cond.Status == metav1.ConditionTrue
	}, 5*time.Second, time.Second, "BindingVerified must not become true for a missing export")
}
