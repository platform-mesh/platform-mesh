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
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pmcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/kcpconfig"
	"go.platform-mesh.io/golang-commons/logger"
	pmmws "go.platform-mesh.io/golang-commons/middleware"
	"go.platform-mesh.io/iam-service/pkg/accountinfo"
	"go.platform-mesh.io/iam-service/pkg/config"
	"go.platform-mesh.io/iam-service/pkg/directive"
	"go.platform-mesh.io/iam-service/pkg/fga/store"
	"go.platform-mesh.io/iam-service/pkg/graph"
	"go.platform-mesh.io/iam-service/pkg/idm"
	"go.platform-mesh.io/iam-service/pkg/idm/none"
	"go.platform-mesh.io/iam-service/pkg/keycloak"
	idmmw "go.platform-mesh.io/iam-service/pkg/middleware/idm"
	kcpmiddleware "go.platform-mesh.io/iam-service/pkg/middleware/kcp"
	keycloakmw "go.platform-mesh.io/iam-service/pkg/middleware/keycloak"
	"go.platform-mesh.io/iam-service/pkg/resolver"
	"go.platform-mesh.io/iam-service/pkg/resolver/pm"
	"go.platform-mesh.io/iam-service/pkg/roles"
	iamRouter "go.platform-mesh.io/iam-service/pkg/router"
	"go.platform-mesh.io/iam-service/pkg/workspace"

	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	mcclient "github.com/kcp-dev/multicluster-provider/client"
	pathaware "github.com/kcp-dev/multicluster-provider/path-aware"
	"github.com/kcp-dev/multicluster-provider/pkg/provider"
	kcpclientset "github.com/kcp-dev/sdk/client/clientset/versioned/cluster"
)

var serverCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start serving",
	Long:  `Start the IAM Service as a Webservice`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, _, shutdown := pmcontext.StartContext(log, serviceCfg, defaultCfg.ShutdownTimeout)
		defer shutdown()

		mgr := setupManager(ctx, log)
		router := setupRouter(ctx, mgr, setupFGAClient())
		start(ctx, serviceCfg, router, log, defaultCfg.IsLocal)
	},
}

// setupIDM returns the user directory and the tenant retriever of the
// identity provider selected with --idm-provider.
func setupIDM(ctx context.Context, fgaClient openfgav1.OpenFGAServiceClient) (idm.UserDirectory, idmmw.IDMTenantRetriever) {
	switch serviceCfg.IDM.Provider {
	case idm.ProviderKeycloak, "":
		// Keycloak has to be reachable at this point: the OIDC discovery of
		// its master realm is done here.
		userDirectory, err := keycloak.New(ctx, serviceCfg)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to create keycloak client")
		}
		return userDirectory, keycloakmw.New()
	case idm.ProviderNone:
		log.Info().Msg("Running without an identity provider directory, users are known by their email address only")
		return none.New(fgaClient, store.NewFGAStoreHelper(serviceCfg.OpenFGA.StoreCacheTTL)), none.TenantRetriever{}
	default:
		log.Fatal().Msgf("unknown idm provider %q, must be %q or %q", serviceCfg.IDM.Provider, idm.ProviderKeycloak, idm.ProviderNone)
		return nil, nil
	}
}

func setupRouter(ctx context.Context, mgr mcmanager.Manager, fgaClient openfgav1.OpenFGAServiceClient) *chi.Mux {
	restcfg, err := kcpconfig.Root(mgr.GetLocalManager().GetConfig())
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to get root config")
	}

	clusterClient, err := kcpclientset.NewForConfig(restcfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create cluster client")
	}

	mws := pmmws.CreateMiddleware(log, true)
	userDirectory, tenantRetriever := setupIDM(ctx, fgaClient)
	kcpmw := kcpmiddleware.New(mgr.GetLocalManager().GetConfig(), serviceCfg.IDM.ExcludedTenants, tenantRetriever, log)
	mws = append(mws, kcpmw.SetKCPUserContext())

	// Prepare AccountInfo Retriever
	accountInfoRetriever, err := accountinfo.New(mgr, clusterClient)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create account info retriever")
	}

	// Prepare Directives
	mcClusterClient, err := mcclient.New(restcfg, ctrlruntimeclient.Options{Scheme: scheme})
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create multicluster client")
	}
	wsClient := workspace.NewClusterClientFactory(mcClusterClient)
	ad := directive.NewAuthorizedDirective(
		accountInfoRetriever,
		wsClient,
		restcfg,
		log,
	)
	dr := graph.DirectiveRoot{
		Authorized: ad.Authorized,
	}

	// Setup role retriever based on feature flag
	var rolesRetriever roles.RolesRetriever
	if serviceCfg.Roles.UseProviderPermissionsRetriever {
		log.Info().Msg("Using ProviderPermissions-based role retriever")
		provider := setupProvider(ctx, log, "providers.platform-mesh.io")
		ppCache := roles.NewProviderPermissionsCache(log)
		if err := ppCache.SetupWithProvider(provider); err != nil {
			log.Fatal().Err(err).Msg("Failed to setup provider permissions cache")
		}
		rolesRetriever = roles.NewProviderPermissionsRetriever(ppCache)
	} else {
		log.Info().Str("path", serviceCfg.Roles.FilePath).Msg("Using file-based role retriever")
		rolesRetriever, err = roles.NewFileBasedRolesRetriever(serviceCfg.Roles.FilePath)
		if err != nil {
			log.Fatal().Err(err).Str("path", serviceCfg.Roles.FilePath).Msg("Failed to create file-based roles retriever")
		}
	}

	// create Resolver Service
	svc := pm.NewResolverService(fgaClient, userDirectory, serviceCfg, mgr, rolesRetriever)
	res := resolver.New(svc, log.ComponentLogger("resolver"))
	router := iamRouter.CreateRouter(defaultCfg, serviceCfg, res, log, mws, dr)
	return router
}

