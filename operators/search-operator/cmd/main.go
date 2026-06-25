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

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"os"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmsearchv1alpha1 "go.platform-mesh.io/apis/search/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/search-operator/internal/config"
	"go.platform-mesh.io/search-operator/internal/controller"
	"go.platform-mesh.io/search-operator/internal/opensearch"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	// kcp imports
	"github.com/kcp-dev/multicluster-provider/apiexport"
	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	// Add kcp types to scheme
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpcorev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha1.AddToScheme(scheme))

	// Add our types
	utilruntime.Must(pmcorev1alpha1.AddToScheme(scheme))
	utilruntime.Must(pmsearchv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var kcpKubeconfig string
	var apiExportEndpointSliceName string
	var enableHTTP2 bool
	var maxConcurrentReconciles int
	var tlsOpts []func(*tls.Config)

	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&kcpKubeconfig, "kcp-kubeconfig", "/etc/kcp/kubeconfig",
		"Path to the kcp kubeconfig file.")
	flag.StringVar(&apiExportEndpointSliceName, "api-export-endpoint-slice-name", "search.platform-mesh.io",
		"Name of the APIExportEndpointSlice to use for the multicluster provider.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.IntVar(&maxConcurrentReconciles, "max-concurrent-reconciles", 1,
		"Maximum number of concurrent reconciles per controller")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Create logger for golang-commons
	logCfg := logger.DefaultConfig()
	log, err := logger.New(logCfg)
	if err != nil {
		setupLog.Error(err, "unable to create logger")
		os.Exit(1)
	}

	// Disable HTTP/2 due to vulnerabilities
	if !enableHTTP2 {
		disableHTTP2 := func(c *tls.Config) {
			setupLog.Info("disabling http/2")
			c.NextProtos = []string{"http/1.1"}
		}
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Get kcp config
	kcpCfg, err := getKCPConfig(kcpKubeconfig)
	if err != nil {
		setupLog.Error(err, "unable to get kcp config")
		os.Exit(1)
	}

	// Create kcp multicluster provider using APIExport
	provider, err := apiexport.New(kcpCfg, apiExportEndpointSliceName, apiexport.Options{
		Scheme: scheme,
	})
	if err != nil {
		setupLog.Error(err, "unable to create cluster provider")
		os.Exit(1)
	}

	// Manager options
	mgrOpts := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
			TLSOpts:     tlsOpts,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "search-operator.platform-mesh.io",
	}

	// Create the multicluster manager
	mgr, err := mcmanager.New(kcpCfg, provider, mgrOpts)
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	cfg, err := config.NewFromEnv()
	if err != nil {
		setupLog.Info("OpenSearch not configured, workspace indexing disabled")
	}

	// Initialize OpenSearch client if configured
	var osClient *opensearch.Client
	setupLog.Info("initializing OpenSearch client", "url", cfg.OpenSearch.URL)
	osClient, err = opensearch.NewClientFromEnv(cfg)
	if err != nil {
		setupLog.Error(err, "unable to create OpenSearch client")
		os.Exit(1)
	}

	if err := osClient.Ping(context.Background()); err != nil {
		setupLog.Error(err, "unable to connect to OpenSearch")
		os.Exit(1)
	}
	setupLog.Info("OpenSearch client connected successfully")

	// Setup SearchIndex controller using lifecycle manager pattern
	if err := controller.NewSearchIndexReconciler(
		log, mgr, osClient, cfg.OpenSearch.IndexNamePrefix, cfg.OpenSearch.SemanticModelID,
	).SetupWithManager(mgr, maxConcurrentReconciles); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "SearchIndex")
		os.Exit(1)
	}

	// Setup IndexableResource controllers for each configured searchable resource type
	for _, GVK := range cfg.SearchableResource.Resources {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(GVK)
		idxRssReconciler, err := controller.NewIndexableResource(log, *cfg, mgr, osClient, apiExportEndpointSliceName, obj)
		if err != nil {
			setupLog.Error(err, "unable to create IndexableResource reconciler")
			os.Exit(1)
		}
		if err := idxRssReconciler.SetupWithManager(mgr, maxConcurrentReconciles, obj); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "IndexableResource")
			os.Exit(1)
		}
	}

	// Setup APIBinding controller: reconciles APIBindings and ensures SearchIndex resources
	// in the owning org workspace for each bound export.
	apiBindingReconciler, err := controller.NewAPIBindingReconciler(log, mgr, cfg.OpenSearch.IndexNamePrefix)
	if err != nil {
		setupLog.Error(err, "unable to create APIBinding reconciler")
		os.Exit(1)
	}
	if err := apiBindingReconciler.SetupWithManager(mgr, maxConcurrentReconciles); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "APIBinding")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// getKCPConfig loads the kcp kubeconfig from the specified path
func getKCPConfig(kubeconfigPath string) (*rest.Config, error) {
	// First check if KCP_KUBECONFIG env var is set
	if envPath := os.Getenv("KCP_KUBECONFIG"); envPath != "" {
		kubeconfigPath = envPath
	}

	return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
}
