package restore

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.platform-mesh.io/apis/backup/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSetPhaseUpdatesProgressStatusAtomically(t *testing.T) {
	restore := &v1alpha1.PlatformRestore{ObjectMeta: metav1.ObjectMeta{Generation: 7}}

	require.True(t, setPhase(restore, v1alpha1.RestorePhaseRestoringEtcd))
	require.Equal(t, v1alpha1.RestorePhaseRestoringEtcd, restore.Status.Phase)
	require.Equal(t, int64(7), restore.Status.ObservedGeneration)

	ready := meta.FindStatusCondition(restore.Status.Conditions, conditionReady)
	require.NotNil(t, ready)
	require.Equal(t, metav1.ConditionFalse, ready.Status)
	require.Equal(t, "RestoreInProgress", ready.Reason)
	require.Equal(t, int64(7), ready.ObservedGeneration)
	require.False(t, setPhase(restore, v1alpha1.RestorePhaseRestoringEtcd))
}

func TestMarkRestoreSucceededUpdatesTerminalStatusAtomically(t *testing.T) {
	restore := &v1alpha1.PlatformRestore{ObjectMeta: metav1.ObjectMeta{Generation: 9}}
	require.True(t, setPhase(restore, v1alpha1.RestorePhaseValidatingIdentity))

	require.True(t, markRestoreSucceeded(restore))
	require.Equal(t, v1alpha1.RestorePhaseSucceeded, restore.Status.Phase)
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
	restore := &v1alpha1.PlatformRestore{ObjectMeta: metav1.ObjectMeta{Generation: 11}}
	require.True(t, setPhase(restore, v1alpha1.RestorePhaseQuiescingPlatform))

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
	require.Equal(t, v1alpha1.RestorePhaseQuiescingPlatform, restore.Status.Phase)

	require.True(t, setPhase(restore, v1alpha1.RestorePhaseRestoringEtcd))
	ready = meta.FindStatusCondition(restore.Status.Conditions, conditionReady)
	require.NotNil(t, ready)
	require.Equal(t, metav1.ConditionFalse, ready.Status)
	require.Equal(t, v1alpha1.RestorePhaseRestoringEtcd, restore.Status.Phase)
}
