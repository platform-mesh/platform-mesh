/*
Copyright 2025.

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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcoordbrokerv1alpha1 "go.platform-mesh.io/apis/coordbroker/v1alpha1"
	"go.platform-mesh.io/subroutines"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// testTemplate returns a ConfigMap template for a migration stage.
func testTemplate(t *testing.T) runtime.RawExtension {
	t.Helper()

	return runtime.RawExtension{
		Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"stage-cm"},"data":{"key":"value"}}`),
	}
}

// testConfiguration returns a MigrationConfiguration for the widget GVK with
// the given stages.
func testConfiguration(stages ...pmcoordbrokerv1alpha1.MigrationStage) *pmcoordbrokerv1alpha1.MigrationConfiguration {
	return &pmcoordbrokerv1alpha1.MigrationConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "migrate-widgets",
		},
		Spec: pmcoordbrokerv1alpha1.MigrationConfigurationSpec{
			From:   testGVK,
			To:     testGVK,
			Stages: stages,
		},
	}
}

func TestStagesGetName(t *testing.T) {
	t.Parallel()

	sub := &stagesSubroutine{}
	assert.Equal(t, pmcoordbrokerv1alpha1.MigrationConditionStagesCompleted, sub.GetName())
}

func TestStagesFinalizers(t *testing.T) {
	t.Parallel()

	sub := &stagesSubroutine{}
	assert.Equal(t, []string{StagesFinalizer}, sub.Finalizers(nil))
}

func TestStagesProcess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		migration       *pmcoordbrokerv1alpha1.Migration
		coordObjs       []ctrlruntimeclient.Object
		fromStagingObjs []ctrlruntimeclient.Object
		toStagingObjs   []ctrlruntimeclient.Object
		wantErr         string
		wantPending     bool
		wantMessage     string
		wantState       pmcoordbrokerv1alpha1.MigrationState
		wantStage       string
		wantComputeCM   bool
	}{
		{
			name:      "skips when cutover in progress",
			migration: testMigration(pmcoordbrokerv1alpha1.MigrationStateCutoverInProgress),
			wantState: pmcoordbrokerv1alpha1.MigrationStateCutoverInProgress,
		},
		{
			name:      "skips when cutover completed",
			migration: testMigration(pmcoordbrokerv1alpha1.MigrationStateCutoverCompleted),
			wantState: pmcoordbrokerv1alpha1.MigrationStateCutoverCompleted,
		},
		{
			name:        "unknown state initializes to pending",
			migration:   testMigration(pmcoordbrokerv1alpha1.MigrationStateUnknown),
			wantPending: true,
			wantMessage: "waiting for staging copy in workspace",
			wantState:   pmcoordbrokerv1alpha1.MigrationStatePending,
		},
		{
			name:            "waits for staging copy in target workspace",
			migration:       testMigration(pmcoordbrokerv1alpha1.MigrationStatePending),
			fromStagingObjs: []ctrlruntimeclient.Object{testStagingWidget("Available")},
			wantPending:     true,
			wantMessage:     "waiting for staging copy in workspace",
			wantState:       pmcoordbrokerv1alpha1.MigrationStatePending,
		},
		{
			name:            "no configuration moves to cutover",
			migration:       testMigration(pmcoordbrokerv1alpha1.MigrationStatePending),
			fromStagingObjs: []ctrlruntimeclient.Object{testStagingWidget("Available")},
			toStagingObjs:   []ctrlruntimeclient.Object{testStagingWidget("")},
			wantState:       pmcoordbrokerv1alpha1.MigrationStateCutoverInProgress,
		},
		{
			name:      "zero stage configuration moves to cutover",
			migration: testMigration(pmcoordbrokerv1alpha1.MigrationStatePending),
			coordObjs: []ctrlruntimeclient.Object{
				testConfiguration(),
			},
			fromStagingObjs: []ctrlruntimeclient.Object{testStagingWidget("Available")},
			toStagingObjs:   []ctrlruntimeclient.Object{testStagingWidget("")},
			wantState:       pmcoordbrokerv1alpha1.MigrationStateCutoverInProgress,
		},
		{
			name: "unknown stage errors",
			migration: func() *pmcoordbrokerv1alpha1.Migration {
				m := testMigration(pmcoordbrokerv1alpha1.MigrationStateInitialInProgress)
				m.Status.Stage = "no-such-stage"
				return m
			}(),
			coordObjs: []ctrlruntimeclient.Object{
				testConfiguration(pmcoordbrokerv1alpha1.MigrationStage{Name: "copy"}),
			},
			fromStagingObjs: []ctrlruntimeclient.Object{testStagingWidget("Available")},
			toStagingObjs:   []ctrlruntimeclient.Object{testStagingWidget("")},
			wantErr:         `stage "no-such-stage" not found`,
		},
		{
			name:      "stage with failing success condition waits",
			migration: testMigration(pmcoordbrokerv1alpha1.MigrationStatePending),
			coordObjs: []ctrlruntimeclient.Object{
				testConfiguration(pmcoordbrokerv1alpha1.MigrationStage{
					Name:              "copy",
					Templates:         map[string]runtime.RawExtension{"cm": testTemplate(t)},
					SuccessConditions: []string{`cm.data.key == "other"`},
				}),
			},
			fromStagingObjs: []ctrlruntimeclient.Object{testStagingWidget("Available")},
			toStagingObjs:   []ctrlruntimeclient.Object{testStagingWidget("")},
			wantPending:     true,
			wantMessage:     `waiting for stage "copy" success conditions`,
			wantState:       pmcoordbrokerv1alpha1.MigrationStateInitialInProgress,
			wantComputeCM:   true,
		},
		{
			name:      "single stage without success conditions completes",
			migration: testMigration(pmcoordbrokerv1alpha1.MigrationStatePending),
			coordObjs: []ctrlruntimeclient.Object{
				testConfiguration(pmcoordbrokerv1alpha1.MigrationStage{Name: "copy"}),
			},
			fromStagingObjs: []ctrlruntimeclient.Object{testStagingWidget("Available")},
			toStagingObjs:   []ctrlruntimeclient.Object{testStagingWidget("")},
			wantState:       pmcoordbrokerv1alpha1.MigrationStateCutoverInProgress,
		},
		{
			name:      "multi stage with progress advances",
			migration: testMigration(pmcoordbrokerv1alpha1.MigrationStatePending),
			coordObjs: []ctrlruntimeclient.Object{
				testConfiguration(
					pmcoordbrokerv1alpha1.MigrationStage{Name: "copy", Progress: true},
					pmcoordbrokerv1alpha1.MigrationStage{Name: "verify"},
				),
			},
			fromStagingObjs: []ctrlruntimeclient.Object{testStagingWidget("Available")},
			toStagingObjs:   []ctrlruntimeclient.Object{testStagingWidget("")},
			wantPending:     true,
			wantMessage:     `stage "copy" completed`,
			wantState:       pmcoordbrokerv1alpha1.MigrationStateInitialCompleted,
			wantStage:       "verify",
		},
		{
			name:      "stage with unmet precondition waits",
			migration: testMigration(pmcoordbrokerv1alpha1.MigrationStatePending),
			coordObjs: []ctrlruntimeclient.Object{
				testConfiguration(pmcoordbrokerv1alpha1.MigrationStage{
					Name:          "copy",
					Preconditions: []string{`to.status.status == "Available"`},
					Templates:     map[string]runtime.RawExtension{"cm": testTemplate(t)},
				}),
			},
			fromStagingObjs: []ctrlruntimeclient.Object{testStagingWidget("Available")},
			toStagingObjs:   []ctrlruntimeclient.Object{testStagingWidget("Provisioning")},
			wantPending:     true,
			wantMessage:     `waiting for stage "copy" preconditions`,
			wantState:       pmcoordbrokerv1alpha1.MigrationStatePending,
		},
		{
			name:      "stage with met precondition completes",
			migration: testMigration(pmcoordbrokerv1alpha1.MigrationStatePending),
			coordObjs: []ctrlruntimeclient.Object{
				testConfiguration(pmcoordbrokerv1alpha1.MigrationStage{
					Name:          "copy",
					Preconditions: []string{`to.status.status == "Available"`},
				}),
			},
			fromStagingObjs: []ctrlruntimeclient.Object{testStagingWidget("Available")},
			toStagingObjs:   []ctrlruntimeclient.Object{testStagingWidget("Available")},
			wantState:       pmcoordbrokerv1alpha1.MigrationStateCutoverInProgress,
		},
		{
			name:      "success condition can reference staging copies",
			migration: testMigration(pmcoordbrokerv1alpha1.MigrationStatePending),
			coordObjs: []ctrlruntimeclient.Object{
				testConfiguration(pmcoordbrokerv1alpha1.MigrationStage{
					Name:              "copy",
					Templates:         map[string]runtime.RawExtension{"cm": testTemplate(t)},
					SuccessConditions: []string{`from.status.status == "Available" && cm.data.key == "value"`},
				}),
			},
			fromStagingObjs: []ctrlruntimeclient.Object{testStagingWidget("Available")},
			toStagingObjs:   []ctrlruntimeclient.Object{testStagingWidget("")},
			wantState:       pmcoordbrokerv1alpha1.MigrationStateCutoverInProgress,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clients := &testClients{
				coordination: testFakeClient(t, tc.coordObjs...),
				provider:     testFakeClient(t),
				fromStaging:  testFakeClient(t, tc.fromStagingObjs...),
				toStaging:    testFakeClient(t, tc.toStagingObjs...),
				compute:      testFakeClient(t),
			}
			sub := &stagesSubroutine{opts: testOptions(t, clients)}
			ctx := subroutines.WithClient(t.Context(), clients.coordination)

			result, err := sub.Process(ctx, tc.migration)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)

			if tc.wantPending {
				assert.Positive(t, result.Requeue())
				assert.Contains(t, result.Message(), tc.wantMessage)
			} else {
				assert.True(t, result.IsContinue())
			}

			assert.Equal(t, tc.wantState, tc.migration.Status.State)
			assert.Equal(t, tc.wantStage, tc.migration.Status.Stage)

			cm := &corev1.ConfigMap{}
			err = clients.compute.Get(t.Context(), types.NamespacedName{Namespace: DefaultStageNamespace, Name: testMigrationName + "-cm"}, cm)
			if tc.wantComputeCM {
				require.NoError(t, err)
				assert.Equal(t, "copy", cm.Labels[MigrationStageLabel])
				assert.Equal(t, testMigrationName, cm.Labels[MigrationNameLabel])
			} else {
				assert.True(t, apierrors.IsNotFound(err))
			}
		})
	}
}

func TestStagesCopiesRelatedResources(t *testing.T) {
	t.Parallel()

	relatedName := "widget-credentials"
	withRelated := func(status string) *unstructured.Unstructured {
		widget := testStagingWidget(status)
		require.NoError(t, unstructured.SetNestedMap(widget.Object, map[string]any{
			"credentials": map[string]any{
				"namespace": testNamespace,
				"name":      relatedName,
				"gvk":       map[string]any{"group": "core", "version": "v1", "kind": "ConfigMap"},
			},
		}, "status", "relatedResources"))
		return widget
	}
	relatedConfigMap := func(value string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testNamespace,
				Name:      relatedName,
			},
			Data: map[string]string{"user": value},
		}
	}

	clients := &testClients{
		coordination: testFakeClient(t),
		provider:     testFakeClient(t),
		fromStaging:  testFakeClient(t, withRelated("Available"), relatedConfigMap("from-user")),
		toStaging:    testFakeClient(t, withRelated(""), relatedConfigMap("to-user")),
		compute:      testFakeClient(t),
	}
	sub := &stagesSubroutine{opts: testOptions(t, clients)}
	ctx := subroutines.WithClient(t.Context(), clients.coordination)

	result, err := sub.Process(ctx, testMigration(pmcoordbrokerv1alpha1.MigrationStatePending))
	require.NoError(t, err)
	assert.True(t, result.IsContinue())

	fromCopy := &corev1.ConfigMap{}
	require.NoError(t, clients.compute.Get(t.Context(), types.NamespacedName{Namespace: testNamespace, Name: "from-" + relatedName}, fromCopy))
	assert.Equal(t, "from-user", fromCopy.Data["user"])

	toCopy := &corev1.ConfigMap{}
	require.NoError(t, clients.compute.Get(t.Context(), types.NamespacedName{Namespace: testNamespace, Name: "to-" + relatedName}, toCopy))
	assert.Equal(t, "to-user", toCopy.Data["user"])
}

func TestStagesFinalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		coordObjs []ctrlruntimeclient.Object
		compute   []ctrlruntimeclient.Object
	}{
		{
			name: "no configuration",
		},
		{
			name: "deletes deployed templates",
			coordObjs: []ctrlruntimeclient.Object{
				testConfiguration(pmcoordbrokerv1alpha1.MigrationStage{
					Name:      "copy",
					Templates: map[string]runtime.RawExtension{"cm": testTemplate(t)},
				}),
			},
			compute: []ctrlruntimeclient.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: DefaultStageNamespace,
						Name:      testMigrationName + "-cm",
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clients := &testClients{
				coordination: testFakeClient(t, tc.coordObjs...),
				provider:     testFakeClient(t),
				fromStaging:  testFakeClient(t),
				toStaging:    testFakeClient(t),
				compute:      testFakeClient(t, tc.compute...),
			}
			sub := &stagesSubroutine{opts: testOptions(t, clients)}
			ctx := subroutines.WithClient(t.Context(), clients.coordination)

			result, err := sub.Finalize(ctx, testMigration(pmcoordbrokerv1alpha1.MigrationStateCutoverCompleted))
			require.NoError(t, err)
			assert.True(t, result.IsContinue())

			cm := &corev1.ConfigMap{}
			err = clients.compute.Get(t.Context(), types.NamespacedName{Namespace: DefaultStageNamespace, Name: testMigrationName + "-cm"}, cm)
			assert.True(t, apierrors.IsNotFound(err))
		})
	}
}

func TestCheckSuccessConditions(t *testing.T) {
	t.Parallel()

	resources := map[string]*unstructured.Unstructured{
		"cm": {
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata":   map[string]any{"name": "stage-cm"},
				"data":       map[string]any{"key": "value"},
			},
		},
	}

	copies := map[string]any{
		"to": map[string]any{
			"status": map[string]any{"status": "Available"},
		},
	}

	tests := []struct {
		name       string
		conditions []string
		want       bool
		wantErr    string
	}{
		{
			name:       "true expression",
			conditions: []string{`cm.data.key == "value"`},
			want:       true,
		},
		{
			name:       "false expression",
			conditions: []string{`cm.data.key == "other"`},
			want:       false,
		},
		{
			name:       "non-boolean expression errors",
			conditions: []string{`cm.data.key`},
			wantErr:    "did not evaluate to a boolean",
		},
		{
			name:       "compile error",
			conditions: []string{`cm.data.key ==`},
			wantErr:    "compiling success condition",
		},
		{
			name:       "metadata field access",
			conditions: []string{`cm.metadata.name == "stage-cm"`},
			want:       true,
		},
		{
			name:       "staging copy access",
			conditions: []string{`to.status.status == "Available"`},
			want:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stage := pmcoordbrokerv1alpha1.MigrationStage{
				Name:              "copy",
				SuccessConditions: tc.conditions,
			}
			got, err := checkSuccessConditions(t.Context(), stage, resources, copies)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
