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
