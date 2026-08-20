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

package restore

import (
	"testing"

	"github.com/stretchr/testify/require"

	pmbackupv1alpha1 "go.platform-mesh.io/apis/backup/v1alpha1"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSetPhaseUpdatesProgressStatusAtomically(t *testing.T) {
	restore := &pmbackupv1alpha1.PlatformRestore{ObjectMeta: metav1.ObjectMeta{Generation: 7}}

	require.True(t, setPhase(restore, pmbackupv1alpha1.RestorePhaseRestoringEtcd))
	require.Equal(t, pmbackupv1alpha1.RestorePhaseRestoringEtcd, restore.Status.Phase)
	require.Equal(t, int64(7), restore.Status.ObservedGeneration)

	ready := meta.FindStatusCondition(restore.Status.Conditions, conditionReady)
	require.NotNil(t, ready)
	require.Equal(t, metav1.ConditionFalse, ready.Status)
	require.Equal(t, "RestoreInProgress", ready.Reason)
	require.Equal(t, int64(7), ready.ObservedGeneration)
	require.False(t, setPhase(restore, pmbackupv1alpha1.RestorePhaseRestoringEtcd))
}

func TestMarkRestoreSucceededUpdatesTerminalStatusAtomically(t *testing.T) {
	restore := &pmbackupv1alpha1.PlatformRestore{ObjectMeta: metav1.ObjectMeta{Generation: 9}}
	require.True(t, setPhase(restore, pmbackupv1alpha1.RestorePhaseValidatingIdentity))

	require.True(t, markRestoreSucceeded(restore))
	require.Equal(t, pmbackupv1alpha1.RestorePhaseSucceeded, restore.Status.Phase)
	require.Equal(t, int64(9), restore.Status.ObservedGeneration)

	ready := meta.FindStatusCondition(restore.Status.Conditions, conditionReady)
	require.NotNil(t, ready)
	require.Equal(t, metav1.ConditionTrue, ready.Status)
	require.Equal(t, "PlatformRestoreCompleted", ready.Reason)
	require.Equal(t, int64(9), ready.ObservedGeneration)
	require.True(t, conditionIsTrue(restore, conditionIdentityPlaneValidated))
	require.False(t, markRestoreSucceeded(restore))
}

func TestMarkPhaseReadyCompletesCurrentPhaseBeforeAdvancing(t *testing.T) {
	restore := &pmbackupv1alpha1.PlatformRestore{ObjectMeta: metav1.ObjectMeta{Generation: 11}}
	require.True(t, setPhase(restore, pmbackupv1alpha1.RestorePhaseQuiescingPlatform))

	require.True(t, markPhaseReady(
		restore,
		conditionPlatformQuiesced,
		"PlatformQuiesced",
		"Platform workloads are quiesced",
	))
	ready := meta.FindStatusCondition(restore.Status.Conditions, conditionReady)
	require.NotNil(t, ready)
	require.Equal(t, metav1.ConditionTrue, ready.Status)
	require.Equal(t, "QuiescingPlatformCompleted", ready.Reason)
	require.Equal(t, pmbackupv1alpha1.RestorePhaseQuiescingPlatform, restore.Status.Phase)

	require.True(t, setPhase(restore, pmbackupv1alpha1.RestorePhaseRestoringEtcd))
	ready = meta.FindStatusCondition(restore.Status.Conditions, conditionReady)
	require.NotNil(t, ready)
	require.Equal(t, metav1.ConditionFalse, ready.Status)
	require.Equal(t, pmbackupv1alpha1.RestorePhaseRestoringEtcd, restore.Status.Phase)
}
