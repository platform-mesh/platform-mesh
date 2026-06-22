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
