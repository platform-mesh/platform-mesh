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

package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	pmgatewayv1alpha1 "go.platform-mesh.io/apis/gateway/v1alpha1"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/authn"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/cluster"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/config"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/graphql"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/metrics"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/queryvalidation"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/requestparser"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/resolver"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/schema"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/schema/extensions"
	utilscontext "go.platform-mesh.io/kubernetes-graphql-gateway/gateway/utils/context"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Endpoint combines a cluster connection with its GraphQL handler.
type Endpoint struct {
	name          string
	cluster       *cluster.Cluster
	graphqlServer *graphql.GraphQLServer
	handler       http.Handler
	cancelFunc    context.CancelFunc
	metrics       *metrics.EndpointMetrics
}

func New(
	ctx context.Context,
	name string,
	schemaJSON []byte,
	graphqlCfg config.GraphQL,
	limits config.Limits,
	tokenReviewCacheTTL time.Duration,
	injectedValidator authn.Validator,
	m *metrics.Collector,
) (*Endpoint, error) {
	var endpointM *metrics.EndpointMetrics
	var resolverM *metrics.ResolverMetrics
	var authM *metrics.AuthMetrics
	if m != nil {
		endpointM = m.Endpoint
		resolverM = m.Resolver
		authM = m.Auth
	}

	schemaData, err := parseSchema(schemaJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema: %w", err)
	}

	cl, err := cluster.New(ctx, name, schemaData.ClusterMetadata)
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster: %w", err)
	}

	// When the caller injects a Validator, they own its lifecycle. Otherwise
	// we build a per-endpoint TokenReviewValidator and own it ourselves.
	validator := injectedValidator
	validatorCancel := context.CancelFunc(func() {})
	if validator == nil {
		validatorCtx, trCancel := context.WithCancel(ctx)
		tr, err := authn.NewTokenReviewValidator(cl.AdminConfig(), tokenReviewCacheTTL, authM)
		if err != nil {
			trCancel()
			return nil, fmt.Errorf("failed to create token validator: %w", err)
		}
		go tr.Start(validatorCtx)
		validator = tr
		validatorCancel = trCancel
	}

	resolverProvider := resolver.New(cl.Client(), resolverM)

	customSubGen, err := extensions.NewCustomSubscriptionGenerator(cl.RestConfig())
	if err != nil {
		validatorCancel()
		return nil, fmt.Errorf("failed to create custom subscription generator: %w", err)
	}

	schemaProvider, err := schema.New(ctx, schemaData.Components.Schemas, resolverProvider, customSubGen)
	if err != nil {
		validatorCancel()
		return nil, fmt.Errorf("failed to create GraphQL schema: %w", err)
	}

	hasPathTemplate := schemaData.ClusterMetadata != nil && schemaData.ClusterMetadata.RequestPathTemplate != ""

	graphqlServer := graphql.NewGraphQLServer(graphqlCfg)
	gqlHandler := graphqlServer.CreateHandler(schemaProvider.GetSchema())

	gqlHTTPHandler := queryvalidation.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") == "text/event-stream" {
			graphqlServer.HandleSubscription(w, r, gqlHandler.Schema)
			return
		}
		gqlHandler.Handler.ServeHTTP(w, r)
	}), queryvalidation.Config{
		MaxDepth:      limits.MaxQueryDepth,
		MaxComplexity: limits.MaxQueryComplexity,
		MaxBatchSize:  limits.MaxQueryBatchSize,
	})

	// Middleware chain (outermost runs first):
	//   requestparser → clusterTarget extraction → auth → queryvalidation → graphql handler
	handler := requestparser.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hasPathTemplate {
			if reqs, ok := utilscontext.GetParsedRequestsFromCtx(r.Context()); ok {
				if target := utilscontext.FindClusterTarget(reqs); target != "" {
					r = r.WithContext(utilscontext.SetClusterTarget(r.Context(), target))
				}
			}
		}

		// Allow unauthenticated GET requests through when playground is enabled.
		if graphqlCfg.PlaygroundEnabled && r.Method == http.MethodGet {
			gqlHTTPHandler.ServeHTTP(w, r)
			return
		}

		token, ok := utilscontext.GetTokenFromCtx(r.Context())
		if !ok || token == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		authenticated, err := validator.Validate(r.Context(), token)
		if err != nil {
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			return
		}
		if !authenticated {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		gqlHTTPHandler.ServeHTTP(w, r)
	}))
	log.FromContext(ctx).Info("Registered endpoint", "cluster", name)

	return &Endpoint{
		name:          name,
		cluster:       cl,
		graphqlServer: graphqlServer,
		handler:       handler,
		cancelFunc:    validatorCancel,
		metrics:       endpointM,
	}, nil
}

func (e *Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if e.handler == nil {
		http.Error(w, "Endpoint not ready", http.StatusServiceUnavailable)
		return
	}
	start := time.Now()
	operation := metrics.OperationQuery
	if r.Header.Get("Accept") == "text/event-stream" {
		operation = metrics.OperationSubscription
	}
	rw := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
	e.handler.ServeHTTP(rw, r)
	if e.metrics != nil {
		labelResult := metrics.ResultSuccess
		if rw.statusCode >= 400 {
			labelResult = metrics.ResultError
		}
		e.metrics.Record(e.name, operation, time.Since(start), labelResult)
	}
}

// statusResponseWriter wraps http.ResponseWriter to capture the HTTP status code.
// It also forwards Flush calls so that SSE streaming works correctly.
type statusResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *statusResponseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *statusResponseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (e *Endpoint) Name() string {
	return e.name
}

func (e *Endpoint) Close() {
	if e.cancelFunc != nil {
		e.cancelFunc()
	}
	if e.cluster != nil {
		e.cluster.Close()
	}
	e.handler = nil
	e.graphqlServer = nil
}

func parseSchema(schemaJSON []byte) (*pmgatewayv1alpha1.Schema, error) {
	var schemaData pmgatewayv1alpha1.Schema
	if err := json.Unmarshal(schemaJSON, &schemaData); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	return &schemaData, nil
}
