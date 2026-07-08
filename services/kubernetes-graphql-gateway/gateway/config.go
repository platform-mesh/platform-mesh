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

package gateway

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway"
	gatewayconfig "go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/config"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/metrics"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/middleware"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/http"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/options"
)

type Config struct {
	Options *options.CompletedOptions

	Gateway    *gateway.Service
	HTTPServer *http.Server
}

func NewConfig(opts *options.CompletedOptions) (*Config, error) {
	cfg := &Config{
		Options: opts,
	}

	gatewayServer, err := gateway.New(gatewayconfig.Gateway{
		SchemaHandler:      cfg.Options.SchemaHandler,
		SchemaDirectory:    cfg.Options.SchemasDir,
		GRPCAddress:        cfg.Options.GRPCListenerAddress,
		GRPCMaxRecvMsgSize: cfg.Options.GRPCMaxRecvMsgSize,
		GraphQL: gatewayconfig.GraphQL{
			Pretty:                     true,
			PlaygroundEnabled:          cfg.Options.PlaygroundEnabled,
			ResourcesByCategoryEnabled: cfg.Options.ResourcesByCategoryEnabled,
			GraphiQL:                   cfg.Options.PlaygroundEnabled,
		},
		Limits: gatewayconfig.Limits{
			MaxQueryDepth:      cfg.Options.MaxQueryDepth,
			MaxQueryComplexity: cfg.Options.MaxQueryComplexity,
			MaxQueryBatchSize:  cfg.Options.MaxQueryBatchSize,
		},
		TokenReviewCacheTTL: cfg.Options.TokenReviewCacheTTL,
		Metrics:             metrics.NewCollector(prometheus.DefaultRegisterer),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create gateway server: %w", err)
	}
	cfg.Gateway = gatewayServer

	subMetrics := metrics.NewSubscriptionMetrics(prometheus.DefaultRegisterer)

	httpServer, err := http.NewServer(http.ServerConfig{
		Gateway:                  gatewayServer,
		ReadyzCheck:              gatewayServer.IsReady,
		Addr:                     fmt.Sprintf("%s:%d", cfg.Options.ServerBindAddress, cfg.Options.ServerBindPort),
		PlaygroundEnabled:        cfg.Options.PlaygroundEnabled,
		MaxRequestBodyBytes:      cfg.Options.MaxRequestBodyBytes,
		MaxInFlightRequests:      cfg.Options.MaxInFlightRequests,
		MaxInFlightSubscriptions: cfg.Options.MaxInFlightSubscriptions,
		RequestTimeout:           cfg.Options.RequestTimeout,
		SubscriptionTimeout:      cfg.Options.SubscriptionTimeout,
		ReadHeaderTimeout:        cfg.Options.ReadHeaderTimeout,
		IdleTimeout:              cfg.Options.IdleTimeout,
		EndpointSuffix:           cfg.Options.EndpointSuffix,
		SubscriptionMetrics: &middleware.InFlightMetrics{
			Active:   subMetrics.Active,
			Total:    subMetrics.Total,
			Rejected: subMetrics.Rejected,
		},
		CORSConfig: http.CORSConfig{
			AllowedOrigins:   cfg.Options.CORSAllowedOrigins,
			AllowedHeaders:   cfg.Options.CORSAllowedHeaders,
			AllowCredentials: true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP server: %w", err)
	}

	cfg.HTTPServer = httpServer

	return cfg, nil
}
