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
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmbrokerv1alpha1 "go.platform-mesh.io/apis/broker/v1alpha1"
	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	testOtherProviderCluster = "other-provider-cluster"
	testOtherAcceptAPIName   = "accept-widgets-2"
	testTargetStagingName    = "staging-target"
)

// migrationTestClients bundles the fake clients backing testMigrationOptions.
type migrationTestClients struct {
	coordination ctrlruntimeclient.Client
	provider     ctrlruntimeclient.Client
	staging      ctrlruntimeclient.Client
	target       ctrlruntimeclient.Client
}

// testMigrationOptions dispatches the workspace client func by path across
// the assigned provider, the assigned staging workspace and the migration
// target staging workspace.
func testMigrationOptions(t *testing.T, clients migrationTestClients, refs []AcceptAPIRef) Options {
	t.Helper()
	return Options{
		GVK:                testGVK,
		GVR:                testGVR,
		StagingTreeRoot:    testTreeRoot,
		CoordinationClient: clients.coordination,
		RequeueInterval:    DefaultRequeueInterval,
		WorkspaceClientFunc: func(path string) (ctrlruntimeclient.Client, error) {
			switch path {
			case testProviderCluster:
				return clients.provider, nil
			case testTreeRoot + ":" + testStagingName:
				return clients.staging, nil
			case testTreeRoot + ":" + testTargetStagingName:
				return clients.target, nil
			}
			t.Fatalf("unexpected workspace client path %q", path)
			return nil, nil
		},
		ListAcceptAPIs: func(_ context.Context) ([]AcceptAPIRef, error) {
			return refs, nil
		},
		PickAcceptAPI: func(refs []AcceptAPIRef) AcceptAPIRef {
			return refs[0]
		},
	}
}

func testMigrationName() string {
	return migrationName(testConsumerCluster, testGVR, testNamespace, testResourceName)
}

func testMigration(state pmcoordbrokerv1alpha1.MigrationState) *pmcoordbrokerv1alpha1.Migration {
	gvk := metav1.GroupVersionKind{Group: testGVK.Group, Version: testGVK.Version, Kind: testGVK.Kind}
	return &pmcoordbrokerv1alpha1.Migration{
		ObjectMeta: metav1.ObjectMeta{Name: testMigrationName()},
		Spec: pmcoordbrokerv1alpha1.MigrationSpec{
			Assignment: testAssignmentName(),
			From: pmcoordbrokerv1alpha1.MigrationTarget{
				GVK:             gvk,
				ProviderCluster: testProviderCluster,
				AcceptAPIName:   testAcceptAPIName,
			},
			To: pmcoordbrokerv1alpha1.MigrationTarget{
				GVK:             gvk,
				ProviderCluster: testOtherProviderCluster,
				AcceptAPIName:   testOtherAcceptAPIName,
			},
		},
		Status: pmcoordbrokerv1alpha1.MigrationStatus{
			State:                state,
			FromStagingWorkspace: testStagingName,
			StagingWorkspace:     testTargetStagingName,
		},
	}
}

// testOtherAcceptAPIRef points at an alternative provider accepting the
// consumer object.
func testOtherAcceptAPIRef() AcceptAPIRef {
	return AcceptAPIRef{
		Cluster: testOtherProviderCluster,
		AcceptAPI: &pmbrokerv1alpha1.AcceptAPI{
			ObjectMeta: metav1.ObjectMeta{Name: testOtherAcceptAPIName},
			Spec: pmbrokerv1alpha1.AcceptAPISpec{
				GVR:           testGVR,
				APIExportName: "other-export",
			},
		},
	}
}

// testNonApplyingAcceptAPI returns the assigned AcceptAPI with a GVR that no
// longer matches the consumer object.
func testNonApplyingAcceptAPI() *pmbrokerv1alpha1.AcceptAPI {
	api := testAcceptAPI()
	api.Spec.GVR = metav1.GroupVersionResource{Group: "example.io", Version: "v1", Resource: "gadgets"}
	return api
}

func TestMigrationGetName(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Migration", (&migrationSubroutine{}).GetName())
}

func TestMigrationFinalizers(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{MigrationFinalizer}, (&migrationSubroutine{}).Finalizers(nil))
}

