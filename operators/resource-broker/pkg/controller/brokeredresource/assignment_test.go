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

package brokeredresource

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
)

func TestAssignmentGetName(t *testing.T) {
	t.Parallel()

	sub := &assignmentSubroutine{}
	assert.Equal(t, "Assignment", sub.GetName())
}

func TestAssignmentFinalizers(t *testing.T) {
	t.Parallel()

	sub := &assignmentSubroutine{}
	assert.Equal(t, []string{AssignmentFinalizer}, sub.Finalizers(nil))
}

func TestAssignmentProcess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		coordObjs     []ctrlruntimeclient.Object
		providerObjs  []ctrlruntimeclient.Object
		refs          []AcceptAPIRef
		wantOK        bool
		wantMsg       string
		wantPhase     pmcoordbrokerv1alpha1.AssignmentPhase
		wantMigration bool
	}{
		{
			name:    "creates assignment for matching accept api",
			refs:    []AcceptAPIRef{{Cluster: testProviderCluster, AcceptAPI: testAcceptAPI()}},
			wantMsg: "created assignment",
		},
		{
			name:    "no matching accept api",
			wantMsg: "no matching AcceptAPI",
		},
		{
			name: "non-matching gvr filtered out",
			refs: []AcceptAPIRef{
				{Cluster: testProviderCluster, AcceptAPI: testNonApplyingAcceptAPI()},
			},
			wantMsg: "no matching AcceptAPI",
		},
		{
			name: "terminating assignment",
			coordObjs: []ctrlruntimeclient.Object{
				func() ctrlruntimeclient.Object {
					assignment := testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound)
					assignment.Finalizers = []string{"keep/me"}
					assignment.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}
					return assignment
				}(),
			},
			wantMsg: "assignment is terminating",
		},
		{
			name: "adopts spec and creates staging workspace",
			coordObjs: []ctrlruntimeclient.Object{
				func() ctrlruntimeclient.Object {
					assignment := testAssignment(pmcoordbrokerv1alpha1.AssignmentPhasePending)
					assignment.Status = pmcoordbrokerv1alpha1.AssignmentStatus{}
					return assignment
				}(),
			},
			providerObjs: []ctrlruntimeclient.Object{testAcceptAPI()},
			wantMsg:      "created staging workspace",
			wantPhase:    pmcoordbrokerv1alpha1.AssignmentPhasePending,
		},
		{
			name: "waits for staging workspace ready",
			coordObjs: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhasePending),
				testStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhasePending),
			},
			providerObjs: []ctrlruntimeclient.Object{testAcceptAPI()},
			wantMsg:      "waiting for staging workspace to become ready",
			wantPhase:    pmcoordbrokerv1alpha1.AssignmentPhasePending,
		},
		{
			name: "staging workspace terminating",
			coordObjs: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhasePending),
				func() ctrlruntimeclient.Object {
					sw := testStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady)
					sw.Finalizers = []string{"keep/me"}
					sw.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}
					return sw
				}(),
			},
			providerObjs: []ctrlruntimeclient.Object{testAcceptAPI()},
			wantMsg:      "staging workspace is terminating",
			wantPhase:    pmcoordbrokerv1alpha1.AssignmentPhasePending,
		},
		{
			name: "bound provider still accepts",
			coordObjs: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
				testStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady),
			},
			providerObjs: []ctrlruntimeclient.Object{testAcceptAPI()},
			wantOK:       true,
			wantPhase:    pmcoordbrokerv1alpha1.AssignmentPhaseBound,
		},
		{
			name: "provider withdrew accept api creates migration",
			coordObjs: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
				testStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady),
			},
			refs:          []AcceptAPIRef{testOtherAcceptAPIRef()},
			wantMsg:       "created migration",
			wantMigration: true,
			wantPhase:     pmcoordbrokerv1alpha1.AssignmentPhaseBound,
		},
		{
			name: "accept api no longer applies creates migration",
			coordObjs: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
				testStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady),
			},
			providerObjs:  []ctrlruntimeclient.Object{testNonApplyingAcceptAPI()},
			refs:          []AcceptAPIRef{testOtherAcceptAPIRef()},
			wantMsg:       "created migration",
			wantMigration: true,
			wantPhase:     pmcoordbrokerv1alpha1.AssignmentPhaseBound,
		},
		{
			name: "no matching accept api to migrate to",
			coordObjs: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
				testStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady),
			},
			providerObjs: []ctrlruntimeclient.Object{testNonApplyingAcceptAPI()},
			wantMsg:      "no matching AcceptAPI to migrate to",
			wantPhase:    pmcoordbrokerv1alpha1.AssignmentPhaseBound,
		},
		{
			name: "excludes current accept api from migration candidates",
			coordObjs: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
				testStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady),
			},
			providerObjs: []ctrlruntimeclient.Object{testNonApplyingAcceptAPI()},
			refs:         []AcceptAPIRef{{Cluster: testProviderCluster, AcceptAPI: testAcceptAPI()}},
			wantMsg:      "no matching AcceptAPI to migrate to",
			wantPhase:    pmcoordbrokerv1alpha1.AssignmentPhaseBound,
		},
		{
			name: "waits for terminating migration",
			coordObjs: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
				func() ctrlruntimeclient.Object {
					migration := testMigration(pmcoordbrokerv1alpha1.MigrationStateCutoverCompleted)
					migration.Finalizers = []string{"keep/me"}
					migration.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}
					return migration
				}(),
			},
			wantMsg:       "migration is terminating",
			wantMigration: true,
			wantPhase:     pmcoordbrokerv1alpha1.AssignmentPhaseBound,
		},
		{
			name: "waits for migration to complete",
			coordObjs: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
				testMigration(pmcoordbrokerv1alpha1.MigrationStateInitialInProgress),
			},
			wantMsg:       "waiting for migration to complete",
			wantMigration: true,
			wantPhase:     pmcoordbrokerv1alpha1.AssignmentPhaseBound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clients := &migrationTestClients{
				coordination: testFakeClient(t, tc.coordObjs...),
				provider:     testFakeClient(t, tc.providerObjs...),
				staging:      testFakeClient(t),
				target:       testFakeClient(t),
			}
			sub := &assignmentSubroutine{opts: testMigrationOptions(t, clients, tc.refs)}
			ctx := mccontext.WithCluster(t.Context(), testConsumerCluster)

			result, err := sub.Process(ctx, testConsumerObject())
			require.NoError(t, err)

			if tc.wantOK {
				assert.True(t, result.IsContinue())
			} else {
				assert.Positive(t, result.Requeue())
				assert.Contains(t, result.Message(), tc.wantMsg)
			}

			if tc.wantPhase != "" {
				assignment := &pmcoordbrokerv1alpha1.Assignment{}
				require.NoError(t, clients.coordination.Get(ctx, types.NamespacedName{Name: testAssignmentName()}, assignment))
				assert.Equal(t, tc.wantPhase, assignment.Status.Phase)
			}

			migration := &pmcoordbrokerv1alpha1.Migration{}
			err = clients.coordination.Get(ctx, types.NamespacedName{Name: testMigrationName()}, migration)
			if tc.wantMigration {
				require.NoError(t, err)
			} else {
				require.True(t, apierrors.IsNotFound(err))
			}
		})
	}
}

