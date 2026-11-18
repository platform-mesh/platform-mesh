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

// Command kro-composition-operator watches ResourceGraphDefinitions across every workspace
// that installed KROaaS and publishes each composite type as a bound API.
package main

import (
	"flag"
	"os"
	"strings"

	krofeatures "github.com/kubernetes-sigs/kro/pkg/features"

	"go.platform-mesh.io/kro-composition-operator/pkg/app"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func main() {
	cfg := app.DefaultConfig()
	flag.StringVar(&cfg.APIExportEndpointSlice, "apiexport-endpointslice", cfg.APIExportEndpointSlice, "APIExportEndpointSlice serving the kro.run RGD API")
	flag.StringVar(&cfg.ProviderWorkspace, "provider-workspace", cfg.ProviderWorkspace, "workspace path holding the kro.run APIExport + endpointslice")
	flag.StringVar(&cfg.HealthProbeBindAddress, "health-probe-bind-address", cfg.HealthProbeBindAddress, "address the health/readiness probe endpoint binds to")
	flag.StringVar(&cfg.MetricsBindAddress, "metrics-bind-address", cfg.MetricsBindAddress, "address the metrics endpoint binds to ('0' disables it)")
	flag.BoolVar(&cfg.LeaderElect, "leader-elect", false, "enable leader election for controller manager (ensures a single active instance)")
	// The gates belong to the embedded kro engine and are read from its global gate, so
	// setting them here reaches both the graph builder and the instance reconciler.
	flag.Func("feature-gates", "comma-separated kro feature gates as key=value ("+
		strings.Join(krofeatures.FeatureGate.KnownFeatures(), ", ")+")", krofeatures.FeatureGate.Set)
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	logf.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if err := app.Run(ctrl.SetupSignalHandler(), config.GetConfigOrDie(), cfg); err != nil {
		logf.Log.WithName("setup").Error(err, "kro-composition-operator")
		os.Exit(1)
	}
}
