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
// Package app wires iam-service together. It is the library entrypoint of the
// component: Run owns no process-global state (signal handlers, logging setup,
// os.Exit), so that it can be embedded into another process next to other
// components.
package app

import (
	"context"
	"crypto/tls"
	stderrors "errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmkeycloakv1alpha1 "go.platform-mesh.io/apis/keycloak/v1alpha1"
	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	pmconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/kcpconfig"
	"go.platform-mesh.io/golang-commons/logger"
	pmmws "go.platform-mesh.io/golang-commons/middleware"
	"go.platform-mesh.io/iam-service/input"
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

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	mcclient "github.com/kcp-dev/multicluster-provider/client"
	pathaware "github.com/kcp-dev/multicluster-provider/path-aware"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	kcpclientset "github.com/kcp-dev/sdk/client/clientset/versioned/cluster"
)

const (
	// CoreAPIExportEndpointSliceName is the APIExportEndpointSlice the service
	// uses to reach the workspaces of accounts.
	CoreAPIExportEndpointSliceName = "core.platform-mesh.io"
	// ProvidersAPIExportEndpointSliceName is the APIExportEndpointSlice the
	// service reads ProviderPermissions from, if that role retriever is enabled.
	ProvidersAPIExportEndpointSliceName = "providers.platform-mesh.io"

	httpShutdownTimeout = 5 * time.Second
)

// Config is the configuration of the service.
type Config struct {
	// Common is the configuration shared by all Platform Mesh services. Run
	// uses Metrics, HealthProbeBindAddress, EnableHTTP2 and IsLocal of it.
	Common *pmconfig.CommonServiceConfig
	// Service is the configuration of iam-service.
	Service *config.ServiceConfig
}

// Deps are the dependencies a caller may inject into Run. Only Logger is
// required, everything else is created by Run if it is not set.
type Deps struct {
	// Logger is the logger of the service.
	Logger *logger.Logger

	// OpenFGA is the OpenFGA client. If nil, Run dials
	// Config.Service.OpenFGA.GRPCAddr (insecure gRPC) and closes the connection
	// on return.
	OpenFGA openfgav1.OpenFGAServiceClient

	// UserDirectory overrides the user directory selected by
	// Config.Service.IDM.Provider.
	UserDirectory idm.UserDirectory

	// RolesRetriever overrides the role retriever selected by
	// Config.Service.Roles.
	RolesRetriever roles.RolesRetriever
}

// NewScheme returns the scheme the service's manager and clients use.
func NewScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(pmcorev1alpha1.AddToScheme(scheme))
	utilruntime.Must(pmkeycloakv1alpha1.AddToScheme(scheme))
	utilruntime.Must(pmprovidersv1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(scheme))
	return scheme
}

