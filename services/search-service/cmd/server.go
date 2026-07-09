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

package cmd

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pmcontext "go.platform-mesh.io/golang-commons/context"
	gerrors "go.platform-mesh.io/golang-commons/errors"
	cmw "go.platform-mesh.io/golang-commons/middleware"
	fgaclient "go.platform-mesh.io/search-service/internal/clients/fga"
	"go.platform-mesh.io/search-service/internal/clients/kcp"
	osclient "go.platform-mesh.io/search-service/internal/clients/opensearch"
	lmw "go.platform-mesh.io/search-service/internal/middleware"
	"go.platform-mesh.io/search-service/internal/observability"
	"go.platform-mesh.io/search-service/internal/router"
	searchservice "go.platform-mesh.io/search-service/internal/service/search"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var serverCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start serving",
	Long:  `Start the Search Service as a Webservice`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, _, shutdown := pmcontext.StartContext(log, serviceCfg, defaultCfg.ShutdownTimeout)
		defer shutdown()

		restCfg, err := loadRestConfig(defaultCfg.Kubeconfig)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to load kube config")
		}

		orgValidator, err := kcp.NewOrgAccessValidator(restCfg, log)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to create org access validator")
		}

		searchIndexResolver, err := kcp.NewSearchIndexResolver(restCfg, serviceCfg.SearchIndex, log)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to create SearchIndex resolver")
		}

		openSearchClient, err := osclient.NewClient(osclient.Config{
			URL:      serviceCfg.OpenSearch.URL,
			Username: serviceCfg.OpenSearch.Username,
			Password: serviceCfg.OpenSearch.Password,
			Insecure: serviceCfg.OpenSearch.Insecure,
			Timeout:  serviceCfg.OpenSearch.Timeout,
		})
		if err != nil {
			log.Fatal().Err(err).Msg("failed to create OpenSearch client")
		}

		fgaClient, err := setupFGAClient(serviceCfg.OpenFGA.GRPCAddr)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to create OpenFGA client")
		}
		log.Info().
			Str("openSearchURL", serviceCfg.OpenSearch.URL).
			Str("openFGAGRPCAddr", serviceCfg.OpenFGA.GRPCAddr).
			Str("searchIndexWorkspacePath", serviceCfg.SearchIndex.WorkspacePath).
			Str("searchIndexOrgWorkspacePath", serviceCfg.SearchIndex.OrgWorkspacePath).
			Str("searchIndexGVR", fmt.Sprintf("%s/%s/%s", serviceCfg.SearchIndex.Group, serviceCfg.SearchIndex.Version, serviceCfg.SearchIndex.Resource)).
			Msg("search service backend configuration")

		metrics := observability.NewMetrics()
		svc := searchservice.NewService(
			searchIndexResolver,
			openSearchClient,
			fgaclient.NewAuthorizer(fgaClient),
			metrics,
			searchservice.ServiceConfig{
				DefaultLimit:   serviceCfg.Search.DefaultLimit,
				MaxLimit:       serviceCfg.Search.MaxLimit,
				FetchBatchSize: serviceCfg.Search.FetchBatchSize,
				MaxScannedHits: serviceCfg.Search.MaxScannedHits,
			},
		)

		mws := cmw.CreateMiddleware(log, true)
		orgCtxMW := lmw.NewOrgContextMiddleware(orgValidator, defaultCfg.IsLocal, serviceCfg.LocalDevelopmentOrg)
		mws = append(mws, orgCtxMW.SetRequestContext())

		r := router.CreateRouter(svc, mws)
		startHTTPServer(ctx, r)
	},
}

func loadRestConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	cfg, err := rest.InClusterConfig()
	if err == nil {
		return cfg, nil
	}
	return nil, fmt.Errorf("unable to load in-cluster config and no kubeconfig provided: %w", err)
}

func setupFGAClient(addr string) (openfgav1.OpenFGAServiceClient, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return nil, err
	}
	return openfgav1.NewOpenFGAServiceClient(conn), nil
}

func startHTTPServer(ctx context.Context, handler http.Handler) {
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", serviceCfg.Port),
		Handler:      handler,
		ReadTimeout:  20 * time.Second,
		WriteTimeout: 20 * time.Second,
		BaseContext:  func(listener net.Listener) context.Context { return ctx },
	}

	go func() {
		err := server.ListenAndServe()
		if err != nil && !gerrors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("failed to start http server")
		}
	}()

	log.Info().Msgf("service started on port: %d", serviceCfg.Port)
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Panic().Err(err).Msg("graceful shutdown failed")
	}
}
