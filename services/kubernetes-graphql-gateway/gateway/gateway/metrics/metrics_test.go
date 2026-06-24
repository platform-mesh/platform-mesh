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
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSubscriptionMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewSubscriptionMetrics(reg)

	families, err := reg.Gather()
	require.NoError(t, err)

	names := make(map[string]struct{})
	for _, f := range families {
		names[f.GetName()] = struct{}{}
	}
	assert.Contains(t, names, "graphql_subscriptions_active")
	assert.Contains(t, names, "graphql_subscriptions_total")
	assert.Contains(t, names, "graphql_subscriptions_rejected_total")

	assert.Equal(t, 0.0, gaugeValue(t, m.Active))
	assert.Equal(t, 0.0, counterValue(t, m.Total))
	assert.Equal(t, 0.0, counterValue(t, m.Rejected))
}

func TestSubscriptionMetricsIncDec(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewSubscriptionMetrics(reg)

	m.Active.Inc()
	m.Active.Inc()
	m.Total.Inc()
	m.Total.Inc()
	m.Total.Inc()
	m.Rejected.Inc()

	assert.Equal(t, 2.0, gaugeValue(t, m.Active))
	assert.Equal(t, 3.0, counterValue(t, m.Total))
	assert.Equal(t, 1.0, counterValue(t, m.Rejected))

	m.Active.Dec()
	assert.Equal(t, 1.0, gaugeValue(t, m.Active))
}

func TestNewEndpointMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewEndpointMetrics(reg)

	m.RequestsTotal.WithLabelValues("prod", "query", "success").Inc()
	m.RequestsTotal.WithLabelValues("prod", "query", "success").Inc()
	m.RequestDuration.WithLabelValues("prod", "subscription").Observe(0.01)

	families, err := reg.Gather()
	require.NoError(t, err)
	names := make(map[string]struct{})
	for _, f := range families {
		names[f.GetName()] = struct{}{}
	}
	assert.Contains(t, names, "kubernetes_graphql_gateway_graphql_requests_total")
	assert.Contains(t, names, "kubernetes_graphql_gateway_graphql_request_duration_seconds")
	assert.Equal(t, 2.0, counterVecValue(t, m.RequestsTotal, "prod", "query", "success"))
}

func TestNewResolverMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewResolverMetrics(reg)

	m.RequestsTotal.WithLabelValues("list", "Pod", "success").Inc()
	m.RequestDuration.WithLabelValues("get", "Deployment").Observe(0.05)

	families, err := reg.Gather()
	require.NoError(t, err)
	names := make(map[string]struct{})
	for _, f := range families {
		names[f.GetName()] = struct{}{}
	}
	assert.Contains(t, names, "kubernetes_graphql_gateway_api_requests_total")
	assert.Contains(t, names, "kubernetes_graphql_gateway_api_request_duration_seconds")
	assert.Equal(t, 1.0, counterVecValue(t, m.RequestsTotal, "list", "Pod", "success"))
}

func TestNewAuthMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewAuthMetrics(reg)

	m.RequestsTotal.WithLabelValues("allowed", "api").Inc()
	m.RequestsTotal.WithLabelValues("denied", "cache").Inc()
	m.RequestDuration.WithLabelValues("api").Observe(0.02)

	families, err := reg.Gather()
	require.NoError(t, err)
	names := make(map[string]struct{})
	for _, f := range families {
		names[f.GetName()] = struct{}{}
	}
	assert.Contains(t, names, "kubernetes_graphql_gateway_auth_requests_total")
	assert.Contains(t, names, "kubernetes_graphql_gateway_auth_request_duration_seconds")
	assert.Equal(t, 1.0, counterVecValue(t, m.RequestsTotal, "allowed", "api"))
	assert.Equal(t, 1.0, counterVecValue(t, m.RequestsTotal, "denied", "cache"))
}

func gaugeValue(t *testing.T, g prometheus.Gauge) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, g.Write(&m))
	return m.GetGauge().GetValue()
}

func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, c.Write(&m))
	return m.GetCounter().GetValue()
}

func counterVecValue(t *testing.T, cv *prometheus.CounterVec, lvs ...string) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, cv.WithLabelValues(lvs...).Write(&m))
	return m.GetCounter().GetValue()
}