func TestAssignmentProcessCreatesAssignmentSpec(t *testing.T) {
	t.Parallel()

	clients := &migrationTestClients{
		coordination: testFakeClient(t),
		provider:     testFakeClient(t),
		staging:      testFakeClient(t),
		target:       testFakeClient(t),
	}
	refs := []AcceptAPIRef{{Cluster: testProviderCluster, AcceptAPI: testAcceptAPI()}}
	sub := &assignmentSubroutine{opts: testMigrationOptions(t, clients, refs)}
	ctx := mccontext.WithCluster(t.Context(), testConsumerCluster)

	result, err := sub.Process(ctx, testConsumerObject())
	require.NoError(t, err)
	assert.Positive(t, result.Requeue())
	assert.Contains(t, result.Message(), "created assignment")

	assignment := &pmcoordbrokerv1alpha1.Assignment{}
	require.NoError(t, clients.coordination.Get(ctx, types.NamespacedName{Name: testAssignmentName()}, assignment))
	assert.Equal(t, testConsumerCluster, assignment.Spec.ConsumerCluster)
	assert.Equal(t, testGVR, assignment.Spec.GVR)
	assert.Equal(t, testNamespace, assignment.Spec.Namespace)
	assert.Equal(t, testResourceName, assignment.Spec.Name)
	assert.Equal(t, testProviderCluster, assignment.Spec.ProviderCluster)
	assert.Equal(t, testAcceptAPIName, assignment.Spec.AcceptAPIName)
}

