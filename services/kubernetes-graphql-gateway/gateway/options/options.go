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

package options

import (
	"errors"
	"time"

	"github.com/spf13/pflag"

	"go.platform-mesh.io/kubernetes-graphql-gateway/defaults"

	"k8s.io/component-base/logs"
	logsv1 "k8s.io/component-base/logs/api/v1"
)

type Options struct {
	Logs *logs.Options

	ExtraOptions
}

type ExtraOptions struct {
	// SchemasDir is the directory to store schema files (used with file watcher).
	SchemasDir string
	// SchemaHandler specifies how to receive schema updates ("file" or "grpc").
	SchemaHandler string
	// GRPCListenerAddress is the address of the gRPC listener (used with grpc watcher).
	GRPCListenerAddress string
	// GRPCMaxRecvMsgSize is the maximum gRPC message size in bytes the gateway will accept.
	GRPCMaxRecvMsgSize int
	// ServerBindAddress is the address for the GraphQL gateway server.
	ServerBindAddress string
	// ServerBindPort is the port for the GraphQL gateway server.
	ServerBindPort int
	// PlaygroundEnabled indicates whether to enable the GraphQL playground.
	PlaygroundEnabled bool
	// ResourcesByCategoryEnabled is the feature gate for the resourcesByCategory query and subscription.
	ResourcesByCategoryEnabled bool
	// CORSAllowedOrigins is the list of allowed origins for CORS.
	CORSAllowedOrigins []string
	// CORSAllowedHeaders is the list of allowed headers for CORS.
	CORSAllowedHeaders []string
	// TokenReviewCacheTTL is the duration to cache TokenReview results.
	TokenReviewCacheTTL time.Duration
	// RequestTimeout is the maximum duration for non-streaming GraphQL requests.
	RequestTimeout time.Duration
	// SubscriptionTimeout is the maximum duration for a single SSE subscription.
	SubscriptionTimeout time.Duration
	// MaxRequestBodyBytes is the maximum allowed request body size in bytes.
	MaxRequestBodyBytes int64
	// MaxInFlightRequests is the maximum number of concurrent in-flight requests.
	MaxInFlightRequests int
	// MaxInFlightSubscriptions is the maximum number of concurrent in-flight SSE subscriptions.
	MaxInFlightSubscriptions int
	// MaxQueryDepth is the maximum allowed nesting depth for GraphQL queries.
	MaxQueryDepth int
	// MaxQueryComplexity is the maximum allowed complexity score for GraphQL queries.
	MaxQueryComplexity int
	// MaxQueryBatchSize is the maximum number of queries allowed in a single batched request.
	MaxQueryBatchSize int
	// ReadHeaderTimeout is the maximum duration for reading request headers.
	ReadHeaderTimeout time.Duration
	// IdleTimeout is the maximum duration an idle keep-alive connection remains open.
	IdleTimeout time.Duration
	// EndpointSuffix is the suffix appended to the cluster endpoint path (e.g. "/graphql").
	EndpointSuffix string
}

type completedOptions struct {
	Logs *logs.Options

	ExtraOptions
}

type CompletedOptions struct {
	*completedOptions
}

func NewOptions() *Options {
	// Default to -v=2
	logs := logs.NewOptions()
	logs.Verbosity = logsv1.VerbosityLevel(2)

	opts := &Options{
		Logs: logs,

		ExtraOptions: ExtraOptions{
			SchemasDir:                 "_output/schemas",
			SchemaHandler:              "file",
			GRPCListenerAddress:        "localhost:50051",
			GRPCMaxRecvMsgSize:         defaults.DefaultGRPCMaxMsgSize,
			ServerBindAddress:          "0.0.0.0",
			ServerBindPort:             8080,
			PlaygroundEnabled:          false,
			ResourcesByCategoryEnabled: false,
			CORSAllowedOrigins:         []string{},
			CORSAllowedHeaders:         []string{},
			TokenReviewCacheTTL:        30 * time.Second,
			RequestTimeout:             60 * time.Second,
			SubscriptionTimeout:        30 * time.Minute,
			MaxRequestBodyBytes:        3 * 1024 * 1024,
			MaxInFlightRequests:        400,
			MaxInFlightSubscriptions:   50,
			MaxQueryDepth:              10,
			MaxQueryComplexity:         1000,
			MaxQueryBatchSize:          10,
			ReadHeaderTimeout:          32 * time.Second,
			IdleTimeout:                90 * time.Second,
			EndpointSuffix:             "/graphql",
		},
	}
	return opts
}

