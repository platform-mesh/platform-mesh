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

	platformeshcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/pm-openfga-operator/pkg/app"

	ctrl "sigs.k8s.io/controller-runtime"
)

var modelGeneratorCmd = &cobra.Command{
	Use: "model-generator",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctrl.SetLogger(log.ComponentLogger("controller-runtime").Logr())

		ctx, _, shutdown := platformeshcontext.StartContext(log, defaultCfg, defaultCfg.ShutdownTimeout)
		defer shutdown()

		restCfg, err := getKubeconfigFromPath(generatorCfg.KCP.Kubeconfig)
		if err != nil {
			log.Error().Err(err).Msg("unable to get kcp kubeconfig")
			return err
		}

		if err := app.RunModelGenerator(ctx, restCfg, &generatorCfg, app.Deps{
			Log:          log,
			CommonConfig: defaultCfg,
		}); err != nil {
			log.Error().Err(err).Msg("problem running model-generator manager")
			return err
		}
		return nil
	},
}
