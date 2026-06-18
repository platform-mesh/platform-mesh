package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// ReconcileTotal counts reconcile calls per controller and result (success/error).
	ReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "security_operator_reconcile_total",
			Help: "Total number of reconcile calls by controller and result.",
		},
		[]string{"controller", "result"},
	)

	// ReconcileDuration observes how long each reconcile loop takes, labelled by controller.
	ReconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "security_operator_reconcile_duration_seconds",
			Help:    "Duration of reconcile calls in seconds by controller.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"controller"},
	)

	// FGAOperations counts OpenFGA tuple operations by operation (apply/delete/list) and result.
	FGAOperations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "security_operator_fga_operations_total",
			Help: "Total number of OpenFGA tuple operations by operation and result.",
		},
		[]string{"operation", "result"},
	)
)

func init() {
	ctrlmetrics.Registry.MustRegister(
		ReconcileTotal,
		ReconcileDuration,
		FGAOperations,
	)
}
