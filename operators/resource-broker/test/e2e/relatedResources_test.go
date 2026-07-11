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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

// TestRelatedResources tests that related resources are copied from
// the staging workspace back to the consumer workspace.
func TestRelatedResources(t *testing.T) {
	t.Parallel()

	frame := NewFrame(t)
	consumer := frame.NewConsumer(t, "consumer")
	provider := frame.NewProvider(t, "provider")

	t.Log("Create AcceptAPI in provider workspace")
	err := provider.Client.Create(
		t.Context(),
		&pmbrokerv1alpha1.AcceptAPI{
			ObjectMeta: metav1.ObjectMeta{
				Name: "accept-vms",
			},
			Spec: pmbrokerv1alpha1.AcceptAPISpec{
				GVR: metav1.GroupVersionResource{
					Group:    "example.platform-mesh.io",
					Version:  "v1alpha1",
					Resource: "vms",
				},
				APIExportName: exampleExportName,
			},
		},
	)
	require.NoError(t, err)

	frame.StartBroker(t)

	namespace := "default"
	vmName := "test-vm"

	t.Log("Create VM in consumer workspace")
	err = consumer.Client.Create(
		t.Context(),
		&examplev1alpha1.VM{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmName,
				Namespace: namespace,
			},
			Spec: examplev1alpha1.VMSpec{
				Arch:   "x86_64",
				Memory: 512,
			},
		},
	)
	require.NoError(t, err)

	t.Log("Wait for VM to appear in staging workspace")
	stagingClient := frame.StagingClient(t, provider)
	vm := &examplev1alpha1.VM{}
	require.Eventually(t, func() bool {
		err := stagingClient.Get(
			t.Context(),
			types.NamespacedName{
				Name:      vmName,
				Namespace: namespace,
			},
			vm,
		)
		if err != nil {
			t.Logf("error getting VM from staging workspace: %v", err)
			return false
		}
		return vm.Name == vmName
	}, wait.ForeverTestTimeout, time.Second)

	t.Log("Create related ConfigMap in staging workspace")
	cmName := "related-configmap"
	cmKey := "related-key"
	cmValue := "related-value"
	err = stagingClient.Create(
		t.Context(),
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: namespace,
			},
			Data: map[string]string{
				cmKey: cmValue,
			},
		},
	)
	require.NoError(t, err)

	t.Log("Update RelatedResources in staging workspace")
	require.Eventually(t, func() bool {
		vm := &examplev1alpha1.VM{}
		err := stagingClient.Get(
			t.Context(),
			types.NamespacedName{
				Name:      vmName,
				Namespace: namespace,
			},
			vm,
		)
		if err != nil {
			t.Logf("error getting VM from staging workspace: %v", err)
			return false
		}
		vm.Status.RelatedResources = pmbrokerv1alpha1.RelatedResources{
			"configmap": pmbrokerv1alpha1.RelatedResource{
				Namespace: namespace,
				Name:      cmName,
				GVK: metav1.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "ConfigMap",
				},
			},
		}

		err = stagingClient.Status().Update(
			t.Context(),
			vm,
		)
		return err == nil
	}, wait.ForeverTestTimeout, time.Second)

	t.Log("Wait for RelatedResource ConfigMap to appear in consumer workspace")
	require.Eventually(t, func() bool {
		cm := &corev1.ConfigMap{}
		err := consumer.Client.Get(
			t.Context(),
			types.NamespacedName{
				Name:      cmName,
				Namespace: namespace,
			},
			cm,
		)
		if err != nil {
			t.Logf("error getting related configmap from consumer workspace: %v", err)
			return false
		}
		return cm.Data[cmKey] == cmValue
	}, wait.ForeverTestTimeout, time.Second)
}
