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

// Binary platform-mesh-access-server exposes the user-side /access
// HTTP endpoints: the unauthenticated /versions probe and one
// /vN/access handler per supported handshake version (currently
// /v1/access). The AccessIdentity reconciler lives in a separate
// workload (platform-mesh-access-controller).
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"golang.org/x/net/netutil"

	"go.platform-mesh.io/golang-commons/logger"
	pmmws "go.platform-mesh.io/golang-commons/middleware"
	pmaccessproto "go.platform-mesh.io/protocol/access"
	accessv1alpha1 "go.platform-mesh.io/sdk/access/apis/v1alpha1"
	accessserver "go.platform-mesh.io/sdk/access/pkg/server"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// Bounds the replay cache. Call-credential JWTs live ~90s (60s TTL
// plus skew tolerance); well-behaved deployments see one entry per
// handshake per Identity. 1000 is generous for mid-size operations.
const replayCacheSize = 1000

// Startup grace for cache sync before the /access listener begins
// accepting traffic.
const cacheSyncTimeout = 2 * time.Minute

func main() {
	var (
		pathPrefix   string
		tlsCertFile  string
		tlsKeyFile   string
		accessListen string
		metricsAddr  string
		probeAddr    string
		logLevel     string
		logNoJSON    bool
		enableHTTP2  bool
	)

	pflag.StringVar(&pathPrefix, "path-prefix", "", "URL path prefix to mount /versions and /vN/access under. Leave empty for root. Use when fronting the SDK with an ingress that preserves a prefix (e.g. \"/platform-mesh\").")
	pflag.StringVar(&tlsCertFile, "tls-cert-file", "", "Path to PEM-encoded TLS cert for /access. Leave empty to serve plain HTTP (expects TLS termination upstream).")
	pflag.StringVar(&tlsKeyFile, "tls-key-file", "", "Path to PEM-encoded TLS key for /access. Must be set together with --tls-cert-file.")
	pflag.StringVar(&accessListen, "access-listen", ":8443", "Listen address for the /access server.")
	pflag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "Metrics listen address.")
	pflag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "Health probe listen address.")
	pflag.StringVar(&logLevel, "log-level", "info", "Set the log level")
	pflag.BoolVar(&logNoJSON, "no-json", false, "Disable JSON logging")
	pflag.BoolVar(&enableHTTP2, "enable-http2", false, "Enable HTTP/2 for the /access server. Disabled by default to reduce the attack surface (CVE-2023-44487 et al.).")
	pflag.Parse()

	logcfg := logger.DefaultConfig()
	logcfg.Name = "access-server"
	logcfg.Level = logLevel
	logcfg.NoJSON = logNoJSON
	log, err := logger.New(logcfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to build logger: %v\n", err)
		os.Exit(1)
	}
	ctrl.SetLogger(log.Logr())
	setupLog := ctrl.Log.WithName("setup")

	if (tlsCertFile == "") != (tlsKeyFile == "") {
		setupLog.Error(fmt.Errorf("flag combination invalid"), "--tls-cert-file and --tls-key-file must be set together, or both left empty")
		os.Exit(1)
	}
	tlsEnabled := tlsCertFile != ""

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(accessv1alpha1.AddToScheme(scheme))

	// Manager provides the shared cache, field indexer, and health /
	// metrics endpoints. It runs no reconcilers.
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         false,
	})
	if err != nil {
		setupLog.Error(err, "unable to build manager")
		os.Exit(1)
	}

	v1Handler := &accessserver.Handler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		ReplayCache: pmaccessproto.NewReplayCache(replayCacheSize),
	}

	// Normalize --path-prefix: trim trailing slash so callers can
	// concatenate "/versions" and "/v1/access" directly. Empty = root.
	pathPrefix = strings.TrimRight(pathPrefix, "/")

	// Mux routes: unauthenticated /versions probe + one /vN/access
	// handler per supported handshake version. When adding v2, mount
	// the v2 handler here and extend pmaccessproto.SupportedVersions.
	mux := http.NewServeMux()
	mux.Handle(pathPrefix+pmaccessproto.VersionsPath, accessserver.VersionsHandler())
	mux.Handle(pathPrefix+pmaccessproto.AccessPath(pmaccessproto.V1), v1Handler)

	// Shared middleware chain: OTel tracing, Sentry panic recovery,
	// request-scoped logger, request-id. auth=false because we do our
	// own Bearer-JWT auth in the /access handler.
	var handler http.Handler = mux
	for _, mw := range pmmws.CreateMiddleware(log, false) {
		handler = mw(handler)
	}

	if err := mgr.Add(&accessHTTPRunnable{
		listen:      accessListen,
		tlsEnabled:  tlsEnabled,
		certFile:    tlsCertFile,
		keyFile:     tlsKeyFile,
		enableHTTP2: enableHTTP2,
		handler:     handler,
		cache:       mgr.GetCache(),
	}); err != nil {
		setupLog.Error(err, "unable to add /access server runnable")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up healthz")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up readyz")
		os.Exit(1)
	}

	setupLog.Info("starting access server", "listen", accessListen, "pathPrefix", pathPrefix)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "manager stopped with error")
		os.Exit(1)
	}
}

// accessHTTPRunnable is a manager.Runnable that serves the /versions
// probe and the versioned /vN/access handlers. It waits for the shared
// cache to sync before accepting traffic so handler List/Get calls hit
// a populated cache. When tlsEnabled is false the server runs plain
// HTTP (for setups where TLS terminates at an upstream gateway).
type accessHTTPRunnable struct {
	listen      string
	tlsEnabled  bool
	certFile    string
	keyFile     string
	enableHTTP2 bool
	handler     http.Handler
	cache       cacheSyncer
}

// cacheSyncer narrows cache.Cache to the one method we need, so tests
// can swap it.
type cacheSyncer interface {
	WaitForCacheSync(ctx context.Context) bool
}

// Start implements manager.Runnable.
func (r *accessHTTPRunnable) Start(ctx context.Context) error {
	syncCtx, cancel := context.WithTimeout(ctx, cacheSyncTimeout)
	defer cancel()
	if !r.cache.WaitForCacheSync(syncCtx) {
		return fmt.Errorf("/access server: cache failed to sync within %s", cacheSyncTimeout)
	}

	// Transport-level limits for a public endpoint. Bounds slowloris
	// and goroutine/fd exhaustion; volumetric DDoS still needs edge
	// scrubbing (CDN/LB) in front.
	srv := &http.Server{
		Addr:              r.listen,
		Handler:           r.handler,
		BaseContext:       func(_ net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if r.tlsEnabled {
		srv.TLSConfig = &tls.Config{}
		if !r.enableHTTP2 {
			srv.TLSConfig.NextProtos = []string{"http/1.1"}
		}
	}

	ln, err := net.Listen("tcp", r.listen)
	if err != nil {
		return fmt.Errorf("listen %s: %w", r.listen, err)
	}
	ln = netutil.LimitListener(ln, 256)

	errCh := make(chan error, 1)
	go func() {
		if r.tlsEnabled {
			errCh <- srv.ServeTLS(ln, r.certFile, r.keyFile)
		} else {
			errCh <- srv.Serve(ln)
		}
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

var _ manager.Runnable = (*accessHTTPRunnable)(nil)