// Run starts iam-service and blocks until ctx is done or a component fails.
// When ctx is done the HTTP server is shut down gracefully and Run returns nil.
//
// Run does not install signal handlers, does not configure logging and never
// exits the process.
func Run(ctx context.Context, kcpCfg *rest.Config, cfg Config, deps Deps) error {
	switch {
	case kcpCfg == nil:
		return stderrors.New("kcp rest config is required")
	case cfg.Common == nil || cfg.Service == nil:
		return stderrors.New("common and service config are required")
	case deps.Logger == nil:
		return stderrors.New("logger is required")
	}

	log := deps.Logger
	serviceCfg := cfg.Service
	scheme := NewScheme()

	ctx = logger.SetLoggerInContext(ctx, log)
	ctx = pmconfig.SetConfigInContext(ctx, serviceCfg)

	restCfg, err := kcpconfig.ForWorkspace(kcpCfg, serviceCfg.KCPWorkspace)
	if err != nil {
		return err
	}
	restCfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return otelhttp.NewTransport(rt)
	})

	rootCfg, err := kcpconfig.Root(restCfg)
	if err != nil {
		return fmt.Errorf("failed to get root config: %w", err)
	}

	// Everything that does not need a running component is set up first, so
	// that configuration errors are returned before anything is started.
	mgr, err := newManager(ctx, restCfg, scheme, cfg.Common, log)
	if err != nil {
		return err
	}

	fgaClient := deps.OpenFGA
	if fgaClient == nil {
		fgaConn, err := grpc.NewClient(serviceCfg.OpenFGA.GRPCAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		)
		if err != nil {
			return fmt.Errorf("failed to create grpc client to OpenFGA: %w", err)
		}
		defer fgaConn.Close() //nolint:errcheck

		fgaClient = openfgav1.NewOpenFGAServiceClient(fgaConn)
	}

	rolesRetriever := deps.RolesRetriever
	var runRolesRetriever func(context.Context) error
	if rolesRetriever == nil {
		rolesRetriever, runRolesRetriever, err = newRolesRetriever(restCfg, scheme, serviceCfg, log)
		if err != nil {
			return err
		}
	}

	userDirectory, tenantRetriever, err := newIDM(ctx, serviceCfg, fgaClient, deps.UserDirectory, log)
	if err != nil {
		return err
	}

	router, err := newRouter(mgr, rootCfg, scheme, cfg, fgaClient, userDirectory, tenantRetriever, rolesRetriever, log)
	if err != nil {
		return err
	}

	addr := serviceCfg.ListenAddress
	if addr == "" {
		addr = fmt.Sprintf(":%d", serviceCfg.Port)
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	server := &http.Server{
		Handler:      router,
		ReadTimeout:  20 * time.Second,
		WriteTimeout: 20 * time.Second,
		BaseContext:  func(net.Listener) context.Context { return ctx },
	}

	g, gctx := errgroup.WithContext(ctx)

	if runRolesRetriever != nil {
		g.Go(func() error { return runRolesRetriever(gctx) })
	}

	g.Go(func() error {
		log.Info().Msg("starting manager")
		if err := mgr.Start(gctx); err != nil {
			return fmt.Errorf("problem running manager: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		log.Info().Msgf("service started on: %s", listener.Addr())
		if cfg.Common.IsLocal {
			log.Info().Msgf("connect to http://%s/ for graphQL playground", listener.Addr())
		}
		if err := server.Serve(listener); err != nil && !stderrors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("problem running http server: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		<-gctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), httpShutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown failed: %w", err)
		}
		return nil
	})

	return g.Wait()
}

func newManager(ctx context.Context, restCfg *rest.Config, scheme *runtime.Scheme, commonCfg *pmconfig.CommonServiceConfig, log *logger.Logger) (mcmanager.Manager, error) {
	provider, err := pathaware.New(restCfg, CoreAPIExportEndpointSliceName, apiexport.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("unable to construct APIExport provider: %w", err)
	}

	var tlsOpts []func(*tls.Config)
	if !commonCfg.EnableHTTP2 {
		tlsOpts = append(tlsOpts, func(c *tls.Config) {
			log.Info().Msg("disabling http/2")
			c.NextProtos = []string{"http/1.1"}
		})
	}

	mgr, err := mcmanager.New(restCfg, provider, mcmanager.Options{
		Scheme: scheme,
		Logger: log.Logr(),
		Metrics: metricsserver.Options{
			BindAddress:   commonCfg.Metrics.BindAddress,
			SecureServing: commonCfg.Metrics.Secure,
			TLSOpts:       tlsOpts,
		},

		BaseContext:            func() context.Context { return ctx },
		HealthProbeBindAddress: commonCfg.HealthProbeBindAddress,
		LeaderElection:         false,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to set up manager: %w", err)
	}

	return mgr, nil
}

// newRolesRetriever returns the role retriever selected by the configuration
// and, if the retriever needs one, a function that runs it until its context
// is done.
func newRolesRetriever(restCfg *rest.Config, scheme *runtime.Scheme, serviceCfg *config.ServiceConfig, log *logger.Logger) (roles.RolesRetriever, func(context.Context) error, error) {
	switch {
	case serviceCfg.Roles.UseProviderPermissionsRetriever:
		log.Info().Msg("Using ProviderPermissions-based role retriever")
		provider, err := pathaware.New(restCfg, ProvidersAPIExportEndpointSliceName, apiexport.Options{Scheme: scheme})
		if err != nil {
			return nil, nil, fmt.Errorf("unable to construct APIExport provider for %s: %w", ProvidersAPIExportEndpointSliceName, err)
		}
		ppCache := roles.NewProviderPermissionsCache(log)
		if err := ppCache.SetupWithProvider(provider.Provider.Provider); err != nil {
			return nil, nil, fmt.Errorf("failed to setup provider permissions cache: %w", err)
		}
		run := func(ctx context.Context) error {
			if err := provider.Start(ctx, nil); err != nil {
				return fmt.Errorf("problem running APIExport provider for %s: %w", ProvidersAPIExportEndpointSliceName, err)
			}
			return nil
		}
		return roles.NewProviderPermissionsRetriever(ppCache), run, nil
	case serviceCfg.Roles.FilePath == "":
		log.Info().Msg("Using built-in default roles")
		retriever, err := roles.NewRolesRetrieverFromYAML(input.DefaultRoles)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load built-in default roles: %w", err)
		}
		return retriever, nil, nil
	default:
		log.Info().Str("path", serviceCfg.Roles.FilePath).Msg("Using file-based role retriever")
		retriever, err := roles.NewFileBasedRolesRetriever(serviceCfg.Roles.FilePath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create file-based roles retriever for %s: %w", serviceCfg.Roles.FilePath, err)
		}
		return retriever, nil, nil
	}
}

// newIDM returns the user directory and the tenant retriever of the configured
// identity provider.
func newIDM(ctx context.Context, serviceCfg *config.ServiceConfig, fgaClient openfgav1.OpenFGAServiceClient, override idm.UserDirectory, log *logger.Logger) (idm.UserDirectory, idmmw.IDMTenantRetriever, error) {
	switch serviceCfg.IDM.Provider {
	case idm.ProviderKeycloak, "":
		if override != nil {
			return override, keycloakmw.New(), nil
		}
		// Keycloak has to be reachable at this point: the OIDC discovery of
		// its master realm is done here.
		userDirectory, err := keycloak.New(ctx, serviceCfg)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create keycloak client: %w", err)
		}
		return userDirectory, keycloakmw.New(), nil
	case idm.ProviderNone:
		log.Info().Msg("Running without an identity provider directory, users are known by their email address only")
		if override != nil {
			return override, none.TenantRetriever{}, nil
		}
		return none.New(fgaClient, store.NewFGAStoreHelper(serviceCfg.OpenFGA.StoreCacheTTL)), none.TenantRetriever{}, nil
	default:
		return nil, nil, fmt.Errorf("unknown idm provider %q, must be %q or %q", serviceCfg.IDM.Provider, idm.ProviderKeycloak, idm.ProviderNone)
	}
}

