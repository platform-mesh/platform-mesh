/*
Copyright 2026.

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

	"github.com/spf13/cobra"

	"go.platform-mesh.io/provider-operator/internal/seed"
)

// initKubeconfigPath is the path to the kcp admin kubeconfig used by `init`.
// When empty, the operator falls back to reading the cluster-admin secret via
// an in-cluster client (see internal/seed).
var initKubeconfigPath string

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "initialize the kcp API surface for platform-mesh (intended to run as an initContainer)",
	Long: "init idempotently bootstraps the provider workspace structure (the provider/providers " +
		"WorkspaceTypes and the root:providers container) and the kcp API surface (workspace, " +
		"APIResourceSchema, APIExport and APIExportEndpointSlice) required by the provider-operator. " +
		"Shared structure is created only when absent, so it coexists with an existing platform-mesh " +
		"install. It exits non-zero on failure so an initContainer blocks the main container until initialized.",
	RunE: RunInit,
}

// RunInit executes the idempotent kcp seeding logic.
func RunInit(cmd *cobra.Command, _ []string) error {
	ctx := context.Background()
	if cmd != nil {
		ctx = cmd.Context()
	}
	return seed.Run(ctx, log, &operatorCfg, initKubeconfigPath)
}
