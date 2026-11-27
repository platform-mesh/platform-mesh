/*
Copyright 2026 The kcp Authors.

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

// Command access-vw-init installs the kcp-side objects the access virtual
// workspace needs — the APIResourceSchema, the APIExport, and the
// APIExportEndpointSlice the RBAC provider follows — and verifies they came
// up.
//
// The APIExport and the ServiceAccount the server runs as land in
// <prefix>:<controllers-workspace>, default root:access:controllers. Both
// halves are configurable so a deployment can bring its own tree:
//
//	access-vw-init --kubeconfig=admin.kubeconfig \
//	  --workspace-prefix=root:magic --controllers-workspace=controllers
//
// Any missing workspace along that path is created, so the credential needs
// rights to create workspaces from the root down. Idempotent: safe to run on
// every pod start and every upgrade.
package main

import (
	"context"
	goflag "flag"
	"os"
	"time"

	"github.com/spf13/pflag"

	"go.platform-mesh.io/account-operator/virtual-workspace/pkg/bootstrap"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

func main() {
	var (
		kubeconfig           string
		workspacePrefix      string
		controllersWorkspace string
		workspaceType        string
		hostOverride         string
		verifyBinding        string
		serverUsers          []string
		serverGroups         []string
		timeout              time.Duration
	)

	klog.InitFlags(goflag.CommandLine)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	pflag.StringVar(&kubeconfig, "kubeconfig", os.Getenv("KUBECONFIG"),
		"Path to the kubeconfig for the target kcp. Defaults to $KUBECONFIG.")
	pflag.StringVar(&workspacePrefix, "workspace-prefix", bootstrap.DefaultWorkspacePrefix,
		"Parent path the controllers workspace is created under, e.g. root:magic.")
	pflag.StringVar(&controllersWorkspace, "controllers-workspace", bootstrap.DefaultControllersWorkspace,
		"Name of the workspace this component owns, created under --workspace-prefix. "+
			"Holds the APIExport and the ServiceAccount the server runs as.")
	pflag.StringVar(&hostOverride, "host-override", os.Getenv("HOST_OVERRIDE"),
		"Replace scheme://host[:port] in the generated kubeconfig, keeping the "+
			"workspace path. Use when init runs against an external URL but the "+
			"server connects from inside the cluster, e.g. "+
			"https://frontproxy.kcp-system.svc:6443.")
	pflag.StringVar(&workspaceType, "workspace-type", bootstrap.DefaultWorkspaceType,
		"WorkspaceType for any workspace this creates.")
	pflag.StringSliceVar(&serverUsers, "server-user", nil,
		"Additional User to grant the controller role to, repeatable. Needed when the "+
			"server does not run as the ServiceAccount this creates -- kcp-operator "+
			"mounts a client certificate instead, so pass its common name, e.g. "+
			"--server-user=access-vw. Without it the server is denied at startup with "+
			`'cannot get path "/apis": access denied'.`)
	pflag.StringSliceVar(&serverGroups, "server-group", nil,
		"Additional Group to grant the controller role to, repeatable. The certificate "+
			"organization, where --server-user is its common name.")
	pflag.StringVar(&verifyBinding, "verify-apibinding", "",
		"After installing, check that this APIBinding in the target workspace has all "+
			"of its permission claims accepted. Useful when the workspace being "+
			"bootstrapped is also a consumer.")
	pflag.DurationVar(&timeout, "timeout", 3*time.Minute,
		"Overall time budget for installing and verifying.")
	pflag.Parse()

	ctx := context.Background()
	logger := klog.FromContext(ctx)

	if kubeconfig == "" {
		logger.Error(nil, "--kubeconfig is required, or set KUBECONFIG")
		os.Exit(1)
	}

	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		logger.Error(err, "loading kubeconfig", "path", kubeconfig)
		os.Exit(1)
	}

	workspacePath, err := bootstrap.JoinWorkspacePath(workspacePrefix, controllersWorkspace)
	if err != nil {
		logger.Error(err, "invalid workspace configuration")
		os.Exit(1)
	}

	target, err := bootstrap.CreateWorkspacePath(ctx, cfg, workspacePath, workspaceType)
	if err != nil {
		logger.Error(err, "resolving workspace path", "path", workspacePath)
		os.Exit(1)
	}

	result, err := bootstrap.Bootstrap(ctx, target, bootstrap.Options{
		WorkspacePath: workspacePath,
		HostOverride:  hostOverride,
		ServerUsers:   serverUsers,
		ServerGroups:  serverGroups,
		Timeout:       timeout,
	})
	if err != nil {
		logger.Error(err, "bootstrap failed")
		os.Exit(1)
	}

	if verifyBinding != "" {
		if err := bootstrap.VerifyAPIBinding(ctx, target, verifyBinding); err != nil {
			logger.Error(err, "APIBinding verification failed", "name", verifyBinding)
			os.Exit(1)
		}
		logger.Info("APIBinding permission claims accepted", "name", verifyBinding)
	}

	report(logger, result)
}

func report(logger klog.Logger, result *bootstrap.Result) {
	logger.Info("bootstrap complete",
		"workspace", result.WorkspacePath,
		"apiExportEndpointSlice", result.APIExportEndpointSlice,
		"virtualWorkspaceURLs", result.VirtualWorkspaceURLs,
	)
	logger.Info("start the server with",
		"--apiexport-endpointslice", result.APIExportEndpointSlice,
		"--kubeconfig", "(from Secret "+result.KubeconfigSecret+" in this workspace)",
	)
	logger.Info("consumer workspaces opt in with an APIBinding to this export; "+
		"see config/examples/apibinding-consumer.yaml",
		"exportPath", result.WorkspacePath,
		"exportCluster", result.ExportsClusterRef,
	)
}
