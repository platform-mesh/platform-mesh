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

	"go.platform-mesh.io/pm-openfga-authz-webhook/pkg/app"
	"go.platform-mesh.io/pm-openfga-authz-webhook/pkg/config"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
)

var serverCfg *config.Config

func NewServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Starts the authorization webhook server",
		Run: func(cmd *cobra.Command, args []string) {
			ctrl.SetLogger(klog.NewKlogr())

			restCfg := ctrl.GetConfigOrDie()

			if err := app.Run(ctrl.SetupSignalHandler(), restCfg, serverCfg, app.Deps{}); err != nil {
				klog.Exit(err)
			}
		},
	}

	serverCfg = config.New()
	serverCfg.AddFlags(cmd.Flags())
	return cmd
}
