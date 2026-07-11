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

	examplev1alpha1 "go.platform-mesh.io/resource-broker/api/example/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kcp-dev/multicluster-provider/envtest"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	"github.com/kcp-dev/sdk/apis/core"
)

// TestExportViaRefPath verifies that discovery resolves endpoint slices
// referencing an APIExport in another workspace, and that deleting the
// slice stops brokering.
//
// The slice deletion is exercised here rather than on the broker home's
// default slice: kcp immediately recreates the default slice of an
// APIExport, so only a manually created slice stays deleted.
func TestExportViaRefPath(t *testing.T) {
	t.Parallel()

	frame := NewFrame(t)
	provider := frame.NewProvider(t, "provider")
	createAcceptAPI(t, provider, "accept-vms", "vms")

	_, exporterPath := envtest.NewWorkspaceFixture(t, kcpClient, core.RootCluster.Path(), envtest.WithNamePrefix("exporter"))
	exporterClient := kcpClient.Cluster(exporterPath)
	applySchemas(t, exporterClient,
		"apiresourceschema-certificates.example.platform-mesh.io.yaml",
		"apiresourceschema-dnszones.example.platform-mesh.io.yaml",
		"apiresourceschema-vms.example.platform-mesh.io.yaml",
	)
	createExport(t, exporterClient, "apiexport-example.platform-mesh.io.yaml", configMapsClaim())

	slice := &kcpapisv1alpha1.APIExportEndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cross-example",
		},
		Spec: kcpapisv1alpha1.APIExportEndpointSliceSpec{
			APIExport: kcpapisv1alpha1.ExportBindingReference{
				Path: exporterPath.String(),
				Name: exampleExportName,
			},
		},
	}
	require.NoError(t, frame.HomeClient.Create(t.Context(), slice))

	frame.StartBroker(t)

	_, consumerPath := envtest.NewWorkspaceFixture(t, kcpClient, core.RootCluster.Path(), envtest.WithNamePrefix("consumer"))
	consumerClient := kcpClient.Cluster(consumerPath)
	createBinding(t, consumerClient, exampleExportName, exporterPath.String(), acceptClaims(configMapsClaim()))
	waitEndpointSlice(t, frame.HomeClient, "cross-example")

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
	require.NoError(t, consumerClient.Create(t.Context(), vm))
	nn := types.NamespacedName{Namespace: vm.Namespace, Name: vm.Name}

	staging := frame.StagingClient(t, provider)
	waitForVM(t, staging, nn)

	require.NoError(t, frame.HomeClient.Delete(t.Context(), slice))

	// Give discovery a moment to process the deletion before updating.
	time.Sleep(3 * time.Second)

	updateVM(t, consumerClient, nn, func(vm *examplev1alpha1.VM) {
		vm.Spec.Memory = 2048
	})

	require.Never(t, func() bool {
		current := &examplev1alpha1.VM{}
		if err := staging.Get(t.Context(), nn, current); err != nil {
			t.Logf("getting staging vm: %v", err)
			return false
		}
		return current.Spec.Memory == 2048
	}, 10*time.Second, time.Second, "consumer updates must not propagate after the endpoint slice is gone")
}
