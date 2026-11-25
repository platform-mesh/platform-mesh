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

// Package app wires the authorization webhook together. It is the library
// entrypoint of the component: Run owns no process-global state (signal
// handlers, logging setup, os.Exit), so that it can be embedded into another
// process next to other components.
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"go.platform-mesh.io/golang-commons/fga/store"
	"go.platform-mesh.io/golang-commons/kcpconfig"
	"go.platform-mesh.io/pm-openfga-authz-webhook/pkg/authorization"
	"go.platform-mesh.io/pm-openfga-authz-webhook/pkg/authorization/union"
	"go.platform-mesh.io/pm-openfga-authz-webhook/pkg/clustercache"
	"go.platform-mesh.io/pm-openfga-authz-webhook/pkg/config"
	"go.platform-mesh.io/pm-openfga-authz-webhook/pkg/handler/batch"
	"go.platform-mesh.io/pm-openfga-authz-webhook/pkg/handler/contextual"
	"go.platform-mesh.io/pm-openfga-authz-webhook/pkg/handler/nonresourceattributes"
	"go.platform-mesh.io/pm-openfga-authz-webhook/pkg/handler/orgs"
	"go.platform-mesh.io/pm-openfga-authz-webhook/pkg/retry"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	pathaware "github.com/kcp-dev/multicluster-provider/path-aware"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

const (
	// OrgsStoreName is the name of the OpenFGA store that holds the
	// permissions on the root:orgs workspace.
	OrgsStoreName = "orgs"

	// AuthzPath is the path the SubjectAccessReview webhook is served on.
	AuthzPath = "/authz"
	// BatchAuthzPath is the path the batch authorization endpoint is served on.
	BatchAuthzPath = "/batch-authz"

	storePollInterval = 2 * time.Second
)

// Deps are the dependencies a caller may inject into Run. The zero value is
// valid and makes Run create everything itself.
type Deps struct {
	// OpenFGA is the OpenFGA client to authorize against. If nil, Run dials
	// cfg.OpenFGAAddr (insecure gRPC) and closes the connection on return.
	OpenFGA openfgav1.OpenFGAServiceClient
}

// NewScheme returns the scheme the webhook's manager and cluster providers use.
func NewScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpcorev1alpha1.AddToScheme(scheme))
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	return scheme
}

