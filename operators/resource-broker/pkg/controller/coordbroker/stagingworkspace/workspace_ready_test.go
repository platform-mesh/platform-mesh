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

package stagingworkspace

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

func readyWorkspace(name, cluster string) *kcptenancyv1alpha1.Workspace {
	return &kcptenancyv1alpha1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: kcptenancyv1alpha1.WorkspaceSpec{
			Cluster: cluster,
		},
		Status: kcptenancyv1alpha1.WorkspaceStatus{
			Phase: kcpcorev1alpha1.LogicalClusterPhaseReady,
		},
	}
}

func TestWorkspaceReadyGetName(t *testing.T) {
	s := &workspaceReadySubroutine{}
	assert.Equal(t, pmcoordbrokerv1alpha1.StagingWorkspaceConditionWorkspaceReady, s.GetName())
}

func TestWorkspaceReadyFinalizers(t *testing.T) {
	s := &workspaceReadySubroutine{}
	assert.Equal(t, []string{StagingFinalizer}, s.Finalizers(nil))
}

func TestWorkspaceReadyProcess(t *testing.T) {
	tests := []struct {
		name        string
		treeObjs    []ctrlruntimeclient.Object
		wantPending bool
		wantOK      bool
		wantPhase   pmcoordbrokerv1alpha1.StagingWorkspacePhase
		wantCluster string
		verify      func(t *testing.T, treeClient ctrlruntimeclient.Client)
	}{
		{
			name:        "creates workspace",
			wantPending: true,
			wantPhase:   pmcoordbrokerv1alpha1.StagingWorkspacePhasePending,
			verify: func(t *testing.T, treeClient ctrlruntimeclient.Client) {
				ws := &kcptenancyv1alpha1.Workspace{}
				require.NoError(t, treeClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: testName}, ws))
			},
		},
		{
			name: "waits for workspace to become ready",
			treeObjs: []ctrlruntimeclient.Object{&kcptenancyv1alpha1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: testName},
			}},
			wantPending: true,
			wantPhase:   pmcoordbrokerv1alpha1.StagingWorkspacePhasePending,
		},
		{
			name:        "ready workspace records cluster name",
			treeObjs:    []ctrlruntimeclient.Object{readyWorkspace(testName, "logical-cluster-1")},
			wantOK:      true,
			wantCluster: "logical-cluster-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, clients := testOptions(t, tt.treeObjs, nil, nil)
			s := &workspaceReadySubroutine{opts: opts}
			sw := testStagingWorkspace()

			result, err := s.Process(t.Context(), sw)
			require.NoError(t, err)
			assert.Equal(t, tt.wantPending, result.IsPending())
			if tt.wantOK {
				assert.True(t, result.IsContinue())
				assert.Zero(t, result.Requeue())
			}
			assert.Equal(t, tt.wantPhase, sw.Status.Phase)
			assert.Equal(t, tt.wantCluster, sw.Status.ClusterName)
			if tt.verify != nil {
				tt.verify(t, clients.tree)
			}
		})
	}
}

func TestWorkspaceReadyProcessTerminatingWorkspace(t *testing.T) {
	opts, clients := testOptions(t, []ctrlruntimeclient.Object{
		readyWorkspace(testName, "logical-cluster-1"),
	}, nil, nil)
	s := &workspaceReadySubroutine{opts: opts}

	// Add a finalizer so Delete leaves the workspace in terminating state.
	ws := &kcptenancyv1alpha1.Workspace{}
	require.NoError(t, clients.tree.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: testName}, ws))
	ws.Finalizers = []string{"keep/me"}
	require.NoError(t, clients.tree.Update(t.Context(), ws))
	require.NoError(t, clients.tree.Delete(t.Context(), ws))

	sw := testStagingWorkspace()
	result, err := s.Process(t.Context(), sw)
	require.NoError(t, err)
	assert.True(t, result.IsPending())
	assert.Equal(t, pmcoordbrokerv1alpha1.StagingWorkspacePhasePending, sw.Status.Phase)
}

func TestWorkspaceReadyFinalize(t *testing.T) {
	tests := []struct {
		name        string
		treeObjs    []ctrlruntimeclient.Object
		wantPending bool
		wantOK      bool
		verify      func(t *testing.T, treeClient ctrlruntimeclient.Client)
	}{
		{
			name:   "workspace already gone",
			wantOK: true,
		},
		{
			name:        "deletes workspace and waits",
			treeObjs:    []ctrlruntimeclient.Object{readyWorkspace(testName, "logical-cluster-1")},
			wantPending: true,
			verify: func(t *testing.T, treeClient ctrlruntimeclient.Client) {
				ws := &kcptenancyv1alpha1.Workspace{}
				err := treeClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: testName}, ws)
				assert.True(t, ctrlruntimeclient.IgnoreNotFound(err) == nil && err != nil, "workspace should be deleted, got: %v", err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, clients := testOptions(t, tt.treeObjs, nil, nil)
			s := &workspaceReadySubroutine{opts: opts}
			sw := testStagingWorkspace()

			result, err := s.Finalize(t.Context(), sw)
			require.NoError(t, err)
			assert.Equal(t, tt.wantPending, result.IsPending())
			if tt.wantOK {
				assert.True(t, result.IsContinue())
				assert.Zero(t, result.Requeue())
			}
			assert.Equal(t, pmcoordbrokerv1alpha1.StagingWorkspacePhaseTerminating, sw.Status.Phase)
			if tt.verify != nil {
				tt.verify(t, clients.tree)
			}
		})
	}
}

func TestWorkspaceReadyFinalizeTerminatingWorkspace(t *testing.T) {
	opts, clients := testOptions(t, []ctrlruntimeclient.Object{
		readyWorkspace(testName, "logical-cluster-1"),
	}, nil, nil)
	s := &workspaceReadySubroutine{opts: opts}

	ws := &kcptenancyv1alpha1.Workspace{}
	require.NoError(t, clients.tree.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: testName}, ws))
	ws.Finalizers = []string{"keep/me"}
	require.NoError(t, clients.tree.Update(t.Context(), ws))
	require.NoError(t, clients.tree.Delete(t.Context(), ws))

	sw := testStagingWorkspace()
	result, err := s.Finalize(t.Context(), sw)
	require.NoError(t, err)
	assert.True(t, result.IsPending())
}