func TestAssignmentProcessAdoptsSpecStatus(t *testing.T) {
	t.Parallel()

	assignment := testAssignment(pmcoordbrokerv1alpha1.AssignmentPhasePending)
	assignment.Status = pmcoordbrokerv1alpha1.AssignmentStatus{}

	clients := &migrationTestClients{
		coordination: testFakeClient(t, assignment),
		provider:     testFakeClient(t, testAcceptAPI()),
		staging:      testFakeClient(t),
		target:       testFakeClient(t),
	}
	sub := &assignmentSubroutine{opts: testMigrationOptions(t, clients, nil)}
	ctx := mccontext.WithCluster(t.Context(), testConsumerCluster)

	result, err := sub.Process(ctx, testConsumerObject())
	require.NoError(t, err)
	assert.Positive(t, result.Requeue())
	assert.Contains(t, result.Message(), "created staging workspace")

	// The adopted status must be persisted.
	updated := &pmcoordbrokerv1alpha1.Assignment{}
	require.NoError(t, clients.coordination.Get(ctx, types.NamespacedName{Name: testAssignmentName()}, updated))
	assert.Equal(t, testProviderCluster, updated.Status.ProviderCluster)
	assert.Equal(t, testAcceptAPIName, updated.Status.AcceptAPIName)
	assert.Equal(t, testExportName, updated.Status.APIExportName)
	assert.Equal(t, testStagingName, updated.Status.StagingWorkspace)
	assert.Equal(t, pmcoordbrokerv1alpha1.AssignmentPhasePending, updated.Status.Phase)

	// The staging workspace must exist with the resolved spec.
	sw := &pmcoordbrokerv1alpha1.StagingWorkspace{}
	require.NoError(t, clients.coordination.Get(ctx, types.NamespacedName{Name: testStagingName}, sw))
	assert.Equal(t, testConsumerCluster, sw.Spec.ConsumerCluster)
	assert.Equal(t, testProviderCluster, sw.Spec.ProviderCluster)
	assert.Equal(t, testExportName, sw.Spec.APIExportName)
}

func TestAssignmentProcessStartMigration(t *testing.T) {
	t.Parallel()

	clients := &migrationTestClients{
		coordination: testFakeClient(t,
			testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			testStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady),
		),
		provider: testFakeClient(t),
		staging:  testFakeClient(t),
		target:   testFakeClient(t),
	}
	refs := []AcceptAPIRef{testOtherAcceptAPIRef()}
	sub := &assignmentSubroutine{opts: testMigrationOptions(t, clients, refs)}
	ctx := mccontext.WithCluster(t.Context(), testConsumerCluster)

	result, err := sub.Process(ctx, testConsumerObject())
	require.NoError(t, err)
	assert.Positive(t, result.Requeue())
	assert.Contains(t, result.Message(), "created migration")

	// The assignment spec must be repointed to the new provider.
	assignment := &pmcoordbrokerv1alpha1.Assignment{}
	require.NoError(t, clients.coordination.Get(ctx, types.NamespacedName{Name: testAssignmentName()}, assignment))
	assert.Equal(t, testOtherProviderCluster, assignment.Spec.ProviderCluster)
	assert.Equal(t, testOtherAcceptAPIName, assignment.Spec.AcceptAPIName)

	// The destination staging workspace must exist.
	sw := &pmcoordbrokerv1alpha1.StagingWorkspace{}
	require.NoError(t, clients.coordination.Get(ctx, types.NamespacedName{Name: testTargetStagingName}, sw))
	assert.Equal(t, testOtherProviderCluster, sw.Spec.ProviderCluster)
	assert.Equal(t, testOtherExportName, sw.Spec.APIExportName)

	// The migration must snapshot both staging workspaces and targets.
	migration := &pmcoordbrokerv1alpha1.Migration{}
	require.NoError(t, clients.coordination.Get(ctx, types.NamespacedName{Name: testMigrationName()}, migration))
	assert.Equal(t, testAssignmentName(), migration.Spec.Assignment)
	assert.Equal(t, testNamespace, migration.Spec.Namespace)
	assert.Equal(t, testResourceName, migration.Spec.Name)
	assert.Equal(t, testStagingName, migration.Spec.FromStagingWorkspace)
	assert.Equal(t, testTargetStagingName, migration.Spec.StagingWorkspace)
	assert.Equal(t, testProviderCluster, migration.Spec.From.ProviderCluster)
	assert.Equal(t, testAcceptAPIName, migration.Spec.From.AcceptAPIName)
	assert.Equal(t, testOtherProviderCluster, migration.Spec.To.ProviderCluster)
	assert.Equal(t, testOtherAcceptAPIName, migration.Spec.To.AcceptAPIName)
	assert.Equal(t, testGVK.Kind, migration.Spec.From.GVK.Kind)
	assert.Equal(t, testGVK.Kind, migration.Spec.To.GVK.Kind)
}

