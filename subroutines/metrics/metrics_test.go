package metrics

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.platform-mesh.io/subroutines"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

func TestMetricsRegistered(t *testing.T) {
	// Record some data so Gather returns our metrics.
	Record("test-ctrl", "test-sub", "process", subroutines.OK(), nil, 100*time.Millisecond)

	families, err := metrics.Registry.Gather()
	require.NoError(t, err)

	names := make(map[string]bool)
	for _, f := range families {
		names[f.GetName()] = true
	}

	assert.True(t, names["lifecycle_subroutine_duration_seconds"], "duration metric should be registered")
}

func TestRecord(t *testing.T) {
	tests := []struct {
		name   string
		result subroutines.Result
		err    error
	}{
		{
			name:   "OK result",
			result: subroutines.OK(),
		},
		{
			name:   "Pending result",
			result: subroutines.Pending(5*time.Second, "waiting"),
		},
		{
			name:   "Stop result",
			result: subroutines.Stop("halted"),
		},
		{
			name:   "StopWithRequeue result",
			result: subroutines.StopWithRequeue(10*time.Second, "rate limited"),
		},
		{
			name:   "error",
			result: subroutines.OK(),
			err:    errors.New("boom"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				Record("test-ctrl", "test-sub", "process", tt.result, tt.err, 100*time.Millisecond)
			})
		})
	}
}

func TestRecordWithRequeue(t *testing.T) {
	// Verify that requeue histogram is populated when requeue > 0.
	Record("test-ctrl", "requeue-sub", "process", subroutines.OKWithRequeue(5*time.Second), nil, 50*time.Millisecond)

	families, err := metrics.Registry.Gather()
	require.NoError(t, err)

	found := false
	for _, f := range families {
		if f.GetName() == "lifecycle_subroutine_requeue_seconds" {
			found = true
			break
		}
	}
	assert.True(t, found, "requeue metric should have data after OKWithRequeue")
}

func TestOutcomeLabel(t *testing.T) {
	tests := []struct {
		name   string
		result subroutines.Result
		err    error
		want   string
	}{
		{"ok", subroutines.OK(), nil, "ok"},
		{"pending", subroutines.Pending(time.Second, ""), nil, "pending"},
		{"stop", subroutines.Stop(""), nil, "stop"},
		{"stop with requeue", subroutines.StopWithRequeue(time.Second, ""), nil, "stop"},
		{"error", subroutines.OK(), errors.New("x"), "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, outcomeLabel(tt.result, tt.err))
		})
	}
}
