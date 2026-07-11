//go:build kube_legacy

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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	brokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"
	examplev1alpha1 "go.platform-mesh.io/resource-broker/api/example/v1alpha1"
	"go.platform-mesh.io/resource-broker/pkg/broker"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

// TestMigrationNoStages tests that migrations are created and processed
// correctly.
func TestMigrationNoStages(t *testing.T) {
	t.Skip("pending kcp test rewrite")
	t.Parallel()

	frame := NewFrame(t)
	vmRules := resourceRules("example.platform-mesh.io", "vms")
	frame.Coordination.AddUser(t, "broker", coordinationRules(vmRules...))

	mgrOptions := frame.Options(t)
	mgrOptions.WatchKinds = []string{"VM.v1alpha1.example.platform-mesh.io"}

	mgr, err := broker.New(mgrOptions)
	require.NoError(t, err)

	go func() {
		err := mgr.Start(t.Context())
		assert.NoError(t, err)
	}()

	t.Log("Create MigrationConfig in coordination control plane")
	err = frame.Coordination.Cluster.GetClient().Create(
		t.Context(),
		&brokerv1alpha1.MigrationConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "migrate-vm",
				Namespace: "default",
			},
			Spec: brokerv1alpha1.MigrationConfigurationSpec{
				From: metav1.GroupVersionKind{
					Group:   "example.platform-mesh.io",
					Version: "v1alpha1",
					Kind:    "VM",
				},
				To: metav1.GroupVersionKind{
					Group:   "example.platform-mesh.io",
					Version: "v1alpha1",
					Kind:    "VM",
				},
				// No stages for test, the migration should still be
				// created in the platform control plane.
				Stages: []brokerv1alpha1.MigrationStage{},
			},
		},
	)
	require.NoError(t, err)

	t.Log("Create x86_64 provider and AcceptAPI")
	x86Provider := frame.NewProvider(t, "x86", providerRules(vmRules...))
	err = x86Provider.Cluster.GetClient().Create(
		t.Context(),
		&brokerv1alpha1.AcceptAPI{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "accept-x86",
				Namespace: "default",
			},
			Spec: brokerv1alpha1.AcceptAPISpec{
				GVR: metav1.GroupVersionResource{
					Group:    "example.platform-mesh.io",
					Version:  "v1alpha1",
					Resource: "vms",
				},
				Filters: []brokerv1alpha1.Filter{
					{
						Key:     "arch",
						ValueIn: []string{"x86_64"},
					},
				},
			},
		},
	)
	require.NoError(t, err)

	t.Log("Create arm64 provider and AcceptAPI")
	armProvider := frame.NewProvider(t, "arm64", providerRules(vmRules...))
	err = armProvider.Cluster.GetClient().Create(
		t.Context(),
		&brokerv1alpha1.AcceptAPI{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "accept-arm64",
				Namespace: "default",
			},
			Spec: brokerv1alpha1.AcceptAPISpec{
				GVR: metav1.GroupVersionResource{
					Group:    "example.platform-mesh.io",
					Version:  "v1alpha1",
					Resource: "vms",
				},
				Filters: []brokerv1alpha1.Filter{
					{
						Key:     "arch",
						ValueIn: []string{"arm64"},
					},
				},
			},
		},
	)
	require.NoError(t, err)

	vmName := "test-vm"
	vmNamespace := "default" //nolint:goconst,nolintlint

	t.Log("Create Consumer with one VM")
	consumer := frame.NewConsumer(t, "consumer")
	err = consumer.Cluster.GetClient().Create(
		t.Context(),
		&examplev1alpha1.VM{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmName,
				Namespace: vmNamespace,
			},
			Spec: examplev1alpha1.VMSpec{
				Arch:   "x86_64",
				Memory: 512,
			},
		},
	)
	require.NoError(t, err)

	providerVMName := sanitizeClusterName("consumer#consumer#cluster") + "-" + vmName

	t.Log("Wait for VM to appear in x86 provider")
	vm := &examplev1alpha1.VM{}
	require.Eventually(t, func() bool {
		err := x86Provider.Cluster.GetClient().Get(
			t.Context(),
			types.NamespacedName{
				Name:      providerVMName,
				Namespace: vmNamespace,
			},
			vm,
		)
		if err != nil {
			return false
		}
		return vm.Spec.Arch == "x86_64"
	}, wait.ForeverTestTimeout, time.Second)

	t.Log("Update VM to arm64 in Consumer")
	vm = &examplev1alpha1.VM{}
	err = consumer.Cluster.GetClient().Get(
		t.Context(),
		types.NamespacedName{
			Name:      vmName,
			Namespace: vmNamespace,
		},
		vm,
	)
	require.NoError(t, err)
	vm.Spec.Arch = "arm64"

	err = consumer.Cluster.GetClient().Update(t.Context(), vm)
	require.NoError(t, err)

	t.Log("Check that Migration is created in coordination control plane")
	require.Eventually(t, func() bool {
		migration := &brokerv1alpha1.Migration{}
		err := frame.Coordination.Cluster.GetClient().Get(
			t.Context(),
			types.NamespacedName{
				Name:      vmName,
				Namespace: vmNamespace,
			},
			migration,
		)
		return err == nil
	}, wait.ForeverTestTimeout, time.Second)

	t.Log("Wait for Migration to complete in coordination control plane")
	assert.Eventually(t, func() bool {
		migration := &brokerv1alpha1.Migration{}
		err := frame.Coordination.Cluster.GetClient().Get(
			t.Context(),
			types.NamespacedName{
				Name:      vmName,
				Namespace: vmNamespace,
			},
			migration,
		)
		if err != nil {
			return false
		}
		t.Logf("Migration status: %+v", migration.Status)
		return migration.Status.State == brokerv1alpha1.MigrationStateCutoverCompleted
	}, wait.ForeverTestTimeout, time.Second)

	t.Log("Check that VM appears in arm64 provider")
	require.Eventually(t, func() bool {
		vm := &examplev1alpha1.VM{}
		err := armProvider.Cluster.GetClient().Get(
			t.Context(),
			types.NamespacedName{
				Name:      providerVMName,
				Namespace: vmNamespace,
			},
			vm,
		)
		if err != nil {
			return false
		}
		return vm.Spec.Arch == "arm64"
	}, wait.ForeverTestTimeout, time.Second)

	t.Log("Check that VM is removed from x86 provider")
	require.Eventually(t, func() bool {
		vm := &examplev1alpha1.VM{}
		err := x86Provider.Cluster.GetClient().Get(
			t.Context(),
			types.NamespacedName{
				Name:      providerVMName,
				Namespace: vmNamespace,
			},
			vm,
		)
		switch {
		case apierrors.IsNotFound(err):
			t.Logf("x86 VM successfully deleted")
			return true
		case err != nil:
			t.Logf("Error checking x86 VM deletion: %v", err)
		default:
			t.Logf("x86 VM still exists: %+v", vm)
		}
		return false
	}, wait.ForeverTestTimeout, time.Second)
}

