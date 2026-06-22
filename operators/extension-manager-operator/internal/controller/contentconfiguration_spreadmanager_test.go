package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"go.platform-mesh.io/subroutines/spread"

	"go.platform-mesh.io/apis/ui/v1alpha1"
)

func TestLegacyNextReconcileDelay_withinMax(t *testing.T) {
	tests := []time.Duration{
		10 * time.Minute,
		time.Hour,
		24 * time.Hour,
	}
	for _, max := range tests {
		t.Run(max.String(), func(t *testing.T) {
			for range 200 {
				got := nextReconcileDelay(max)
				require.GreaterOrEqual(t, got, max/2, "delay should be at least half of max")
				require.Less(t, got, max, "delay should stay below max")
			}
		})
	}
}

func TestContentConfigurationSpread_ReconcileRequired(t *testing.T) {
	var s contentConfigurationSpreadManager

	t.Run("wrong type panics", func(t *testing.T) {
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p"}}
		require.Panics(t, func() { _ = s.ReconcileRequired(pod) })
	})

	t.Run("generation differs from observed", func(t *testing.T) {
		cc := &v1alpha1.ContentConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 2,
				Labels:     map[string]string{},
			},
			Status: v1alpha1.ContentConfigurationStatus{
				ObservedGeneration: 1,
				NextReconcileTime:  metav1.NewTime(time.Now().Add(time.Hour)),
			},
		}
		require.True(t, s.ReconcileRequired(cc))
	})

	t.Run("refresh label forces reconcile", func(t *testing.T) {
		cc := &v1alpha1.ContentConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
				Labels: map[string]string{
					spread.RefreshLabel: "true",
				},
			},
			Status: v1alpha1.ContentConfigurationStatus{
				ObservedGeneration: 1,
				NextReconcileTime:  metav1.NewTime(time.Now().Add(time.Hour)),
			},
		}
		require.True(t, s.ReconcileRequired(cc))
	})

	t.Run("zero next reconcile time", func(t *testing.T) {
		cc := &v1alpha1.ContentConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
				Labels:     map[string]string{},
			},
			Status: v1alpha1.ContentConfigurationStatus{
				ObservedGeneration: 1,
				NextReconcileTime:  metav1.Time{},
			},
		}
		require.True(t, s.ReconcileRequired(cc))
	})

	t.Run("next reconcile in future", func(t *testing.T) {
		cc := &v1alpha1.ContentConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
				Labels:     map[string]string{},
			},
			Status: v1alpha1.ContentConfigurationStatus{
				ObservedGeneration: 1,
				NextReconcileTime:  metav1.NewTime(time.Now().Add(30 * time.Minute)),
			},
		}
		require.False(t, s.ReconcileRequired(cc))
	})

	t.Run("next reconcile in past", func(t *testing.T) {
		cc := &v1alpha1.ContentConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Generation: 1,
				Labels:     map[string]string{},
			},
			Status: v1alpha1.ContentConfigurationStatus{
				ObservedGeneration: 1,
				NextReconcileTime:  metav1.NewTime(time.Now().Add(-time.Minute)),
			},
		}
		require.True(t, s.ReconcileRequired(cc))
	})
}

func TestContentConfigurationSpread_RequeueDelay(t *testing.T) {
	var s contentConfigurationSpreadManager

	t.Run("wrong type panics", func(t *testing.T) {
		require.Panics(t, func() { _ = s.RequeueDelay(&corev1.Pod{}) })
	})

	t.Run("zero next reconcile", func(t *testing.T) {
		cc := &v1alpha1.ContentConfiguration{
			Status: v1alpha1.ContentConfigurationStatus{
				NextReconcileTime: metav1.Time{},
			},
		}
		require.Equal(t, time.Duration(0), s.RequeueDelay(cc))
	})

	t.Run("future next reconcile", func(t *testing.T) {
		future := time.Now().UTC().Add(7 * time.Minute)
		cc := &v1alpha1.ContentConfiguration{
			Status: v1alpha1.ContentConfigurationStatus{
				NextReconcileTime: metav1.NewTime(future),
			},
		}
		got := s.RequeueDelay(cc)
		require.Greater(t, got, 6*time.Minute)
		require.Less(t, got, 8*time.Minute)
	})

	t.Run("past next reconcile", func(t *testing.T) {
		cc := &v1alpha1.ContentConfiguration{
			Status: v1alpha1.ContentConfigurationStatus{
				NextReconcileTime: metav1.NewTime(time.Now().Add(-time.Second)),
			},
		}
		require.Equal(t, time.Duration(0), s.RequeueDelay(cc))
	})
}