func TestAssignmentProcessPick(t *testing.T) {
	t.Parallel()

	clients := &migrationTestClients{
		coordination: testFakeClient(t),
		provider:     testFakeClient(t),
		staging:      testFakeClient(t),
		target:       testFakeClient(t),
	}
	refs := []AcceptAPIRef{
		{Cluster: testProviderCluster, AcceptAPI: testAcceptAPI()},
		testOtherAcceptAPIRef(),
	}
	opts := testMigrationOptions(t, clients, refs)
	opts.PickAcceptAPI = func(refs []AcceptAPIRef) AcceptAPIRef {
		return refs[1]
	}
	sub := &assignmentSubroutine{opts: opts}
	ctx := mccontext.WithCluster(t.Context(), testConsumerCluster)

	result, err := sub.Process(ctx, testConsumerObject())
	require.NoError(t, err)
	assert.Positive(t, result.Requeue())

	assignment := &pmcoordbrokerv1alpha1.Assignment{}
	require.NoError(t, clients.coordination.Get(ctx, types.NamespacedName{Name: testAssignmentName()}, assignment))
	assert.Equal(t, testOtherProviderCluster, assignment.Spec.ProviderCluster)
	assert.Equal(t, testOtherAcceptAPIName, assignment.Spec.AcceptAPIName)
}

func TestAssignmentProcessFinishesMigration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		stagingObjs []ctrlruntimeclient.Object
		wantMsg     string
		wantDone    bool
	}{
		{
			name:        "deletes old staging copy",
			stagingObjs: []ctrlruntimeclient.Object{testStagingCopy()},
			wantMsg:     "waiting for old staging copy to be deleted",
		},
		{
			name:     "old copy gone finishes migration",
			wantMsg:  "waiting for migration to be deleted",
			wantDone: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clients := &migrationTestClients{
				coordination: testFakeClient(t,
					testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
					testMigration(pmcoordbrokerv1alpha1.MigrationStateCutoverCompleted),
					testStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady),
				),
				provider: testFakeClient(t),
				staging:  testFakeClient(t, tc.stagingObjs...),
				target:   testFakeClient(t),
			}
			sub := &assignmentSubroutine{opts: testMigrationOptions(t, clients, nil)}
			ctx := mccontext.WithCluster(t.Context(), testConsumerCluster)

			result, err := sub.Process(ctx, testConsumerObject())
			require.NoError(t, err)
			assert.Positive(t, result.Requeue())
			assert.Contains(t, result.Message(), tc.wantMsg)

			// The old staging copy must be gone in all cases.
			stagingCopy := testConsumerObject()
			err = clients.staging.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: testResourceName}, stagingCopy)
			require.True(t, apierrors.IsNotFound(err))

			// The assignment must already be repointed at the target staging
			// workspace so the copy subroutine does not recreate the old copy.
			repointed := &pmcoordbrokerv1alpha1.Assignment{}
			require.NoError(t, clients.coordination.Get(ctx, types.NamespacedName{Name: testAssignmentName()}, repointed))
			assert.Equal(t, testTargetStagingName, repointed.Status.StagingWorkspace)

			if !tc.wantDone {
				return
			}

			// The migration must be deleted.
			migration := &pmcoordbrokerv1alpha1.Migration{}
			err = clients.coordination.Get(ctx, types.NamespacedName{Name: testMigrationName()}, migration)
			require.True(t, apierrors.IsNotFound(err))

			// The assignment status must point at the new provider.
			assignment := &pmcoordbrokerv1alpha1.Assignment{}
			require.NoError(t, clients.coordination.Get(ctx, types.NamespacedName{Name: testAssignmentName()}, assignment))
			assert.Equal(t, testOtherProviderCluster, assignment.Status.ProviderCluster)
			assert.Equal(t, testOtherAcceptAPIName, assignment.Status.AcceptAPIName)
			assert.Empty(t, assignment.Status.APIExportName)
			assert.Equal(t, testTargetStagingName, assignment.Status.StagingWorkspace)
			assert.Equal(t, pmcoordbrokerv1alpha1.AssignmentPhaseBound, assignment.Status.Phase)

			// The old staging workspace must be released.
			sw := &pmcoordbrokerv1alpha1.StagingWorkspace{}
			err = clients.coordination.Get(ctx, types.NamespacedName{Name: testStagingName}, sw)
			require.True(t, apierrors.IsNotFound(err))
		})
	}
}

