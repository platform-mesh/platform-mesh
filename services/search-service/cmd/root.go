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
	platformmeshcontext "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"

	"go.platform-mesh.io/search-service/internal/config"
)

var (
	serviceCfg = config.NewServiceConfig()
	defaultCfg *platformmeshcontext.CommonServiceConfig
	log        *logger.Logger
)

var rootCmd = &cobra.Command{
	Use:   "search-service",
	Short: "Platform Mesh search service",
}

func init() {
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
