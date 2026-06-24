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
)

// Result label values.
const (
	ResultSuccess = "success"
	ResultError   = "error"
	ResultAllowed = "allowed"
	ResultDenied  = "denied"
)

// Source label values for auth metrics.
const (
	SourceCache = "cache"
	SourceAPI   = "api"
)

// Operation label values for endpoint metrics.
const (
	OperationQuery        = "query"
	OperationSubscription = "subscription"
)

// Operation label values for resolver metrics.
const (
	OperationList   = "list"
	OperationGet    = "get"
	OperationCreate = "create"
	OperationUpdate = "update"
	OperationDelete = "delete"
	OperationApply  = "apply"
)

// Collector groups all gateway metric structs.
type Collector struct {
	Endpoint *EndpointMetrics
	Resolver *ResolverMetrics
	Auth     *AuthMetrics
}

// NewCollector creates and registers all gateway metrics under a single registerer.
func NewCollector(reg prometheus.Registerer) *Collector {
	return &Collector{
		Endpoint: NewEndpointMetrics(reg),
		Resolver: NewResolverMetrics(reg),
		Auth:     NewAuthMetrics(reg),
	}
}

type SubscriptionMetrics struct {
	Active   prometheus.Gauge
	Total    prometheus.Counter
	Rejected prometheus.Counter
}

func NewSubscriptionMetrics(reg prometheus.Registerer) *SubscriptionMetrics {
	m := &SubscriptionMetrics{
		Active: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "graphql_subscriptions_active",
			Help: "Current number of active (in-flight) GraphQL subscriptions.",
		}),
		Total: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "graphql_subscriptions_total",
			Help: "Total number of GraphQL subscriptions opened.",
		}),
		Rejected: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "graphql_subscriptions_rejected_total",
			Help: "Total number of GraphQL subscriptions rejected due to max-inflight limit.",
		}),
	}
	reg.MustRegister(m.Active, m.Total, m.Rejected)
	return m
}

// EndpointMetrics tracks GraphQL requests per cluster endpoint.
type EndpointMetrics struct {
	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
}

func NewEndpointMetrics(reg prometheus.Registerer) *EndpointMetrics {
	m := &EndpointMetrics{
		RequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubernetes_graphql_gateway_graphql_requests_total",
			Help: "Total number of GraphQL requests by cluster, operation (query/subscription), and result.",
		}, []string{"cluster", "operation", "result"}),
		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "kubernetes_graphql_gateway_graphql_request_duration_seconds",
			Help:    "Duration of GraphQL requests in seconds by cluster and operation.",
			Buckets: prometheus.DefBuckets,
		}, []string{"cluster", "operation"}),
	}
	reg.MustRegister(m.RequestsTotal, m.RequestDuration)
	return m
}

// Record increments the requests counter and observes the duration.
func (m *EndpointMetrics) Record(cluster, operation string, d time.Duration, result string) {
	m.RequestsTotal.WithLabelValues(cluster, operation, result).Inc()
	m.RequestDuration.WithLabelValues(cluster, operation).Observe(d.Seconds())
}

// ResolverMetrics tracks Kubernetes API calls made by GraphQL resolvers.
type ResolverMetrics struct {
	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
}

func NewResolverMetrics(reg prometheus.Registerer) *ResolverMetrics {
	m := &ResolverMetrics{
		RequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubernetes_graphql_gateway_api_requests_total",
			Help: "Total number of Kubernetes API calls by operation (list/get/create/update/delete/apply), kind, and result.",
		}, []string{"operation", "kind", "result"}),
		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "kubernetes_graphql_gateway_api_request_duration_seconds",
			Help:    "Duration of Kubernetes API calls in seconds by operation and kind.",
			Buckets: prometheus.DefBuckets,
		}, []string{"operation", "kind"}),
	}
	reg.MustRegister(m.RequestsTotal, m.RequestDuration)
	return m
}

// Record increments the requests counter and observes the duration.
func (m *ResolverMetrics) Record(operation, kind string, d time.Duration, result string) {
	m.RequestsTotal.WithLabelValues(operation, kind, result).Inc()
	m.RequestDuration.WithLabelValues(operation, kind).Observe(d.Seconds())
}

// AuthMetrics tracks token authentication attempts.
type AuthMetrics struct {
	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
}

func NewAuthMetrics(reg prometheus.Registerer) *AuthMetrics {
	m := &AuthMetrics{
		RequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubernetes_graphql_gateway_auth_requests_total",
			Help: "Total number of token authentication attempts by result (allowed/denied/error) and source (cache/api).",
		}, []string{"result", "source"}),
		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "kubernetes_graphql_gateway_auth_request_duration_seconds",
			Help:    "Duration of token authentication API calls in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"source"}),
	}
	reg.MustRegister(m.RequestsTotal, m.RequestDuration)
	return m
}

// RecordAPICall records a real TokenReview API call result and its duration.
func (m *AuthMetrics) RecordAPICall(result string, d time.Duration) {
	m.RequestsTotal.WithLabelValues(result, SourceAPI).Inc()
	m.RequestDuration.WithLabelValues(SourceAPI).Observe(d.Seconds())
}

// RecordCacheHit records a cache-served auth result. Duration is not recorded
// because 0-duration cache hits would pollute the real API latency histogram.
func (m *AuthMetrics) RecordCacheHit(result string) {
	m.RequestsTotal.WithLabelValues(result, SourceCache).Inc()
}
