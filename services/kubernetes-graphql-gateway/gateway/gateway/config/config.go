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

package config

import (
	"time"

	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/authn"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/metrics"
)

// Gateway holds the complete gateway service configuration.
type Gateway struct {
	// SchemaHandler specifies which watcher to use ("file" or "grpc")
	SchemaHandler string

	// SchemaDirectory is the directory to watch when SchemaHandler is "file"
	SchemaDirectory string

	// GRPCAddress is the gRPC server address when SchemaHandler is "grpc"
	GRPCAddress string

	// GRPCMaxRecvMsgSize is the maximum gRPC message size in bytes the gateway will accept.
	GRPCMaxRecvMsgSize int

	// GraphQL contains GraphQL-specific configuration
	GraphQL GraphQL

	// Limits contains DoS mitigation resource limits
	Limits Limits

	// TokenReviewCacheTTL is the duration to cache TokenReview results.
	// Ignored when Validator is non-nil — the supplied validator owns its
	// own caching strategy.
	TokenReviewCacheTTL time.Duration

	// Validator authenticates incoming bearer tokens. When nil (the default),
	// each endpoint builds a TokenReviewValidator against its cluster's admin
	// config and owns the validator's lifecycle.
	//
	// Set this when the gateway is embedded behind a mux that has already
	// authenticated the caller — pass authn.NoopValidator{} to skip the
	// per-request TokenReview entirely, or inject a custom Validator for
	// alternate auth strategies.
	//
	// When set, the caller owns the validator's lifecycle (e.g. calling
	// Start on TokenReviewValidator). The same Validator instance is shared
	// across all endpoints.
	Validator authn.Validator

	// Metrics is optional. When non-nil, all components record Prometheus
	// metrics. Pass nil to disable instrumentation entirely.
	Metrics *metrics.Collector
}

// GraphQL holds GraphQL handler configuration.
type GraphQL struct {
	Pretty            bool
	PlaygroundEnabled bool
	GraphiQL          bool
}

// Limits holds query validation limits enforced at the GraphQL layer.
// HTTP-level limits (body size, in-flight concurrency) live in http.ServerConfig.
type Limits struct {
	// MaxQueryDepth is the maximum allowed nesting depth for GraphQL queries.
	// 0 disables the limit.
	MaxQueryDepth int

	// MaxQueryComplexity is the maximum allowed complexity score for GraphQL queries.
	// Each field resolution counts as 1 point of complexity.
	// 0 disables the limit.
	MaxQueryComplexity int

	// MaxQueryBatchSize is the maximum number of queries allowed in a single batched request.
	// 0 disables the limit.
	MaxQueryBatchSize int
}
