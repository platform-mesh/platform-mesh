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

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/component-base/logs"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/klog/v2"

	kcpsdkapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpcorev1alpha "github.com/kcp-dev/sdk/apis/core/v1alpha1"

	_ "k8s.io/component-base/logs/json/register"
)

var (
	logOpts = logs.NewOptions()

	rootCmd = &cobra.Command{
		Use: "rebac-authz-webhook",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return logsapi.ValidateAndApply(logOpts, nil)
		},
	}

	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(kcpsdkapisv1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpcorev1alpha.AddToScheme(scheme))
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	rootCmd.AddCommand(NewServeCmd())

	logsapi.AddFlags(logOpts, rootCmd.PersistentFlags())

}

func Execute() {
	defer klog.Flush()
	cobra.CheckErr(rootCmd.Execute())
}