func TestAssignmentProcessNoClusterInContext(t *testing.T) {
	t.Parallel()

	clients := &migrationTestClients{
		coordination: testFakeClient(t),
		provider:     testFakeClient(t),
		staging:      testFakeClient(t),
		target:       testFakeClient(t),
	}
	sub := &assignmentSubroutine{opts: testMigrationOptions(t, clients, nil)}

	_, err := sub.Process(t.Context(), testConsumerObject())
	require.ErrorContains(t, err, "no cluster name in context")
}

func TestAssignmentFinalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		coordObjs          []ctrlruntimeclient.Object
		wantOK             bool
		wantMsg            string
		wantAssignmentGone bool
		wantMigrationGone  bool
		wantKeptWorkspaces []string
		wantGoneWorkspaces []string
	}{
		{
			name:   "nothing to clean up",
			wantOK: true,
		},
		{
			name: "deletes assignment and releases staging workspace",
			coordObjs: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
				testStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady),
			},
			wantMsg:            "waiting for assignment to be deleted",
			wantAssignmentGone: true,
			wantGoneWorkspaces: []string{testStagingName},
		},
		{
			name: "keeps staging workspace referenced by another assignment",
			coordObjs: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
				testStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady),
				func() ctrlruntimeclient.Object {
					other := testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound)
					other.Name = "assignment-other"
					return other
				}(),
			},
			wantMsg:            "waiting for assignment to be deleted",
			wantAssignmentGone: true,
			wantKeptWorkspaces: []string{testStagingName},
		},
		{
			name: "migration cleanup releases both staging workspaces",
			coordObjs: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
				testMigration(pmcoordbrokerv1alpha1.MigrationStatePending),
				testStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady),
				testTargetStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady),
			},
			wantMsg:            "waiting for migration to be deleted",
			wantMigrationGone:  true,
			wantGoneWorkspaces: []string{testStagingName, testTargetStagingName},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clients := &migrationTestClients{
				coordination: testFakeClient(t, tc.coordObjs...),
				provider:     testFakeClient(t),
				staging:      testFakeClient(t),
				target:       testFakeClient(t),
			}
			sub := &assignmentSubroutine{opts: testMigrationOptions(t, clients, nil)}
			ctx := mccontext.WithCluster(t.Context(), testConsumerCluster)

			result, err := sub.Finalize(ctx, testConsumerObject())
			require.NoError(t, err)

			if tc.wantOK {
				assert.True(t, result.IsContinue())
			} else {
				assert.Positive(t, result.Requeue())
				assert.Contains(t, result.Message(), tc.wantMsg)
			}

			if tc.wantAssignmentGone {
				assignment := &pmcoordbrokerv1alpha1.Assignment{}
				err := clients.coordination.Get(ctx, types.NamespacedName{Name: testAssignmentName()}, assignment)
				require.True(t, apierrors.IsNotFound(err))
			}

			if tc.wantMigrationGone {
				migration := &pmcoordbrokerv1alpha1.Migration{}
				err := clients.coordination.Get(ctx, types.NamespacedName{Name: testMigrationName()}, migration)
				require.True(t, apierrors.IsNotFound(err))
			}

			for _, wsName := range tc.wantKeptWorkspaces {
				sw := &pmcoordbrokerv1alpha1.StagingWorkspace{}
				require.NoError(t, clients.coordination.Get(ctx, types.NamespacedName{Name: wsName}, sw))
			}

			for _, wsName := range tc.wantGoneWorkspaces {
				sw := &pmcoordbrokerv1alpha1.StagingWorkspace{}
				err := clients.coordination.Get(ctx, types.NamespacedName{Name: wsName}, sw)
				require.True(t, apierrors.IsNotFound(err))
			}
		})
	}
}

func TestAssignmentName(t *testing.T) {
	t.Parallel()

	name := assignmentName(testConsumerCluster, testGVR, testNamespace, testResourceName)
	assert.Regexp(t, regexp.MustCompile(`^assignment-[0-9a-f]{16}$`), name)

	require.Equal(t, name, assignmentName(testConsumerCluster, testGVR, testNamespace, testResourceName))
}
