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
	apisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	tenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	"github.com/spf13/cobra"
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	platformmeshcontext "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"go.platform-mesh.io/iam-service/pkg/config"
)

var (
	scheme     = runtime.NewScheme()
	serviceCfg = config.NewServiceConfig()
	defaultCfg *platformmeshcontext.CommonServiceConfig
	log        *logger.Logger
)

var rootCmd = &cobra.Command{
	Use:   "iam-service",
	Short: "the platform mesh iam-service",
}

func init() {
	utilruntime.Must(pmcorev1alpha1.AddToScheme(scheme))
	utilruntime.Must(tenancyv1alpha1.AddToScheme(scheme))
	utilruntime.Must(apisv1alpha1.AddToScheme(scheme))
	rootCmd.AddCommand(serverCmd)

	defaultCfg = platformmeshcontext.NewDefaultConfig()
	defaultCfg.AddFlags(rootCmd.PersistentFlags())
	serviceCfg.AddFlags(serverCmd.Flags())

	cobra.OnInitialize(initLog)
}

func initLog() {
	lCfg := logger.DefaultConfig()
	lCfg.Level = defaultCfg.Log.Level
	lCfg.NoJSON = defaultCfg.Log.NoJson

	var err error
	log, err = logger.New(lCfg)
	if err != nil {
		panic(err)
	}
}

func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}
