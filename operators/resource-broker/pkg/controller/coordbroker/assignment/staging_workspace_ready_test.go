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

package assignment

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func testStagingWorkspaceName() string {
	return stagingWorkspaceName(testConsumerCluster, testProviderCluster, testExportName)
}

func testStagingWorkspace(phase pmcoordbrokerv1alpha1.StagingWorkspacePhase) *pmcoordbrokerv1alpha1.StagingWorkspace {
	return &pmcoordbrokerv1alpha1.StagingWorkspace{
		ObjectMeta: metav1.ObjectMeta{Name: testStagingWorkspaceName()},
		Spec: pmcoordbrokerv1alpha1.StagingWorkspaceSpec{
			ConsumerCluster: testConsumerCluster,
			ProviderCluster: testProviderCluster,
			APIExportName:   testExportName,
		},
		Status: pmcoordbrokerv1alpha1.StagingWorkspaceStatus{
			Phase: phase,
		},
	}
}

func TestStagingWorkspaceReadyGetName(t *testing.T) {
	s := &stagingWorkspaceReadySubroutine{}
	assert.Equal(t, pmcoordbrokerv1alpha1.AssignmentConditionStagingWorkspaceReady, s.GetName())
}

func TestStagingWorkspaceReadyFinalizers(t *testing.T) {
	s := &stagingWorkspaceReadySubroutine{}
	assert.Equal(t, []string{AssignmentFinalizer}, s.Finalizers(nil))
}

func TestStagingWorkspaceReadyProcess(t *testing.T) {
	tests := []struct {
		name         string
		assignment   *pmcoordbrokerv1alpha1.Assignment
		coordObjs    []ctrlruntimeclient.Object
		providerObjs []ctrlruntimeclient.Object
		wantErr      bool
		wantPhase    pmcoordbrokerv1alpha1.AssignmentPhase
		wantPending  bool
		check        func(t *testing.T, clients *testClients, assignment *pmcoordbrokerv1alpha1.Assignment)
	}{
		{
			name:       "missing accept api errors",
			assignment: testAssignment(),
			wantErr:    true,
		},
		{
			name:         "creates staging workspace",
			assignment:   testAssignment(),
			providerObjs: []ctrlruntimeclient.Object{testAcceptAPI()},
			wantPhase:    pmcoordbrokerv1alpha1.AssignmentPhasePending,
			wantPending:  true,
			check: func(t *testing.T, clients *testClients, assignment *pmcoordbrokerv1alpha1.Assignment) {
				assert.Equal(t, testExportName, assignment.Status.APIExportName)
				assert.Equal(t, testStagingWorkspaceName(), assignment.Status.StagingWorkspace)

				sw := &pmcoordbrokerv1alpha1.StagingWorkspace{}
				require.NoError(t, clients.coordination.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: testStagingWorkspaceName()}, sw))
				assert.Equal(t, testConsumerCluster, sw.Spec.ConsumerCluster)
				assert.Equal(t, testProviderCluster, sw.Spec.ProviderCluster)
				assert.Equal(t, testExportName, sw.Spec.APIExportName)
			},
		},
		{
			name: "resolved export name skips provider lookup",
			assignment: func() *pmcoordbrokerv1alpha1.Assignment {
				assignment := testAssignment()
				assignment.Status.APIExportName = testExportName
				return assignment
			}(),
			coordObjs:   []ctrlruntimeclient.Object{testStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady)},
			wantPhase:   pmcoordbrokerv1alpha1.AssignmentPhaseBound,
			wantPending: false,
		},
		{
			name:         "waits for staging workspace to become ready",
			assignment:   testAssignment(),
			coordObjs:    []ctrlruntimeclient.Object{testStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhasePending)},
			providerObjs: []ctrlruntimeclient.Object{testAcceptAPI()},
			wantPhase:    pmcoordbrokerv1alpha1.AssignmentPhasePending,
			wantPending:  true,
		},
		{
			name:         "ready staging workspace binds",
			assignment:   testAssignment(),
			coordObjs:    []ctrlruntimeclient.Object{testStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady)},
			providerObjs: []ctrlruntimeclient.Object{testAcceptAPI()},
			wantPhase:    pmcoordbrokerv1alpha1.AssignmentPhaseBound,
			wantPending:  false,
			check: func(t *testing.T, _ *testClients, assignment *pmcoordbrokerv1alpha1.Assignment) {
				assert.Equal(t, testStagingWorkspaceName(), assignment.Status.StagingWorkspace)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, clients := testOptions(t, tt.coordObjs, tt.providerObjs)
			s := &stagingWorkspaceReadySubroutine{opts: opts}
			ctx := subroutines.WithClient(t.Context(), clients.coordination)

			result, err := s.Process(ctx, tt.assignment)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantPhase, tt.assignment.Status.Phase)
			assert.Equal(t, tt.wantPending, result.IsPending())
			if tt.check != nil {
				tt.check(t, clients, tt.assignment)
			}
		})
	}
}

