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

package controller

import (
	"github.com/prometheus/client_golang/prometheus"

	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

//nolint:goconst
var (
	shardDistribution = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "resource_sharding_distribution",
			Help: "Number of resources assigned to each shard",
		},
		[]string{"resourcesharding", "shard"},
	)

	shardImbalanceRatio = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "resource_sharding_imbalance_ratio",
			Help: "Maximum deviation from ideal distribution (0.0 = perfect balance)",
		},
		[]string{"resourcesharding"},
	)

	assignmentsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "resource_sharding_assignments_total",
			Help: "Total number of shard assignments made",
		},
		[]string{"resourcesharding", "shard"},
	)

	rebalanceMovesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "resource_sharding_rebalance_moves_total",
			Help: "Total number of resources moved during rebalancing",
		},
		[]string{"resourcesharding"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		shardDistribution,
		shardImbalanceRatio,
		assignmentsTotal,
		rebalanceMovesTotal,
	)
}