func TestMigrationProcess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		coordObjs     []ctrlruntimeclient.Object
		providerObjs  []ctrlruntimeclient.Object
		refs          []AcceptAPIRef
		wantOK        bool
		wantMsg       string
		wantMigration bool
	}{
		{
			name:   "no assignment",
			wantOK: true,
		},
		{
			name: "assignment not bound",
			coordObjs: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhasePending),
			},
			wantOK: true,
		},
		{
			name: "provider still accepts",
			coordObjs: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			},
			providerObjs: []ctrlruntimeclient.Object{testAcceptAPI()},
			wantOK:       true,
		},
		{
			name: "provider withdrew acceptapi migrates",
			coordObjs: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			},
			refs:          []AcceptAPIRef{testOtherAcceptAPIRef()},
			wantMsg:       "created migration",
			wantMigration: true,
		},
		{
			name: "acceptapi no longer applies migrates",
			coordObjs: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			},
			providerObjs:  []ctrlruntimeclient.Object{testNonApplyingAcceptAPI()},
			refs:          []AcceptAPIRef{testOtherAcceptAPIRef()},
			wantMsg:       "created migration",
			wantMigration: true,
		},
		{
			name: "no matching acceptapi to migrate to",
			coordObjs: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			},
			wantMsg: "no matching AcceptAPI to migrate to",
		},
		{
			name: "excludes current acceptapi from candidates",
			coordObjs: []ctrlruntimeclient.Object{
				testAssignment(pmcoordbrokerv1alpha1.AssignmentPhaseBound),
			},
			providerObjs: []ctrlruntimeclient.Object{testNonApplyingAcceptAPI()},
			refs: []AcceptAPIRef{
				{Cluster: testProviderCluster, AcceptAPI: testAcceptAPI()},
			},
			wantMsg: "no matching AcceptAPI to migrate to",
		},
		{
			name: "waits for terminating migration",
			coordObjs: []ctrlruntimeclient.Object{
				func() ctrlruntimeclient.Object {
					migration := testMigration(pmcoordbrokerv1alpha1.MigrationStatePending)
					migration.SetDeletionTimestamp(&metav1.Time{Time: time.Now()})
					migration.SetFinalizers([]string{"keep/me"})
					return migration
				}(),
			},
			wantMsg: "migration is terminating",
		},
		{
			name: "waits for migration to complete",
			coordObjs: []ctrlruntimeclient.Object{
				testMigration(pmcoordbrokerv1alpha1.MigrationStateInitialInProgress),
			},
			wantMsg: "waiting for migration to complete",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clients := migrationTestClients{
				coordination: testFakeClient(t, tc.coordObjs...),
				provider:     testFakeClient(t, tc.providerObjs...),
				staging:      testFakeClient(t),
				target:       testFakeClient(t),
			}
			s := &migrationSubroutine{opts: testMigrationOptions(t, clients, tc.refs)}

			ctx := testCopyContext(t, testFakeClient(t))
			result, err := s.Process(ctx, testConsumerObject())
			require.NoError(t, err)

			if tc.wantOK {
				assert.True(t, result.IsContinue())
				assert.Zero(t, result.Requeue())
			} else {
				assert.Positive(t, result.Requeue())
				assert.Equal(t, tc.wantMsg, result.Message())
			}

			migration := &pmcoordbrokerv1alpha1.Migration{}
			err = clients.coordination.Get(ctx, ctrlruntimeclient.ObjectKey{Name: testMigrationName()}, migration)
			if !tc.wantMigration {
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testAssignmentName(), migration.Spec.Assignment)
			assert.Equal(t, testProviderCluster, migration.Spec.From.ProviderCluster)
			assert.Equal(t, testAcceptAPIName, migration.Spec.From.AcceptAPIName)
			assert.Equal(t, testOtherProviderCluster, migration.Spec.To.ProviderCluster)
			assert.Equal(t, testOtherAcceptAPIName, migration.Spec.To.AcceptAPIName)
			assert.Equal(t, testGVK.Kind, migration.Spec.From.GVK.Kind)
			assert.Equal(t, testGVK.Kind, migration.Spec.To.GVK.Kind)
		})
	}
}