func TestStagingWorkspaceReadyProcessTerminatingWorkspace(t *testing.T) {
	sw := testStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady)
	sw.Finalizers = []string{"keep/me"}

	opts, clients := testOptions(t, []ctrlruntimeclient.Object{sw}, []ctrlruntimeclient.Object{testAcceptAPI()})
	require.NoError(t, clients.coordination.Delete(t.Context(), sw))

	s := &stagingWorkspaceReadySubroutine{opts: opts}
	ctx := subroutines.WithClient(t.Context(), clients.coordination)

	assignment := testAssignment()
	result, err := s.Process(ctx, assignment)
	require.NoError(t, err)
	assert.True(t, result.IsPending())
	assert.Equal(t, pmcoordbrokerv1alpha1.AssignmentPhasePending, assignment.Status.Phase)
}

func TestStagingWorkspaceReadyFinalize(t *testing.T) {
	boundAssignment := func(name string) *pmcoordbrokerv1alpha1.Assignment {
		assignment := testAssignment()
		assignment.Name = name
		assignment.Status.APIExportName = testExportName
		assignment.Status.StagingWorkspace = testStagingWorkspaceName()
		return assignment
	}

	tests := []struct {
		name        string
		assignment  *pmcoordbrokerv1alpha1.Assignment
		coordObjs   []ctrlruntimeclient.Object
		wantDeleted bool
	}{
		{
			name:        "no staging workspace recorded",
			assignment:  testAssignment(),
			wantDeleted: true,
		},
		{
			name:       "last reference deletes staging workspace",
			assignment: boundAssignment(testAssignmentName),
			coordObjs: []ctrlruntimeclient.Object{
				boundAssignment(testAssignmentName),
				testStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady),
			},
			wantDeleted: true,
		},
		{
			name:       "other reference keeps staging workspace",
			assignment: boundAssignment(testAssignmentName),
			coordObjs: []ctrlruntimeclient.Object{
				boundAssignment(testAssignmentName),
				boundAssignment("other-assignment"),
				testStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady),
			},
			wantDeleted: false,
		},
		{
			name:       "deleting other reference does not count",
			assignment: boundAssignment(testAssignmentName),
			coordObjs: []ctrlruntimeclient.Object{
				boundAssignment(testAssignmentName),
				func() *pmcoordbrokerv1alpha1.Assignment {
					other := boundAssignment("other-assignment")
					other.Finalizers = []string{AssignmentFinalizer}
					now := metav1.Now()
					other.DeletionTimestamp = &now
					return other
				}(),
				testStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady),
			},
			wantDeleted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, clients := testOptions(t, tt.coordObjs, nil)
			s := &stagingWorkspaceReadySubroutine{opts: opts}
			ctx := subroutines.WithClient(t.Context(), clients.coordination)

			result, err := s.Finalize(ctx, tt.assignment)
			require.NoError(t, err)
			assert.True(t, result.IsContinue())
			assert.Equal(t, pmcoordbrokerv1alpha1.AssignmentPhaseTerminating, tt.assignment.Status.Phase)

			err = clients.coordination.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: testStagingWorkspaceName()}, &pmcoordbrokerv1alpha1.StagingWorkspace{})
			if tt.wantDeleted {
				assert.True(t, apierrors.IsNotFound(err))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestStagingWorkspaceName(t *testing.T) {
	name := testStagingWorkspaceName()
	assert.Regexp(t, "^staging-[0-9a-f]{16}$", name)
	assert.Equal(t, name, stagingWorkspaceName(testConsumerCluster, testProviderCluster, testExportName))
	assert.NotEqual(t, name, stagingWorkspaceName(testConsumerCluster, testProviderCluster, "other-export"))
}
