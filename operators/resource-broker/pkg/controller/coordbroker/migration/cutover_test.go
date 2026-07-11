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

package migration

import (
	"testing"

	"github.com/stretchr/testify/require"

	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"
	"go.platform-mesh.io/subroutines"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCutoverGetName(t *testing.T) {
	t.Parallel()

	sub := &cutoverSubroutine{}
	require.Equal(t, pmcoordbrokerv1alpha1.MigrationConditionCutoverCompleted, sub.GetName())
}

func TestCutoverProcess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		migration     *pmcoordbrokerv1alpha1.Migration
		toStagingObjs []ctrlruntimeclient.Object
		wantOK        bool
		wantPending   string
		wantState     pmcoordbrokerv1alpha1.MigrationState
	}{
		{
			name:      "cutover already completed",
			migration: testMigration(pmcoordbrokerv1alpha1.MigrationStateCutoverCompleted),
			wantOK:    true,
			wantState: pmcoordbrokerv1alpha1.MigrationStateCutoverCompleted,
		},
		{
			name:        "waits for stages to complete",
			migration:   testMigration(pmcoordbrokerv1alpha1.MigrationStatePending),
			wantPending: "waiting for stages to complete",
			wantState:   pmcoordbrokerv1alpha1.MigrationStatePending,
		},
		{
			name:        "waits for staging copy in target workspace",
			migration:   testMigration(pmcoordbrokerv1alpha1.MigrationStateCutoverInProgress),
			wantPending: "waiting for staging copy in target workspace",
			wantState:   pmcoordbrokerv1alpha1.MigrationStateCutoverInProgress,
		},
		{
			name:          "waits for staging copy to become available",
			migration:     testMigration(pmcoordbrokerv1alpha1.MigrationStateCutoverInProgress),
			toStagingObjs: []ctrlruntimeclient.Object{testStagingWidget("Provisioning")},
			wantPending:   "waiting for staging copy to become available",
			wantState:     pmcoordbrokerv1alpha1.MigrationStateCutoverInProgress,
		},
		{
			name:          "available staging copy completes cutover",
			migration:     testMigration(pmcoordbrokerv1alpha1.MigrationStateCutoverInProgress),
			toStagingObjs: []ctrlruntimeclient.Object{testStagingWidget("Available")},
			wantOK:        true,
			wantState:     pmcoordbrokerv1alpha1.MigrationStateCutoverCompleted,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clients := &testClients{
				coordination: testFakeClient(t),
				provider:     testFakeClient(t),
				fromStaging:  testFakeClient(t),
				toStaging:    testFakeClient(t, tc.toStagingObjs...),
				compute:      testFakeClient(t),
			}
			sub := &cutoverSubroutine{opts: testOptions(t, clients)}
			ctx := subroutines.WithClient(t.Context(), clients.coordination)

			result, err := sub.Process(ctx, tc.migration)
			require.NoError(t, err)

			if tc.wantPending != "" {
				require.Positive(t, result.Requeue())
				require.Contains(t, result.Message(), tc.wantPending)
			}
			if tc.wantOK {
				require.True(t, result.IsContinue())
			}
			require.Equal(t, tc.wantState, tc.migration.Status.State)
		})
	}
}

func TestCutoverProcessWrongType(t *testing.T) {
	t.Parallel()

	sub := &cutoverSubroutine{}
	_, err := sub.Process(t.Context(), &pmcoordbrokerv1alpha1.Assignment{})
	require.ErrorContains(t, err, "expected Migration")
}