func TestMigrationProcessFinishesMigration(t *testing.T) {
	t.Parallel()

	nn := types.NamespacedName{Namespace: testNamespace, Name: testResourceName}

	tests := []struct {
		name         string
		stagingObjs  []ctrlruntimeclient.Object
		wantMsg      string
		wantCopyGone bool
		wantMigGone  bool
	}{
		{
			name:         "deletes old staging copy",
			stagingObjs:  []ctrlruntimeclient.Object{testStagingCopy()},
			wantMsg:      "waiting for old staging copy to be deleted",
			wantCopyGone: true,
		},
		{
			name:        "old copy gone deletes migration",
			wantMsg:     "waiting for migration to be deleted",
			wantMigGone: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clients := migrationTestClients{
				coordination: testFakeClient(t, testMigration(pmcoordbrokerv1alpha1.MigrationStateCutoverCompleted)),
				provider:     testFakeClient(t),
				staging:      testFakeClient(t, tc.stagingObjs...),
				target:       testFakeClient(t),
			}
			s := &migrationSubroutine{opts: testMigrationOptions(t, clients, nil)}

			ctx := testCopyContext(t, testFakeClient(t))
			result, err := s.Process(ctx, testConsumerObject())
			require.NoError(t, err)
			assert.Positive(t, result.Requeue())
			assert.Equal(t, tc.wantMsg, result.Message())

			if tc.wantCopyGone {
				stagingCopy := &unstructured.Unstructured{}
				stagingCopy.SetGroupVersionKind(testGVK)
				err := clients.staging.Get(ctx, nn, stagingCopy)
				assert.True(t, err != nil)
			}

			migration := &pmcoordbrokerv1alpha1.Migration{}
			err = clients.coordination.Get(ctx, ctrlruntimeclient.ObjectKey{Name: testMigrationName()}, migration)
			if tc.wantMigGone {
				assert.True(t, err != nil)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMigrationProcessNoClusterInContext(t *testing.T) {
	t.Parallel()

	s := &migrationSubroutine{}
	_, err := s.Process(t.Context(), testConsumerObject())
	require.ErrorContains(t, err, "no cluster name in context")
}

func TestMigrationFinalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		coordObjs []ctrlruntimeclient.Object
		wantOK    bool
		wantGone  bool
	}{
		{
			name:     "no migration",
			wantOK:   true,
			wantGone: true,
		},
		{
			name: "deletes migration",
			coordObjs: []ctrlruntimeclient.Object{
				testMigration(pmcoordbrokerv1alpha1.MigrationStatePending),
			},
			wantGone: true,
		},
		{
			name: "waits for terminating migration",
			coordObjs: []ctrlruntimeclient.Object{
				func() ctrlruntimeclient.Object {
					migration := testMigration(pmcoordbrokerv1alpha1.MigrationStatePending)
					migration.SetDeletionTimestamp(&metav1.Time{Time: time.Now()})
					migration.SetFinalizers([]string{"keep/me"})
					return migration
				}(),
			},
			wantGone: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clients := migrationTestClients{
				coordination: testFakeClient(t, tc.coordObjs...),
				provider:     testFakeClient(t),
				staging:      testFakeClient(t),
				target:       testFakeClient(t),
			}
			s := &migrationSubroutine{opts: testMigrationOptions(t, clients, nil)}

			ctx := testCopyContext(t, testFakeClient(t))
			result, err := s.Finalize(ctx, testConsumerObject())
			require.NoError(t, err)

			if tc.wantOK {
				assert.True(t, result.IsContinue())
				assert.Zero(t, result.Requeue())
			} else {
				assert.Positive(t, result.Requeue())
			}

			migration := &pmcoordbrokerv1alpha1.Migration{}
			err = clients.coordination.Get(ctx, ctrlruntimeclient.ObjectKey{Name: testMigrationName()}, migration)
			if tc.wantGone {
				assert.True(t, err != nil)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMigrationName(t *testing.T) {
	t.Parallel()

	name := migrationName(testConsumerCluster, testGVR, testNamespace, testResourceName)
	assert.Regexp(t, regexp.MustCompile(`^migration-[0-9a-f]{16}$`), name)
	assert.Equal(t, name, migrationName(testConsumerCluster, testGVR, testNamespace, testResourceName))
}
