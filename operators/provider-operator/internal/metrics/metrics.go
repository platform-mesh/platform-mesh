package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// ReconcileTotal counts reconcile calls per controller and result (success/error).
	ReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "platform_mesh_operator_reconcile_total",
			Help: "Total number of reconcile calls by controller and result.",
		},
		[]string{"controller", "result"},
	)

	// SubroutineTotal counts Process calls per subroutine and result (success/error).
	SubroutineTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "platform_mesh_operator_subroutine_total",
			Help: "Total number of subroutine Process calls by subroutine and result.",
		},
		[]string{"subroutine", "result"},
	)

	// SubroutineDuration observes how long each subroutine Process call takes.
	SubroutineDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "platform_mesh_operator_subroutine_duration_seconds",
			Help:    "Duration of subroutine Process calls in seconds by subroutine.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"subroutine"},
	)
)

func init() {
	ctrlmetrics.Registry.MustRegister(
		ReconcileTotal,
		SubroutineTotal,
		SubroutineDuration,
	)
}
