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

// Package cmd is the entry point of sdk-access-operator: the user-side
// AccessIdentity reconciler that mints the HMAC token + Secret and keeps
// its Ready status in sync. The /access HTTP server lives in a separate
// workload (sdk-access-server).
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/pflag"

	accessv1alpha1 "go.platform-mesh.io/apis/sdk-access/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	accessctrl "go.platform-mesh.io/sdk-access-operator/pkg/controller/access"
	identityctrl "go.platform-mesh.io/sdk-access-operator/pkg/controller/accessidentity"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// Execute runs the sdk-access-operator binary.
func Execute() {
	var (
		metricsAddr string
		probeAddr   string
		leaderElect bool
		logLevel    string
		logNoJSON   bool
	)

	pflag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "Metrics listen address.")
	pflag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "Health probe listen address.")
	pflag.BoolVar(&leaderElect, "leader-elect", true, "Enable leader election.")
	pflag.StringVar(&logLevel, "log-level", "info", "Set the log level")
	pflag.BoolVar(&logNoJSON, "no-json", false, "Disable JSON logging")
	pflag.Parse()

	logcfg := logger.DefaultConfig()
	logcfg.Name = "sdk-access-operator"
	logcfg.Level = logLevel
	logcfg.NoJSON = logNoJSON
	log, err := logger.New(logcfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to build logger: %v\n", err)
		os.Exit(1)
	}
	ctrl.SetLogger(log.Logr())
	setupLog := ctrl.Log.WithName("setup")

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(accessv1alpha1.AddToScheme(scheme))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress:        probeAddr,
		LeaderElection:                leaderElect,
		LeaderElectionID:              "sdk-access-operator.access-sdk.platform-mesh.io",
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to build manager")
		os.Exit(1)
	}

	if err := (&identityctrl.Reconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to register AccessIdentity reconciler")
		os.Exit(1)
	}

	if err := (&accessctrl.Reconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to register Access reconciler")
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

	setupLog.Info("starting sdk-access-operator")
	ctx := logger.SetLoggerInContext(ctrl.SetupSignalHandler(), log)
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "manager stopped with error")
		os.Exit(1)
	}
}
