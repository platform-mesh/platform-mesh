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

	pmcontext "go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/iam-service/pkg/app"

	ctrl "sigs.k8s.io/controller-runtime"
)

var serverCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start serving",
	Long:  `Start the IAM Service as a Webservice`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, _, shutdown := pmcontext.StartContext(log, serviceCfg, defaultCfg.ShutdownTimeout)
		defer shutdown()

		ctrl.SetLogger(log.Logr())

		err := app.Run(ctx, ctrl.GetConfigOrDie(), app.Config{
			Common:  defaultCfg,
			Service: serviceCfg,
		}, app.Deps{
			Logger: log,
		})
		if err != nil {
			log.Fatal().Err(err).Msg("iam-service failed")
		}
	},
}
