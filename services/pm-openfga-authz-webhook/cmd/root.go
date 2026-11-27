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

	"k8s.io/component-base/logs"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/klog/v2"

	_ "k8s.io/component-base/logs/json/register"
)

var (
	logOpts = logs.NewOptions()

	rootCmd = &cobra.Command{
		Use: "pm-openfga-authz-webhook",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return logsapi.ValidateAndApply(logOpts, nil)
		},
	}
)

func init() {
	rootCmd.AddCommand(NewServeCmd())

	logsapi.AddFlags(logOpts, rootCmd.PersistentFlags())
}

func Execute() {
	defer klog.Flush()
	cobra.CheckErr(rootCmd.Execute())
}