func (options *Options) AddFlags(fs *pflag.FlagSet) {
	logsv1.AddFlags(options.Logs, fs)

	fs.StringVar(&options.SchemasDir, "schemas-dir", options.SchemasDir, "directory to watch for schema files (used with --schema-handler=file)")
	fs.StringVar(&options.SchemaHandler, "schema-handler", options.SchemaHandler, "how to receive schema updates: 'file' or 'grpc'")
	fs.StringVar(&options.GRPCListenerAddress, "grpc-listener-address", options.GRPCListenerAddress, "address of the gRPC listener (used with --schema-handler=grpc)")
	fs.IntVar(&options.GRPCMaxRecvMsgSize, "grpc-max-recv-msg-size", options.GRPCMaxRecvMsgSize, "maximum gRPC receive message size in bytes (used with --schema-handler=grpc)")
	fs.IntVar(&options.ServerBindPort, "gateway-port", options.ServerBindPort, "port for the GraphQL gateway server")
	fs.StringVar(&options.ServerBindAddress, "gateway-address", options.ServerBindAddress, "address for the GraphQL gateway server")
	fs.BoolVar(&options.PlaygroundEnabled, "enable-playground", options.PlaygroundEnabled, "enable the GraphQL playground (allows unauthenticated GET requests to serve the playground UI)")
	fs.BoolVar(&options.ResourcesByCategoryEnabled, "enable-resources-by-category", options.ResourcesByCategoryEnabled, "enable the resourcesByCategory query and subscription")
	fs.StringSliceVar(&options.CORSAllowedOrigins, "cors-allowed-origins", options.CORSAllowedOrigins, "list of allowed origins for CORS")
	fs.StringSliceVar(&options.CORSAllowedHeaders, "cors-allowed-headers", options.CORSAllowedHeaders, "list of allowed headers for CORS")
	fs.DurationVar(&options.TokenReviewCacheTTL, "token-review-cache-ttl", options.TokenReviewCacheTTL, "TTL for cached TokenReview results (0 to disable caching)")
	fs.DurationVar(&options.RequestTimeout, "request-timeout", options.RequestTimeout, "maximum duration for non-streaming GraphQL requests (0 to disable)")
	fs.DurationVar(&options.SubscriptionTimeout, "subscription-timeout", options.SubscriptionTimeout, "maximum duration for SSE subscription connections (0 to disable)")
	fs.Int64Var(&options.MaxRequestBodyBytes, "max-request-body-bytes", options.MaxRequestBodyBytes, "maximum allowed request body size in bytes (0 to disable)")
	fs.IntVar(&options.MaxInFlightRequests, "max-inflight-requests", options.MaxInFlightRequests, "maximum number of concurrent in-flight requests (0 to disable)")
	fs.IntVar(&options.MaxInFlightSubscriptions, "max-inflight-subscriptions", options.MaxInFlightSubscriptions, "maximum number of concurrent in-flight SSE subscriptions (0 to disable)")
	fs.IntVar(&options.MaxQueryDepth, "max-query-depth", options.MaxQueryDepth, "maximum allowed nesting depth for GraphQL queries (0 to disable)")
	fs.IntVar(&options.MaxQueryComplexity, "max-query-complexity", options.MaxQueryComplexity, "maximum allowed complexity score for GraphQL queries (0 to disable)")
	fs.IntVar(&options.MaxQueryBatchSize, "max-query-batch-size", options.MaxQueryBatchSize, "maximum number of queries allowed in a single batched request (0 to disable)")
	fs.DurationVar(&options.ReadHeaderTimeout, "read-header-timeout", options.ReadHeaderTimeout, "maximum duration for reading request headers (0 to disable)")
	fs.DurationVar(&options.IdleTimeout, "idle-timeout", options.IdleTimeout, "maximum duration an idle keep-alive connection remains open (0 to disable)")
	fs.StringVar(&options.EndpointSuffix, "endpoint-suffix", options.EndpointSuffix, "suffix appended to the cluster endpoint path (default \"/graphql\")")
}

func (options *Options) Complete() (*CompletedOptions, error) {
	co := &CompletedOptions{
		completedOptions: &completedOptions{
			Logs:         options.Logs,
			ExtraOptions: options.ExtraOptions,
		},
	}

	return co, nil
}

func (options *CompletedOptions) Validate() error {
	if options.SchemaHandler == "grpc" && options.GRPCListenerAddress == "" {
		return errors.New("--grpc-listener-address must be set when --schema-handler=grpc")
	}

	if options.SchemaHandler == "grpc" && options.GRPCMaxRecvMsgSize <= 0 {
		return errors.New("--grpc-max-recv-msg-size must be a positive value")
	}

	if options.SchemaHandler == "file" && options.SchemasDir == "" {
		return errors.New("--schemas-dir must be set when --schema-handler=file")
	}

	if options.TokenReviewCacheTTL < 0 {
		return errors.New("--token-review-cache-ttl must not be negative")
	}

	if options.RequestTimeout < 0 {
		return errors.New("--request-timeout must not be negative")
	}

	if options.SubscriptionTimeout < 0 {
		return errors.New("--subscription-timeout must not be negative")
	}

	if options.MaxRequestBodyBytes < 0 {
		return errors.New("--max-request-body-bytes must not be negative")
	}

	if options.MaxInFlightRequests < 0 {
		return errors.New("--max-inflight-requests must not be negative")
	}

	if options.MaxInFlightSubscriptions < 0 {
		return errors.New("--max-inflight-subscriptions must not be negative")
	}

	if options.MaxQueryDepth < 0 {
		return errors.New("--max-query-depth must not be negative")
	}

	if options.MaxQueryComplexity < 0 {
		return errors.New("--max-query-complexity must not be negative")
	}

	if options.MaxQueryBatchSize < 0 {
		return errors.New("--max-query-batch-size must not be negative")
	}

	if options.ReadHeaderTimeout < 0 {
		return errors.New("--read-header-timeout must not be negative")
	}

	if options.IdleTimeout < 0 {
		return errors.New("--idle-timeout must not be negative")
	}

	return nil
}
