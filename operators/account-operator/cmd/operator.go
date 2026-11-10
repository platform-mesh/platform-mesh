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
	"github.com/spf13/cobra"

	"go.platform-mesh.io/account-operator/pkg/app"
	platformmeshcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/traces"

	ctrl "sigs.k8s.io/controller-runtime"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var operatorCmd = &cobra.Command{
	Use:   "operator",
	Short: "operator to reconcile Accounts",
	Run:   RunController,
}

func RunController(_ *cobra.Command, _ []string) { // coverage-ignore
	ctrl.SetLogger(log.ComponentLogger("controller-runtime").Logr())

	ctx, _, shutdown := platformmeshcontext.StartContext(log, operatorCfg, defaultCfg.ShutdownTimeout)
	defer shutdown()

	if defaultCfg.Tracing.Enabled {
		traceShutdown, err := traces.InitProvider(ctx, defaultCfg.Tracing.Collector)
		if err != nil {
			log.Fatal().Err(err).Msg("unable to start gRPC-Sidecar TracerProvider")
		}
		defer func() {
			if err := traceShutdown(ctx); err != nil {
				log.Fatal().Err(err).Msg("failed to shutdown TracerProvider")
			}
		}()
	}

	if err := app.Run(ctx, operatorCfg, app.Deps{Log: log, Common: defaultCfg}); err != nil {
		log.Fatal().Err(err).Msg("problem running operator")
	}
}
