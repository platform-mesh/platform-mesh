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

package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/cors"

	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/middleware"
	utilscontext "go.platform-mesh.io/kubernetes-graphql-gateway/gateway/utils/context"

	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type ServerConfig struct {
	Gateway http.Handler

	// HealthzCheck and ReadyzCheck are optional controller-runtime healthz.Checker
	// functions. When set, they are used by /healthz and /readyz respectively.
	// When nil, the endpoints always return 200.
	HealthzCheck healthz.Checker
	ReadyzCheck  healthz.Checker

	CORSConfig CORSConfig

	PlaygroundEnabled        bool
	MaxRequestBodyBytes      int64
	MaxInFlightRequests      int
	MaxInFlightSubscriptions int
	RequestTimeout           time.Duration
	SubscriptionTimeout      time.Duration
	ReadHeaderTimeout        time.Duration
	IdleTimeout              time.Duration

	// SubscriptionMetrics provides optional Prometheus instrumentation for
	// the subscription concurrency limiter. When nil, no metrics are recorded.
	SubscriptionMetrics *middleware.InFlightMetrics

	Addr           string
	EndpointSuffix string
}

type CORSConfig struct {
	AllowedOrigins   []string
	AllowedHeaders   []string
	AllowCredentials bool
}

type Server struct {
	Server *http.Server
}

// NewServer creates a new HTTP server with the provided configuration
// It main server, used to serve the GraphQL API, health checks, and metrics
func NewServer(c ServerConfig) (*Server, error) {
	s := http.NewServeMux()

	queryHandler := middleware.WithMaxInFlightRequests(middleware.WithTimeout(c.Gateway, c.RequestTimeout), c.MaxInFlightRequests, nil)
	subscriptionHandler := middleware.WithMaxInFlightRequests(middleware.WithTimeout(c.Gateway, c.SubscriptionTimeout), c.MaxInFlightSubscriptions, c.SubscriptionMetrics)

	s.Handle(fmt.Sprintf("/api/clusters/{clusterName}%s", c.EndpointSuffix), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c.MaxRequestBodyBytes > 0 {
			r.Body = http.MaxBytesReader(w, r.Body, c.MaxRequestBodyBytes)
		}

		clusterName := r.PathValue("clusterName")

		// Allow unauthenticated GET requests through to the playground handler.
		if c.PlaygroundEnabled && r.Method == http.MethodGet {
			ctx := utilscontext.SetCluster(r.Context(), clusterName)
			queryHandler.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Unauthorized: missing Authorization header", http.StatusUnauthorized)
			return
		}
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "Unauthorized: invalid Authorization header format", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == "" {
			http.Error(w, "Unauthorized: empty bearer token", http.StatusUnauthorized)
			return
		}

		ctx := utilscontext.SetToken(r.Context(), token)
		ctx = utilscontext.SetCluster(ctx, clusterName)

		// Route to separate timeout/concurrency pools; the endpoint layer
		// checks this header again to pick the GraphQL execution path.
		if r.Header.Get("Accept") == "text/event-stream" {
			subscriptionHandler.ServeHTTP(w, r.WithContext(ctx))
		} else {
			queryHandler.ServeHTTP(w, r.WithContext(ctx))
		}
	}))

	// TODO: Add middleware for logging, metrics, tracing, etc.

	// Health and metrics endpoints
	s.Handle("/healthz", healthz.CheckHandler{Checker: healthz.Ping})
	s.Handle("/readyz", healthz.CheckHandler{Checker: checkerOrPing(c.ReadyzCheck)})
	s.Handle("/metrics", promhttp.Handler())

	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   c.CORSConfig.AllowedOrigins,
		AllowedHeaders:   c.CORSConfig.AllowedHeaders,
		AllowCredentials: c.CORSConfig.AllowCredentials,
	})

	return &Server{
		Server: &http.Server{
			Handler:           corsHandler.Handler(s),
			Addr:              c.Addr,
			ReadHeaderTimeout: c.ReadHeaderTimeout,
			IdleTimeout:       c.IdleTimeout,
		},
	}, nil
}

// checkerOrPing returns the given checker if non-nil, otherwise healthz.Ping (always healthy).
func checkerOrPing(c healthz.Checker) healthz.Checker {
	if c != nil {
		return c
	}
	return healthz.Ping
}

func (s *Server) Run(ctx context.Context) error {
	logger := log.FromContext(ctx)

	logger.WithValues("addr", s.Server.Addr).Info("Starting HTTP server")

	// Gracefully shut down the HTTP server when the context is cancelled
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Server.Shutdown(shutdownCtx); err != nil {
			logger.Error(err, "HTTP server shutdown error")
		}
	}()

	if err := s.Server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
