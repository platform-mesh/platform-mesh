package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.platform-mesh.io/subroutines"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	subroutineDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "lifecycle_subroutine_duration_seconds",
			Help:    "Duration of subroutine execution in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"controller", "subroutine", "action", "outcome"},
	)

	subroutineErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "lifecycle_subroutine_errors_total",
			Help: "Total number of subroutine errors.",
		},
		[]string{"controller", "subroutine", "action"},
	)

	subroutineRequeue = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "lifecycle_subroutine_requeue_seconds",
			Help:    "Requeue duration of subroutine execution in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"controller", "subroutine", "outcome"},
	)
)

func init() {
	metrics.Registry.MustRegister(subroutineDuration, subroutineErrors, subroutineRequeue)
}

// Record records metrics for a subroutine execution.
func Record(controllerName, subroutineName, action string, result subroutines.Result, err error, duration time.Duration) {
	outcome := outcomeLabel(result, err)

	subroutineDuration.WithLabelValues(controllerName, subroutineName, action, outcome).Observe(duration.Seconds())

	if err != nil {
		subroutineErrors.WithLabelValues(controllerName, subroutineName, action).Inc()
	}

	if requeue := result.Requeue(); requeue > 0 {
		subroutineRequeue.WithLabelValues(controllerName, subroutineName, outcome).Observe(requeue.Seconds())
	}
}

func outcomeLabel(result subroutines.Result, err error) string {
	switch {
	case err != nil:
		return "error"
	case result.IsPending():
		return "pending"
	case result.IsStopWithRequeue() || result.IsStop():
		return "stop"
	default:
		return "ok"
	}
}
