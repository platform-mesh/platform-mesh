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

package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestAddToScheme(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))

	tests := []struct {
		name string
		kind string
		obj  runtime.Object
	}{
		{name: "Assignment", kind: "Assignment", obj: &Assignment{}},
		{name: "AssignmentList", kind: "AssignmentList", obj: &AssignmentList{}},
		{name: "Migration", kind: "Migration", obj: &Migration{}},
		{name: "MigrationList", kind: "MigrationList", obj: &MigrationList{}},
		{name: "MigrationConfiguration", kind: "MigrationConfiguration", obj: &MigrationConfiguration{}},
		{name: "MigrationConfigurationList", kind: "MigrationConfigurationList", obj: &MigrationConfigurationList{}},
		{name: "StagingWorkspace", kind: "StagingWorkspace", obj: &StagingWorkspace{}},
		{name: "StagingWorkspaceList", kind: "StagingWorkspaceList", obj: &StagingWorkspaceList{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gvk := SchemeGroupVersion.WithKind(tt.kind)
			assert.True(t, scheme.Recognizes(gvk), "scheme should recognize %s", gvk)

			obj, err := scheme.New(gvk)
			require.NoError(t, err)
			assert.IsType(t, tt.obj, obj)
		})
	}
}

func TestDeepCopyRoundTrip(t *testing.T) {
	t.Run("Assignment", func(t *testing.T) {
		orig := &Assignment{
			Spec: AssignmentSpec{
				ConsumerCluster: "consumer-a",
				GVR:             metav1.GroupVersionResource{Group: "group.example.com", Version: "v1", Resource: "things"},
				Namespace:       "ns",
				Name:            "thing-1",
				ProviderCluster: "provider-b",
				AcceptAPIName:   "accept-things",
			},
			Status: AssignmentStatus{
				StagingWorkspace: "abc123",
				Phase:            AssignmentPhaseBound,
			},
		}

		cp := orig.DeepCopy()
		assert.Equal(t, orig, cp)

		cp.Spec.ProviderCluster = "provider-c"
		assert.NotEqual(t, orig.Spec.ProviderCluster, cp.Spec.ProviderCluster)
	})

	t.Run("StagingWorkspace", func(t *testing.T) {
		orig := &StagingWorkspace{
			Spec: StagingWorkspaceSpec{
				ConsumerCluster: "consumer-a",
				ProviderCluster: "provider-cluster-b",
				APIExportName:   "things.example.com",
			},
			Status: StagingWorkspaceStatus{
				Phase:       StagingWorkspacePhaseReady,
				ClusterName: "staging-abc123",
			},
		}

		cp := orig.DeepCopy()
		assert.Equal(t, orig, cp)

		cp.Status.Phase = StagingWorkspacePhaseTerminating
		assert.NotEqual(t, orig.Status.Phase, cp.Status.Phase)
	})
}
