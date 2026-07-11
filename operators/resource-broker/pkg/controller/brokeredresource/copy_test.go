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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
)

func TestCopyGetName(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Copy", (&copySubroutine{}).GetName())
}

func TestCopyFinalizers(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{CopyFinalizer}, (&copySubroutine{}).Finalizers(nil))
}

// testCopyContext builds a subroutine context with cluster name and consumer client.
func testCopyContext(t *testing.T, consumerClient ctrlruntimeclient.Client) context.Context {
	t.Helper()
	ctx := mccontext.WithCluster(t.Context(), testConsumerCluster)
	return subroutines.WithClient(ctx, consumerClient)
}

func testStagingCopy() *unstructured.Unstructured {
	obj := testConsumerObject()
	obj.SetAnnotations(map[string]string{
		ConsumerClusterAnnotation: testConsumerCluster,
		ConsumerNameAnnotation:    testResourceName,
	})
	return obj
}

func TestCopyProcess(t *testing.T) {
	t.Parallel()

	nn := types.NamespacedName{Namespace: testNamespace, Name: testResourceName}

	tests := []struct {
		name        string
		assignments []ctrlruntimeclient.Object
		stagingObjs []ctrlruntimeclient.Object
		wantMsg     string
		wantCopied  bool
	}{
		{
			name:    "waits for assignment",
			wantMsg: "waiting for assignment",
		},
		{
			name: "waits for assignment to be bound",
			assignments: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhasePending),
			},
			wantMsg: "waiting for assignment to be bound",
		},
		{
			name: "copies resource into staging workspace",
			assignments: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			},
			wantCopied: true,
		},
		{
			name: "updates existing staging copy",
			assignments: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			},
			stagingObjs: []ctrlruntimeclient.Object{
				func() ctrlruntimeclient.Object {
					obj := testStagingCopy()
					require.NoError(t, unstructured.SetNestedField(obj.Object, int64(1), "spec", "size"))
					return obj
				}(),
			},
			wantCopied: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			consumer := testFakeClient(t, testConsumerObject())
			clients := testClients{
				coordination: testFakeClient(t, tc.assignments...),
				staging:      testFakeClient(t, tc.stagingObjs...),
			}
			opts := testOptions(t, clients, nil)
			opts.RequeueInterval = DefaultRequeueInterval
			s := &copySubroutine{opts: opts}

			ctx := testCopyContext(t, consumer)
			result, err := s.Process(ctx, testConsumerObject())
			require.NoError(t, err)
			assert.Positive(t, result.Requeue())
			if tc.wantMsg != "" {
				assert.Equal(t, tc.wantMsg, result.Message())
			} else {
				assert.True(t, result.IsContinue())
			}

			stagingCopy := &unstructured.Unstructured{}
			stagingCopy.SetGroupVersionKind(testGVK)
			err = clients.staging.Get(ctx, nn, stagingCopy)
			if !tc.wantCopied {
				assert.True(t, err != nil)
				return
			}
			require.NoError(t, err)

			size, _, err := unstructured.NestedInt64(stagingCopy.Object, "spec", "size")
			require.NoError(t, err)
			assert.Equal(t, int64(3), size)

			anns := stagingCopy.GetAnnotations()
			assert.Equal(t, testConsumerCluster, anns[ConsumerClusterAnnotation])
			assert.Equal(t, testResourceName, anns[ConsumerNameAnnotation])

			ns := &corev1.Namespace{}
			require.NoError(t, clients.staging.Get(ctx, types.NamespacedName{Name: testNamespace}, ns))
		})
	}
}

func TestCopyProcessStatusCopyBack(t *testing.T) {
	t.Parallel()

	staged := testStagingCopy()
	require.NoError(t, unstructured.SetNestedField(staged.Object, "Ready", "status", "state"))

	consumer := testFakeClient(t, testConsumerObject())
	clients := testClients{
		coordination: testFakeClient(t, testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound)),
		staging:      testFakeClient(t, staged),
	}
	opts := testOptions(t, clients, nil)
	opts.RequeueInterval = DefaultRequeueInterval
	s := &copySubroutine{opts: opts}

	ctx := testCopyContext(t, consumer)
	_, err := s.Process(ctx, testConsumerObject())
	require.NoError(t, err)

	consumerObj := &unstructured.Unstructured{}
	consumerObj.SetGroupVersionKind(testGVK)
	nn := types.NamespacedName{Namespace: testNamespace, Name: testResourceName}
	require.NoError(t, consumer.Get(ctx, nn, consumerObj))

	state, _, err := unstructured.NestedString(consumerObj.Object, "status", "state")
	require.NoError(t, err)
	assert.Equal(t, "Ready", state)
}

