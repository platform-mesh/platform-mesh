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
	"net/http"
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"go.platform-mesh.io/golang-commons/fga/store"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/authorization"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/authorization/union"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/clustercache"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/config"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/handler/batch"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/handler/contextual"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/handler/nonresourceattributes"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/handler/orgs"
	"go.platform-mesh.io/rebac-authz-webhook/pkg/retry"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"github.com/kcp-dev/multicluster-provider/apiexport"
	pathaware "github.com/kcp-dev/multicluster-provider/path-aware"
)

var serverCfg *config.Config

func NewServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Starts the authorization webhook server",
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()

			ctrl.SetLogger(klog.NewKlogr())

			restCfg := ctrl.GetConfigOrDie()

			restCfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
				return otelhttp.NewTransport(rt)
			})

			endpointSliceName := serverCfg.APIExportEndpointSliceName
			klog.InfoS("using endpoint slice name", "name", endpointSliceName)

			provider, err := pathaware.New(restCfg, endpointSliceName, apiexport.Options{
				Scheme: scheme,
			})
			if err != nil {
				klog.Exit(err, "unable to construct cluster provider")
			}

			// Use Root kcp config for manager
			mgr, err := mcmanager.New(restCfg, provider, mcmanager.Options{
				Scheme: scheme,
				Logger: klog.NewKlogr(),
				WebhookServer: webhook.NewServer(webhook.Options{
					CertDir: serverCfg.Webhook.CertDir,
				}),
				Metrics: metricsserver.Options{
					BindAddress: serverCfg.MetricsBindAddress,
					TLSOpts: []func(*tls.Config){
						func(c *tls.Config) {
							klog.Info("disabling http/2")
							c.NextProtos = []string{"http/1.1"}
						},
					},
				},
				HealthProbeBindAddress: serverCfg.HealthProbeBindAddress,
			})
			if err != nil {
				klog.Exit(err, "unable to set up overall controller manager")
			}

			klog.InfoS("using store endpoint slice name", "name", serverCfg.StoreAPIExportEndpointSliceName)
			storeProvider, err := pathaware.New(restCfg, serverCfg.StoreAPIExportEndpointSliceName, apiexport.Options{
				Scheme: scheme,
			})
			if err != nil {
				klog.Exit(err, "unable to construct store cluster provider")
			}
			// Only used to read Stores, so no Aware engages its clusters.
			if err := mgr.GetLocalManager().Add(manager.RunnableFunc(func(ctx context.Context) error {
				return storeProvider.Start(ctx, nil)
			})); err != nil {
				klog.Exit(err, "unable to register store cluster provider")
			}

			clusterCache, err := clustercache.New(mgr, clustercache.WithStoreClusters(storeProvider))
			if err != nil {
				klog.Exit(err, "failed to create cluster cache")
			}

			conn, err := grpc.NewClient(serverCfg.OpenFGAAddr,
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
			)
			if err != nil {
				klog.Exit(err, "cannot create grpc client to OpenFGA")
			}
			defer conn.Close() //nolint:errcheck

			fga := openfgav1.NewOpenFGAServiceClient(conn)

			// The store is created by the security-operator, on a fresh
			// install, e.g. in CI, the authz webhook may be started
			// before the store is available. Instead of crash-looping
			// the pod wait until the store is available.
			storeID, err := waitForStore(ctx, fga, orgsStoreName)
			if err != nil {
				klog.Exit(err, "cannot find the OpenFGA store to authorize against")
			}
			klog.InfoS("using OpenFGA store", "id", storeID)

			extraAttrClusterKey := serverCfg.Webhook.ClusterKey
			cacheMissTracker := retry.NewExpiringRetryTracker[string](ctx, serverCfg.Webhook.CacheMissMaxRetries, serverCfg.Webhook.CacheMissTTL)
			mgr.GetWebhookServer().Register("/authz", authorization.New(
				klog.NewKlogr(),
				union.New(
					nonresourceattributes.New(serverCfg.Webhook.AllowedNonResourcePrefixes...),
					orgs.New(fga, mgr, extraAttrClusterKey, storeID),
					contextual.New(fga, clusterCache, extraAttrClusterKey, cacheMissTracker, serverCfg.Webhook.CacheMissRetryAfter),
				),
			))

			batchHandler := batch.New(
				klog.NewKlogr(),
				fga,
				clusterCache,
				serverCfg.Webhook.ClusterPathKey,
			)
			mgr.GetWebhookServer().Register("/batch-authz", batchHandler)

			if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
				klog.Exit(err, "unable to set up health check")
			}
			if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
				klog.Exit(err, "unable to set up ready check")
			}

			if err := mgr.Add(clusterCache); err != nil {
				klog.Exit(err, "unable to register cluster cache")
			}

			klog.Info("starting manager")
			if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
				klog.Exit(err, "problem running manager")
			}
		},
	}

	serverCfg = config.New()
	serverCfg.AddFlags(cmd.Flags())
	return cmd
}

const (
	orgsStoreName     = "orgs"
	storePollInterval = 2 * time.Second
	storePollTimeout  = 4 * time.Minute
)

func waitForStore(ctx context.Context, fga openfgav1.OpenFGAServiceClient, name string) (string, error) {
	var storeID string

	err := wait.PollUntilContextTimeout(ctx, storePollInterval, storePollTimeout, true, func(ctx context.Context) (bool, error) {
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