// Run starts the authorization webhook and blocks until ctx is done or a
// component fails.
//
// kcpCfg is a kcp rest config; cfg.KCPWorkspace names the workspace in it that
// holds the APIExportEndpointSlices named by cfg.APIExportEndpointSliceName and
// cfg.StoreAPIExportEndpointSliceName (root:platform-mesh-system in a Platform
// Mesh installation; empty: the one kcpCfg points at). kcpCfg is copied, the
// caller's config is not modified.
//
// Run does not install signal handlers, does not configure logging and never
// exits the process.
func Run(ctx context.Context, kcpCfg *rest.Config, cfg *config.Config, deps Deps) error {
	if kcpCfg == nil {
		return errors.New("kcp rest config is required")
	}
	if cfg == nil {
		return errors.New("config is required")
	}

	log := klog.NewKlogr()
	scheme := NewScheme()

	restCfg, err := kcpconfig.ForWorkspace(kcpCfg, cfg.KCPWorkspace)
	if err != nil {
		return err
	}
	restCfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return otelhttp.NewTransport(rt)
	})

	klog.InfoS("using endpoint slice name", "name", cfg.APIExportEndpointSliceName)
	provider, err := pathaware.New(restCfg, cfg.APIExportEndpointSliceName, apiexport.Options{
		Scheme: scheme,
	})
	if err != nil {
		return fmt.Errorf("unable to construct cluster provider: %w", err)
	}

	// Use Root kcp config for manager
	mgr, err := mcmanager.New(restCfg, provider, mcmanager.Options{
		Scheme: scheme,
		Logger: log,
		WebhookServer: webhook.NewServer(webhook.Options{
			Host:    cfg.Webhook.Host,
			Port:    cfg.Webhook.Port,
			CertDir: cfg.Webhook.CertDir,
		}),
		Metrics: metricsserver.Options{
			BindAddress: cfg.MetricsBindAddress,
			TLSOpts: []func(*tls.Config){
				func(c *tls.Config) {
					klog.Info("disabling http/2")
					c.NextProtos = []string{"http/1.1"}
				},
			},
		},
		HealthProbeBindAddress: cfg.HealthProbeBindAddress,
	})
	if err != nil {
		return fmt.Errorf("unable to set up overall controller manager: %w", err)
	}

	klog.InfoS("using store endpoint slice name", "name", cfg.StoreAPIExportEndpointSliceName)
	storeProvider, err := pathaware.New(restCfg, cfg.StoreAPIExportEndpointSliceName, apiexport.Options{
		Scheme: scheme,
	})
	if err != nil {
		return fmt.Errorf("unable to construct store cluster provider: %w", err)
	}
	// Only used to read Stores, so no Aware engages its clusters.
	if err := mgr.GetLocalManager().Add(manager.RunnableFunc(func(ctx context.Context) error {
		return storeProvider.Start(ctx, nil)
	})); err != nil {
		return fmt.Errorf("unable to register store cluster provider: %w", err)
	}

	clusterCache, err := clustercache.New(mgr, clustercache.WithStoreClusters(storeProvider))
	if err != nil {
		return fmt.Errorf("failed to create cluster cache: %w", err)
	}

	fga := deps.OpenFGA
	if fga == nil {
		conn, err := grpc.NewClient(cfg.OpenFGAAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		)
		if err != nil {
			return fmt.Errorf("cannot create grpc client to OpenFGA: %w", err)
		}
		defer conn.Close() //nolint:errcheck

		fga = openfgav1.NewOpenFGAServiceClient(conn)
	}

	// The store is created by the pm-openfga-operator, on a fresh
	// install, e.g. in CI, the authz webhook may be started
	// before the store is available. Instead of failing right away
	// wait until the store is available.
	storeID, err := WaitForStore(ctx, fga, OrgsStoreName, cfg.StoreWaitTimeout)
	if err != nil {
		return fmt.Errorf("cannot find the OpenFGA store to authorize against: %w", err)
	}
	klog.InfoS("using OpenFGA store", "id", storeID)

	extraAttrClusterKey := cfg.Webhook.ClusterKey
	cacheMissTracker := retry.NewExpiringRetryTracker[string](ctx, cfg.Webhook.CacheMissMaxRetries, cfg.Webhook.CacheMissTTL)
	mgr.GetWebhookServer().Register(AuthzPath, authorization.New(
		log,
		union.New(
			nonresourceattributes.New(cfg.Webhook.AllowedNonResourcePrefixes...),
			orgs.New(fga, mgr, extraAttrClusterKey, storeID),
			contextual.New(fga, clusterCache, extraAttrClusterKey, cacheMissTracker, cfg.Webhook.CacheMissRetryAfter),
		),
	))

	mgr.GetWebhookServer().Register(BatchAuthzPath, batch.New(
		log,
		fga,
		clusterCache,
		cfg.Webhook.ClusterPathKey,
	))

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}

	if err := mgr.Add(clusterCache); err != nil {
		return fmt.Errorf("unable to register cluster cache: %w", err)
	}

	klog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("problem running manager: %w", err)
	}

	return nil
}

// WaitForStore polls OpenFGA until a store with the given name exists and
// returns its ID. It gives up when ctx is done or, if timeout is positive,
// after timeout. A timeout of zero or less waits until ctx is done.
func WaitForStore(ctx context.Context, fga openfgav1.OpenFGAServiceClient, name string, timeout time.Duration) (string, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	var storeID string

	err := wait.PollUntilContextCancel(ctx, storePollInterval, true, func(ctx context.Context) (bool, error) {
		id, found, err := store.FindStoreIDByName(ctx, fga, name)
		switch {
		case err != nil:
			klog.ErrorS(err, "cannot list OpenFGA stores, retrying", "name", name)
			return false, nil
		case !found:
			klog.InfoS("no OpenFGA store yet, retrying", "name", name)
			return false, nil
		default:
			storeID = id
			return true, nil
		}
	})
	if err != nil {
		return "", fmt.Errorf("waiting for the %q OpenFGA store: %w", name, err)
	}

	return storeID, nil
}