func setupFGAClient() openfgav1.OpenFGAServiceClient {
	fgaConn, err := grpc.NewClient(serviceCfg.OpenFGA.GRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to start grpc server")
	}

	fgaClient := openfgav1.NewOpenFGAServiceClient(fgaConn)
	return fgaClient
}

func setupProvider(ctx context.Context, log *logger.Logger, apiExportName string) *provider.Provider {
	restCfg := getKCPConfig()
	restCfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return otelhttp.NewTransport(rt)
	})

	provider, err := pathaware.New(restCfg, apiExportName, apiexport.Options{Scheme: scheme})
	if err != nil {
		log.Fatal().Err(err).Str("apiExport", apiExportName).Msg("unable to construct APIExport provider")
	}

	go func() {
		if err := provider.Start(ctx, nil); err != nil {
			log.Fatal().Err(err).Str("apiExport", apiExportName).Msg("problem running provider")
		}
	}()

	return provider.Provider.Provider
}

func setupManager(ctx context.Context, log *logger.Logger) mcmanager.Manager {
	ctrl.SetLogger(log.Logr())
	restCfg := getKCPConfig()
	restCfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return otelhttp.NewTransport(rt)
	})

	provider, err := pathaware.New(restCfg, "core.platform-mesh.io", apiexport.Options{Scheme: scheme})
	if err != nil {
		log.Fatal().Err(err).Msg("unable to construct APIExport provider")
	}

	var tlsOpts []func(*tls.Config)
	disableHTTP2 := func(c *tls.Config) {
		log.Info().Msg("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}
	if !defaultCfg.EnableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	mgr, err := mcmanager.New(restCfg, provider, mcmanager.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   defaultCfg.Metrics.BindAddress,
			SecureServing: defaultCfg.Metrics.Secure,
			TLSOpts:       tlsOpts,
		},

		BaseContext:            func() context.Context { return ctx },
		HealthProbeBindAddress: defaultCfg.HealthProbeBindAddress,
		LeaderElection:         false,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("unable to start manager")
	}

	log.Info().Msg("starting manager")
	go func() {
		if err := mgr.Start(ctx); err != nil {
			log.Fatal().Err(err).Msg("problem running manager")
		}
	}()

	return mgr
}

func start(ctx context.Context, serviceCfg *config.ServiceConfig, router *chi.Mux, log *logger.Logger, isLocal bool) {
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", serviceCfg.Port),
		Handler:      router,
		ReadTimeout:  20 * time.Second,
		WriteTimeout: 20 * time.Second,
		BaseContext:  func(listener net.Listener) context.Context { return ctx },
	}
	// Bind before going on: a port that cannot be bound must stop the
	// process, not leave it running without an HTTP server.
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to start http server")
	}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Msg("http server failed")
		}
	}()

	log.Info().Msgf("service started on port: %d", serviceCfg.Port)
	if isLocal {
		log.Info().Msgf("connect to http://localhost:%d/ for graphQL playground", serviceCfg.Port)
	}
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Panic().Err(err).Msg("Graceful shutdown failed")
	}
}

// getKCPConfig returns the kcp rest config, pointed at the workspace of
// --kcp-workspace if that is set.
func getKCPConfig() *rest.Config {
	restCfg, err := kcpconfig.ForWorkspace(ctrl.GetConfigOrDie(), serviceCfg.KCPWorkspace)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to point the kcp config at the workspace")
	}
	return restCfg
}