func TestContentConfigurationSpread_SetNextReconcileTime(t *testing.T) {
	var s contentConfigurationSpreadManager

	t.Run("wrong type panics", func(t *testing.T) {
		pod := &corev1.Pod{}
		require.Panics(t, func() { s.SetNextReconcileTime(pod) })
	})

	t.Run("default max uses 24h window", func(t *testing.T) {
		cc := &v1alpha1.ContentConfiguration{}
		before := time.Now()
		s.SetNextReconcileTime(cc)
		after := time.Now()
		nrt := cc.Status.NextReconcileTime.Time
		require.False(t, nrt.IsZero())
		require.True(t, nrt.After(before))
		// Legacy jitter: delay in [12h, 24h) for default border.
		require.True(t, !nrt.Before(before.Add(12*time.Hour)))
		require.True(t, nrt.Before(after.Add(24*time.Hour)))
	})

	t.Run("remote spec uses shorter GenerateNextReconcileTime", func(t *testing.T) {
		cc := &v1alpha1.ContentConfiguration{
			Spec: v1alpha1.ContentConfigurationSpec{
				RemoteConfiguration: &v1alpha1.RemoteConfiguration{},
			},
		}
		before := time.Now()
		s.SetNextReconcileTime(cc)
		nrt := cc.Status.NextReconcileTime.Time
		require.False(t, nrt.IsZero())
		// Five-minute cap: int64(floor) jitter => delays are 2-3 minutes.
		delay := nrt.Sub(before)
		require.GreaterOrEqual(t, delay, 2*time.Minute)
		require.Less(t, delay, 5*time.Minute)
	})
}

func TestContentConfigurationSpread_UpdateObservedGeneration(t *testing.T) {
	var s contentConfigurationSpreadManager

	t.Run("wrong type panics", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Generation: 3},
		}
		require.Panics(t, func() { s.UpdateObservedGeneration(pod) })
	})

	t.Run("sets observed from generation", func(t *testing.T) {
		cc := &v1alpha1.ContentConfiguration{
			ObjectMeta: metav1.ObjectMeta{Generation: 7},
		}
		s.UpdateObservedGeneration(cc)
		require.EqualValues(t, 7, cc.Status.ObservedGeneration)
	})
}

func TestContentConfigurationSpread_RemoveRefreshLabel(t *testing.T) {
	var s contentConfigurationSpreadManager

	t.Run("wrong type panics", func(t *testing.T) {
		require.Panics(t, func() { _ = s.RemoveRefreshLabel(&corev1.Pod{}) })
	})

	t.Run("nil labels", func(t *testing.T) {
		cc := &v1alpha1.ContentConfiguration{}
		require.False(t, s.RemoveRefreshLabel(cc))
	})

	t.Run("label absent", func(t *testing.T) {
		cc := &v1alpha1.ContentConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"other": "v"},
			},
		}
		require.False(t, s.RemoveRefreshLabel(cc))
		require.Contains(t, cc.GetLabels(), "other")
	})

	t.Run("removes refresh label", func(t *testing.T) {
		cc := &v1alpha1.ContentConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					spread.RefreshLabel: "true",
					"keep":              "yes",
				},
			},
		}
		require.True(t, s.RemoveRefreshLabel(cc))
		_, has := cc.GetLabels()[spread.RefreshLabel]
		require.False(t, has)
		require.Equal(t, "yes", cc.GetLabels()["keep"])
	})
}
