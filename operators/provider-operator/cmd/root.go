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

	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	pmconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

var (
	scheme     = runtime.NewScheme()
	defaultCfg *pmconfig.CommonServiceConfig
	log        *logger.Logger
)

var rootCmd = &cobra.Command{
	Use:   "provider-operator",
	Short: "operator to reconcile Provider and ManagedProvider resources",
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(pmprovidersv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme

	rootCmd.AddCommand(operatorCmd)

	defaultCfg = pmconfig.NewDefaultConfig()
	defaultCfg.AddFlags(rootCmd.PersistentFlags())

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
}

func Execute() { // coverage-ignore
	cobra.CheckErr(rootCmd.Execute())
}
