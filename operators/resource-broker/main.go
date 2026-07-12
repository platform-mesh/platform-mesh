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

// The main package running the resource-broker.
package main

import (
	"flag"
	"os"

	"go.platform-mesh.io/resource-broker/pkg/broker"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	mctrl "sigs.k8s.io/multicluster-runtime"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var (
	setupLog = ctrl.Log.WithName("setup")

	fKcpKubeconfig = flag.String(
		"kcp-kubeconfig",
		"",
		"Kubeconfig for the kcp instance. If not set, the local config is used.",
	)
	fComputeKubeconfig = flag.String(
		"compute-kubeconfig",
		"",
		"Kubeconfig for the compute cluster. If not set, the local config is used.",
	)

	fAcceptAPI = flag.String(
		"acceptapi",
		"",
		"APIExportEndpointSlice name to watch for AcceptAPIs. All other APIExportEndpointSlices in the broker workspace serve brokered APIs.",
	)

	fCoordinationWorkspace = flag.String(
		"coordination-workspace",
		"root:platform:broker",
		"kcp workspace path holding Assignments and StagingWorkspaces",
	)
	fVerificationTreeRoot = flag.String(
		"verification-tree-root",
		"root:platform:broker:verification",
		"kcp workspace path under which verification workspaces are created",
	)
	fStagingTreeRoot = flag.String(
		"staging-tree-root",
		"root:platform:broker:staging",
		"kcp workspace path under which staging workspaces are created",
	)

	fRequeueInterval = flag.Duration(
		"requeue-interval",
		0,
		"Interval between reconciliations while waiting on pending state transitions. If not set, the controller defaults are used.",
	)
)

// loadKubeconfig loads a rest config from the given kubeconfig path, falling
// back to fallback if the path is empty.
func loadKubeconfig(path string, fallback *rest.Config) (*rest.Config, error) {
	if path == "" {
		return fallback, nil
	}

	raw, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return nil, err
	}

	return clientcmd.NewNonInteractiveClientConfig(
		*raw,
		raw.CurrentContext,
		&clientcmd.ConfigOverrides{},
		nil,
	).ClientConfig()
}

func main() {
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)

	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	ctx := mctrl.SetupSignalHandler()

	local, err := ctrl.GetConfig()
	if err != nil {
		setupLog.Error(err, "unable to get local kubeconfig")
		os.Exit(1)
	}

	kcpConfig, err := loadKubeconfig(*fKcpKubeconfig, local)
	if err != nil {
		setupLog.Error(err, "unable to load kcp kubeconfig", "path", *fKcpKubeconfig)
		os.Exit(1)
	}

	computeConfig, err := loadKubeconfig(*fComputeKubeconfig, local)
	if err != nil {
		setupLog.Error(err, "unable to load compute kubeconfig", "path", *fComputeKubeconfig)
		os.Exit(1)
	}

	brk, err := broker.New(broker.Options{
		Log: setupLog.WithName("broker"),

		LocalConfig:   local,
		KcpConfig:     kcpConfig,
		ComputeConfig: computeConfig,

		AcceptAPIName: *fAcceptAPI,

		CoordinationWorkspace: *fCoordinationWorkspace,
		VerificationTreeRoot:  *fVerificationTreeRoot,
		StagingTreeRoot:       *fStagingTreeRoot,

		RequeueInterval: *fRequeueInterval,
	})
	if err != nil {
		setupLog.Error(err, "unable to setup broker")
		os.Exit(1)
	}

	if err := brk.Start(ctx); err != nil {
		setupLog.Error(err, "exiting due to error")
		os.Exit(1)
	}
}