func newRouter(
	mgr mcmanager.Manager,
	rootCfg *rest.Config,
	scheme *runtime.Scheme,
	cfg Config,
	fgaClient openfgav1.OpenFGAServiceClient,
	userDirectory idm.UserDirectory,
	tenantRetriever idmmw.IDMTenantRetriever,
	rolesRetriever roles.RolesRetriever,
	log *logger.Logger,
) (*chi.Mux, error) {
	serviceCfg := cfg.Service

	clusterClient, err := kcpclientset.NewForConfig(rootCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster client: %w", err)
	}

	mws := pmmws.CreateMiddleware(log, true)
	kcpmw := kcpmiddleware.New(mgr.GetLocalManager().GetConfig(), serviceCfg.IDM.ExcludedTenants, tenantRetriever, log)
	mws = append(mws, kcpmw.SetKCPUserContext())

	// Prepare AccountInfo Retriever
	accountInfoRetriever, err := accountinfo.New(mgr, clusterClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create account info retriever: %w", err)
	}

	// Prepare Directives
	mcClusterClient, err := mcclient.New(rootCfg, ctrlruntimeclient.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create multicluster client: %w", err)
	}
	wsClient := workspace.NewClusterClientFactory(mcClusterClient)
	ad := directive.NewAuthorizedDirective(
		accountInfoRetriever,
		wsClient,
		rootCfg,
		log,
	)
	dr := graph.DirectiveRoot{
		Authorized: ad.Authorized,
	}

	// create Resolver Service
	svc := pm.NewResolverService(fgaClient, userDirectory, serviceCfg, mgr, rolesRetriever)
	res := resolver.New(svc, log.ComponentLogger("resolver"))

	return iamRouter.CreateRouter(cfg.Common, serviceCfg, res, log, mws, dr), nil
}