func TestCopyProcessCopiesToMigrationTarget(t *testing.T) {
	t.Parallel()

	nn := types.NamespacedName{Namespace: testNamespace, Name: testResourceName}

	frozen := testStagingCopy()
	require.NoError(t, unstructured.SetNestedField(frozen.Object, int64(1), "spec", "size"))

	consumer := testFakeClient(t, testConsumerObject())
	clients := &migrationTestClients{
		coordination: testFakeClient(t,
			testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			testMigration(pmcoordbrokerv1alpha1.MigrationStateInitialInProgress),
			testTargetStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady),
		),
		provider: testFakeClient(t),
		staging:  testFakeClient(t, frozen),
		target:   testFakeClient(t),
	}
	s := &copySubroutine{opts: testMigrationOptions(t, clients, nil)}

	ctx := testCopyContext(t, consumer)
	result, err := s.Process(ctx, testConsumerObject())
	require.NoError(t, err)
	assert.True(t, result.IsContinue())

	targetCopy := &unstructured.Unstructured{}
	targetCopy.SetGroupVersionKind(testGVK)
	require.NoError(t, clients.target.Get(ctx, nn, targetCopy))

	size, _, err := unstructured.NestedInt64(targetCopy.Object, "spec", "size")
	require.NoError(t, err)
	assert.Equal(t, int64(3), size)

	anns := targetCopy.GetAnnotations()
	assert.Equal(t, testConsumerCluster, anns[ConsumerClusterAnnotation])
	assert.Equal(t, testResourceName, anns[ConsumerNameAnnotation])

	ns := &corev1.Namespace{}
	require.NoError(t, clients.target.Get(ctx, types.NamespacedName{Name: testNamespace}, ns))

	// The assigned staging workspace is frozen during the migration; the
	// consumer update must not reach the old copy.
	oldCopy := &unstructured.Unstructured{}
	oldCopy.SetGroupVersionKind(testGVK)
	require.NoError(t, clients.staging.Get(ctx, nn, oldCopy))
	size, _, err = unstructured.NestedInt64(oldCopy.Object, "spec", "size")
	require.NoError(t, err)
	assert.Equal(t, int64(1), size)
}

func TestCopyProcessMigrationWithoutTargetFreezes(t *testing.T) {
	t.Parallel()

	nn := types.NamespacedName{Namespace: testNamespace, Name: testResourceName}

	// The migration's target staging workspace does not exist yet in the
	// coordination cluster; the copy must not flow anywhere.
	migration := testMigration(pmcoordbrokerv1alpha1.MigrationStatePending)

	consumer := testFakeClient(t, testConsumerObject())
	clients := &migrationTestClients{
		coordination: testFakeClient(t,
			testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			migration,
		),
		provider: testFakeClient(t),
		staging:  testFakeClient(t),
		target:   testFakeClient(t),
	}
	s := &copySubroutine{opts: testMigrationOptions(t, clients, nil)}

	ctx := testCopyContext(t, consumer)
	result, err := s.Process(ctx, testConsumerObject())
	require.NoError(t, err)
	assert.Positive(t, result.Requeue())
	assert.Contains(t, result.Message(), "waiting for migration target staging workspace")

	// Neither staging workspace received a copy.
	for name, cl := range map[string]ctrlruntimeclient.Client{
		"staging": clients.staging,
		"target":  clients.target,
	} {
		stagingCopy := &unstructured.Unstructured{}
		stagingCopy.SetGroupVersionKind(testGVK)
		require.Error(t, cl.Get(ctx, nn, stagingCopy), name)
	}
}

func TestCopyProcessCutoverCompletedResumesNormalPath(t *testing.T) {
	t.Parallel()

	nn := types.NamespacedName{Namespace: testNamespace, Name: testResourceName}

	consumer := testFakeClient(t, testConsumerObject())
	clients := &migrationTestClients{
		coordination: testFakeClient(t,
			testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			testMigration(pmcoordbrokerv1alpha1.MigrationStateCutoverCompleted),
		),
		provider: testFakeClient(t),
		staging:  testFakeClient(t),
		target:   testFakeClient(t),
	}
	s := &copySubroutine{opts: testMigrationOptions(t, clients, nil)}

	ctx := testCopyContext(t, consumer)
	result, err := s.Process(ctx, testConsumerObject())
	require.NoError(t, err)
	assert.True(t, result.IsContinue())

	// A completed migration no longer freezes the assigned staging
	// workspace; the copy flows there again.
	stagingCopy := &unstructured.Unstructured{}
	stagingCopy.SetGroupVersionKind(testGVK)
	require.NoError(t, clients.staging.Get(ctx, nn, stagingCopy))
}