// TestMigrationWithStages tests that migrations are created and processed
// correctly with stages defined.
func TestMigrationWithStages(t *testing.T) {
	t.Skip("pending kcp test rewrite")
	t.Parallel()

	frame := NewFrame(t)
	vmRules := resourceRules("example.platform-mesh.io", "vms")
	frame.Coordination.AddUser(t, "broker", coordinationRules(vmRules...))

	mgrOptions := frame.Options(t)
	mgrOptions.WatchKinds = []string{"VM.v1alpha1.example.platform-mesh.io"}

	mgr, err := broker.New(mgrOptions)
	require.NoError(t, err)

	go func() {
		err := mgr.Start(t.Context())
		assert.NoError(t, err)
	}()

	t.Log("Create MigrationConfig in coordination control plane")
	err = frame.Coordination.Cluster.GetClient().Create(
		t.Context(),
		&brokerv1alpha1.MigrationConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "migrate-vm",
				Namespace: "default",
			},
			Spec: brokerv1alpha1.MigrationConfigurationSpec{
				From: metav1.GroupVersionKind{
					Group:   "example.platform-mesh.io",
					Version: "v1alpha1",
					Kind:    "VM",
				},
				To: metav1.GroupVersionKind{
					Group:   "example.platform-mesh.io",
					Version: "v1alpha1",
					Kind:    "VM",
				},
				Stages: []brokerv1alpha1.MigrationStage{
					{
						Name: "dummy-configmap",
						Templates: map[string]runtime.RawExtension{
							"dummy": {
								Object: &corev1.ConfigMap{
									TypeMeta: metav1.TypeMeta{
										APIVersion: "v1",
										Kind:       "ConfigMap",
									},
									ObjectMeta: metav1.ObjectMeta{
										Name: "dummy-config",
									},
									Data: map[string]string{
										"key": "value",
									},
								},
							},
						},
						SuccessConditions: []string{},
					},
				},
			},
		},
	)
	require.NoError(t, err)

	t.Log("Create x86_64 provider and AcceptAPI")
	x86Provider := frame.NewProvider(t, "x86", providerRules(vmRules...))
	err = x86Provider.Cluster.GetClient().Create(
		t.Context(),
		&brokerv1alpha1.AcceptAPI{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "accept-x86",
				Namespace: "default",
			},
			Spec: brokerv1alpha1.AcceptAPISpec{
				GVR: metav1.GroupVersionResource{
					Group:    "example.platform-mesh.io",
					Version:  "v1alpha1",
					Resource: "vms",
				},
				Filters: []brokerv1alpha1.Filter{
					{
						Key:     "arch",
						ValueIn: []string{"x86_64"},
					},
				},
			},
		},
	)
	require.NoError(t, err)

	t.Log("Create arm64 provider and AcceptAPI")
	armProvider := frame.NewProvider(t, "arm64", providerRules(vmRules...))
	err = armProvider.Cluster.GetClient().Create(
		t.Context(),
		&brokerv1alpha1.AcceptAPI{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "accept-arm64",
				Namespace: "default",
			},
			Spec: brokerv1alpha1.AcceptAPISpec{
				GVR: metav1.GroupVersionResource{
					Group:    "example.platform-mesh.io",
					Version:  "v1alpha1",
					Resource: "vms",
				},
				Filters: []brokerv1alpha1.Filter{
					{
						Key:     "arch",
						ValueIn: []string{"arm64"},
					},
				},
			},
		},
	)
	require.NoError(t, err)

	vmName := "test-vm"
	vmNamespace := "default" //nolint:goconst,nolintlint

	t.Log("Create Consumer with one VM")
	consumer := frame.NewConsumer(t, "consumer", consumerRules(vmRules...))
	err = consumer.Cluster.GetClient().Create(
		t.Context(),
		&examplev1alpha1.VM{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmName,
				Namespace: vmNamespace,
			},
			Spec: examplev1alpha1.VMSpec{
				Arch:   "x86_64",
				Memory: 512,
			},
		},
	)
	require.NoError(t, err)

	providerVMName := sanitizeClusterName("consumer#consumer#cluster") + "-" + vmName

	t.Log("Wait for VM to appear in x86 provider")
	vm := &examplev1alpha1.VM{}
	require.Eventually(t, func() bool {
		err := x86Provider.Cluster.GetClient().Get(
			t.Context(),
			types.NamespacedName{
				Name:      providerVMName,
				Namespace: vmNamespace,
			},
			vm,
		)
		if err != nil {
			return false
		}
		return vm.Spec.Arch == "x86_64"
	}, wait.ForeverTestTimeout, time.Second)

	t.Log("Update VM to arm64 in Consumer")
	vm = &examplev1alpha1.VM{}
	err = consumer.Cluster.GetClient().Get(
		t.Context(),
		types.NamespacedName{
			Name:      vmName,
			Namespace: vmNamespace,
		},
		vm,
	)
	require.NoError(t, err)

	vm.Spec.Arch = "arm64"
	err = consumer.Cluster.GetClient().Update(t.Context(), vm)
	require.NoError(t, err)

	t.Log("Check that Migration is created in coordination control plane")
	migration := &brokerv1alpha1.Migration{}
	require.Eventually(t, func() bool {
		err := frame.Coordination.Cluster.GetClient().Get(
			t.Context(),
			types.NamespacedName{
				Name:      vmName,
				Namespace: vmNamespace,
			},
			migration,
		)
		return err == nil
	}, wait.ForeverTestTimeout, time.Second)
	t.Logf("Migration ID: %s", migration.Status.ID)

	t.Log("Wait for VM to appear in arm provider")
	require.Eventually(t, func() bool {
		vm := &examplev1alpha1.VM{}
		err := armProvider.Cluster.GetClient().Get(
			t.Context(),
			types.NamespacedName{
				Name:      providerVMName,
				Namespace: vmNamespace,
			},
			vm,
		)
		if err != nil {
			t.Logf("Error getting arm64 VM: %v", err)
			return false
		}
		return vm.Spec.Arch == "arm64"
	}, wait.ForeverTestTimeout, time.Second)

	t.Log("Check that VM is removed from x86 provider")
	assert.Eventually(t, func() bool {
		vm := &examplev1alpha1.VM{}
		err := x86Provider.Cluster.GetClient().Get(
			t.Context(),
			types.NamespacedName{
				Name:      providerVMName,
				Namespace: vmNamespace,
			},
			vm,
		)
		if apierrors.IsNotFound(err) {
			t.Logf("x86 VM successfully deleted")
			return true
		}
		t.Logf("Error checking x86 VM deletion: %v", err)
		if err == nil {
			t.Logf("x86 VM still exists: %+v", vm)
		}
		return false
	}, wait.ForeverTestTimeout, time.Second)
}

// sanitizeClusterName is a legacy shim for the removed
// pkg/broker/generic.SanitizeClusterName helper.
func sanitizeClusterName(cluster string) string {
	return strings.ReplaceAll(cluster, "#", ".")
}
