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

	"go.platform-mesh.io/account-operator/internal/config"
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	platformmeshcontext "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

var (
	scheme      = runtime.NewScheme()
	operatorCfg config.OperatorConfig
	defaultCfg  *platformmeshcontext.CommonServiceConfig
	log         *logger.Logger
)

var rootCmd = &cobra.Command{
	Use:   "account-operator",
	Short: "operator to reconcile Accounts",
}

func init() {
	utilruntime.Must(pmcorev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpapisv1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpcorev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme

	rootCmd.AddCommand(operatorCmd)

	defaultCfg = platformmeshcontext.NewDefaultConfig()
	operatorCfg = config.NewOperatorConfig()
	defaultCfg.AddFlags(rootCmd.PersistentFlags())
	operatorCfg.AddFlags(operatorCmd.Flags())

	cobra.OnInitialize(initLog)
}

func initLog() { // coverage-ignore
	logcfg := logger.DefaultConfig()
	logcfg.Level = defaultCfg.Log.Level
	logcfg.NoJSON = defaultCfg.Log.NoJson

	var err error
	log, err = logger.New(logcfg)
	if err != nil {
		panic(err)
	}
	ctrl.SetLogger(log.Logr())
}

func Execute() { // coverage-ignore
	cobra.CheckErr(rootCmd.Execute())
}
