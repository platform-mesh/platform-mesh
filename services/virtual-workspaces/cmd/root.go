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
	"flag"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.platform-mesh.io/virtual-workspaces/pkg/config"
	genericapiserveroptions "k8s.io/apiserver/pkg/server/options"
	"k8s.io/klog/v2"
)

var (
	cfg                            = config.NewServiceConfig()
	secureServing                  = genericapiserveroptions.SecureServingOptions{}
	delegatingAuthenticationOption = genericapiserveroptions.DelegatingAuthenticationOptions{}
)

var rootCmd = &cobra.Command{
	Use:   "virtual-workspaces",
	Short: "The Platform-Mesh virtual workspace",
}

func init() {
	rootCmd.AddCommand(startCmd)
	cfg.AddFlags(startCmd.Flags())

	delegatingAuthenticationOption = *genericapiserveroptions.NewDelegatingAuthenticationOptions()

	delegatingAuthenticationOption.AddFlags(startCmd.Flags())
	secureServing.AddFlags(startCmd.Flags())

	klogFlagSet := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlagSet)

	pflag.CommandLine.AddGoFlagSet(klogFlagSet)
	rootCmd.PersistentFlags().AddGoFlagSet(klogFlagSet)
}

func Execute() { // coverage-ignore
	defer klog.Flush()
	cobra.CheckErr(rootCmd.Execute())
}