func TestCopyProcessMigrationTargetUpdatesDrift(t *testing.T) {
	t.Parallel()

	nn := types.NamespacedName{Namespace: testNamespace, Name: testResourceName}

	drifted := testStagingCopy()
	require.NoError(t, unstructured.SetNestedField(drifted.Object, int64(1), "spec", "size"))

	consumer := testFakeClient(t, testConsumerObject())
	clients := &migrationTestClients{
		coordination: testFakeClient(t,
			testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			testMigration(pmcoordbrokerv1alpha1.MigrationStateInitialInProgress),
			testTargetStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady),
		),
		provider: testFakeClient(t),
		staging:  testFakeClient(t),
		target:   testFakeClient(t, drifted),
	}
	s := &copySubroutine{opts: testMigrationOptions(t, clients, nil)}

	ctx := testCopyContext(t, consumer)
	_, err := s.Process(ctx, testConsumerObject())
	require.NoError(t, err)

	targetCopy := &unstructured.Unstructured{}
	targetCopy.SetGroupVersionKind(testGVK)
	require.NoError(t, clients.target.Get(ctx, nn, targetCopy))

	size, _, err := unstructured.NestedInt64(targetCopy.Object, "spec", "size")
	require.NoError(t, err)
	assert.Equal(t, int64(3), size)
}

func TestCopyProcessMigrationTargetNoStatusSync(t *testing.T) {
	t.Parallel()

	nn := types.NamespacedName{Namespace: testNamespace, Name: testResourceName}

	targetCopy := testStagingCopy()
	require.NoError(t, unstructured.SetNestedField(targetCopy.Object, "Ready", "status", "state"))

	consumer := testFakeClient(t, testConsumerObject())
	clients := &migrationTestClients{
		coordination: testFakeClient(t,
			testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			testMigration(pmcoordbrokerv1alpha1.MigrationStateInitialInProgress),
			testTargetStagingWorkspace(pmcoordbrokerv1alpha1.StagingWorkspacePhaseReady),
		),
		provider: testFakeClient(t),
		staging:  testFakeClient(t),
		target:   testFakeClient(t, targetCopy),
	}
	s := &copySubroutine{opts: testMigrationOptions(t, clients, nil)}

	ctx := testCopyContext(t, consumer)
	_, err := s.Process(ctx, testConsumerObject())
	require.NoError(t, err)

	// The target status must not flow back to the consumer; status keeps
	// coming from the assigned staging workspace until cutover.
	consumerObj := &unstructured.Unstructured{}
	consumerObj.SetGroupVersionKind(testGVK)
	require.NoError(t, consumer.Get(ctx, nn, consumerObj))
	_, found, err := unstructured.NestedString(consumerObj.Object, "status", "state")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestCopyProcessNoClusterInContext(t *testing.T) {
	t.Parallel()

	s := &copySubroutine{}
	_, err := s.Process(t.Context(), testConsumerObject())
	require.ErrorContains(t, err, "no cluster name in context")
}

func TestCopyFinalize(t *testing.T) {
	t.Parallel()

	nn := types.NamespacedName{Namespace: testNamespace, Name: testResourceName}

	tests := []struct {
		name        string
		assignments []ctrlruntimeclient.Object
		stagingObjs []ctrlruntimeclient.Object
		wantOK      bool
		wantGone    bool
	}{
		{
			name:     "no assignment",
			wantOK:   true,
			wantGone: true,
		},
		{
			name: "staging copy gone",
			assignments: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			},
			wantOK:   true,
			wantGone: true,
		},
		{
			name: "deletes staging copy",
			assignments: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			},
			stagingObjs: []ctrlruntimeclient.Object{testStagingCopy()},
			wantGone:    true,
		},
		{
			name: "waits for terminating staging copy",
			assignments: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			},
			stagingObjs: []ctrlruntimeclient.Object{
				func() ctrlruntimeclient.Object {
					obj := testStagingCopy()
					obj.SetDeletionTimestamp(&metav1.Time{Time: time.Now()})
					obj.SetFinalizers([]string{"keep/me"})
					return obj
				}(),
			},
			wantGone: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			consumer := testFakeClient(t)
			clients := testClients{
				coordination: testFakeClient(t, tc.assignments...),
				staging:      testFakeClient(t, tc.stagingObjs...),
			}
			opts := testOptions(t, clients, nil)
			opts.RequeueInterval = DefaultRequeueInterval
			s := &copySubroutine{opts: opts}

			ctx := testCopyContext(t, consumer)
			result, err := s.Finalize(ctx, testConsumerObject())
			require.NoError(t, err)

			if tc.wantOK {
				assert.True(t, result.IsContinue())
				assert.Zero(t, result.Requeue())
			} else {
				assert.False(t, result.IsContinue())
				assert.Positive(t, result.Requeue())
			}

			stagingCopy := &unstructured.Unstructured{}
			stagingCopy.SetGroupVersionKind(testGVK)
			err = clients.staging.Get(ctx, nn, stagingCopy)
			if tc.wantGone {
				assert.True(t, err != nil)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
