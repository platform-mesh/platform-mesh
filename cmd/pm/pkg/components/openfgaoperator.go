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

package components

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sync/errgroup"

	fgaapp "go.platform-mesh.io/pm-openfga-operator/pkg/app"
	fgaconfig "go.platform-mesh.io/pm-openfga-operator/pkg/config"

	"k8s.io/client-go/rest"
)

// OpenFGAOperatorName is the name of the OpenFGAOperator component.
const OpenFGAOperatorName = "pm-openfga-operator"

// coreModule is the authorization model every organization's store starts
// from. It is a copy of coreModule in charts/pm-openfga-operator/values.yaml,
// which is not importable: keep the two the same.
//
//go:embed coremodule.fga
var coreModule []byte

// OpenFGAOperator keeps OpenFGA in step with kcp: a store per organization,
// its authorization model, and the tuples that say who owns which account.
// It is the five roles of operators/pm-openfga-operator, five Deployments in
// a cluster and five goroutines here:
//
//   - initializer and terminator hold a new or a deleted workspace until
//     its store and tuples are written or removed,
//   - fga writes the stores, models and tuples to OpenFGA,
//   - system grants who may bind which APIExport,
//   - model-generator extends a store's model with the APIs bound in the
//     organization.
func OpenFGAOperator() Component {
	return Component{
		Name:     OpenFGAOperatorName,
		OpenFGA:  true,
		Bindings: []string{coreExport, openfgaExport},
		Manifests: []string{
			"01-platform-mesh-system/apiresourceschema-stores.openfga.platform-mesh.io.yaml",
			"01-platform-mesh-system/apiresourceschema-apiexportpolicies.openfga.platform-mesh.io.yaml",
			"01-platform-mesh-system/apiresourceschema-authorizationmodels.openfga.platform-mesh.io.yaml",
			"01-platform-mesh-system/apiresourceschema-providers.providers.platform-mesh.io.yaml",
			"01-platform-mesh-system/apiresourceschema-providerpermissions.providers.platform-mesh.io.yaml",
			"01-platform-mesh-system/apiexport-openfga.platform-mesh.io.yaml",
			"01-platform-mesh-system/apiexport-provider-openfga.platform-mesh.io.yaml",
			"01-platform-mesh-system/apiexport-providers.platform-mesh.io.yaml",
			"01-platform-mesh-system/apiexportendpointslice-openfga.platform-mesh.io.yaml",
			"01-platform-mesh-system/apiexportendpointslice-provider-openfga.platform-mesh.io.yaml",
		},
		// The type whose initializer and terminator the operator serves;
		// the org and account types extend it.
		WorkspaceTypes: []string{"workspace-type-openfga.yaml"},
		// The store of :root:orgs itself: who may create organizations.
		OrgsManifests: []string{"03-orgs/store.yaml"},
		Run: func(ctx context.Context, env Env) error {
			if env.OpenFGA == nil {
				return errors.New("needs OpenFGA")
			}
			log, err := newLogger(OpenFGAOperatorName, env.LogLevel)
			if err != nil {
				return err
			}
			modulePath := filepath.Join(env.DataDir, "openfga", "coreModule.fga")
			if err := os.MkdirAll(filepath.Dir(modulePath), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(modulePath, coreModule, 0o600); err != nil {
				return err
			}

			deps := fgaapp.Deps{Log: log, CommonConfig: quietCommon(), FGAClient: env.OpenFGA.Client}
			newConfig := func(workspace string) (*fgaconfig.Config, error) {
				cfg := fgaconfig.NewConfig()
				cfg.FGA.Target = env.OpenFGA.Addr
				cfg.CoreModulePath = modulePath
				cfg.KCP.Workspace = workspace
				cfg.WorkspacePath = env.Platform.Root.String()
				// The deprecated core.platform-mesh.io Stores and models have
				// exports of their own in a cluster; they are not installed.
				if err := cfg.FeatureGates.Set("LegacyAPIs=false"); err != nil {
					return nil, err
				}
				return &cfg, nil
			}

			roles := []struct {
				name string
				// workspace is where the role's APIExports are or, for the
				// initializer and the terminator, where the WorkspaceType is.
				workspace string
				run       func(context.Context, *rest.Config, *fgaconfig.Config, fgaapp.Deps) error
			}{
				{"fga", env.Platform.System.String(), fgaapp.RunOperator},
				{"system", env.Platform.System.String(), fgaapp.RunSystem},
				{"model-generator", env.Platform.System.String(), fgaapp.RunModelGenerator},
				{"initializer", env.Platform.Root.String(), fgaapp.RunInitializer},
				{"terminator", env.Platform.Root.String(), fgaapp.RunTerminator},
			}
			g, ctx := errgroup.WithContext(ctx)
			for _, role := range roles {
				cfg, err := newConfig(role.workspace)
				if err != nil {
					return err
				}
				g.Go(func() error {
					if err := role.run(ctx, env.Config, cfg, deps); err != nil {
						return fmt.Errorf("%s: %w", role.name, err)
					}
					return nil
				})
			}
			return g.Wait()
		},
	}
}
