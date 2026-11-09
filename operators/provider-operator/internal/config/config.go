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

package config

import (
	"github.com/spf13/pflag"
)

type KCPConfig struct {
	Url                    string
	Namespace              string
	RootShardName          string
	FrontProxyName         string
	FrontProxyPort         string
	ClusterAdminSecretName string
}

type ManagedProviderSubroutineConfig struct {
	Enabled bool
}

type ManagedProviderSubroutinesConfig struct {
	ProviderResource ManagedProviderSubroutineConfig
	WaitProvider     ManagedProviderSubroutineConfig
	KubeconfigCopy   ManagedProviderSubroutineConfig
	Deploy           ManagedProviderSubroutineConfig
}

type SubroutinesConfig struct {
	ManagedProvider ManagedProviderSubroutinesConfig
}

// OperatorConfig struct to hold the app config
type OperatorConfig struct {
	KCP         KCPConfig
	Subroutines SubroutinesConfig
}

func NewOperatorConfig() OperatorConfig {
	return OperatorConfig{
		KCP: KCPConfig{
			Namespace:              "platform-mesh-system",
			RootShardName:          "root",
			FrontProxyName:         "frontproxy",
			FrontProxyPort:         "8443",
			ClusterAdminSecretName: "kcp-cluster-admin-client-cert",
		},
		Subroutines: SubroutinesConfig{
			ManagedProvider: ManagedProviderSubroutinesConfig{
				ProviderResource: ManagedProviderSubroutineConfig{Enabled: true},
				WaitProvider:     ManagedProviderSubroutineConfig{Enabled: true},
				KubeconfigCopy:   ManagedProviderSubroutineConfig{Enabled: true},
				Deploy:           ManagedProviderSubroutineConfig{Enabled: true},
			},
		},
	}
}

func (c *OperatorConfig) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.KCP.Url, "kcp-url", c.KCP.Url, "Set KCP URL")
	fs.StringVar(&c.KCP.Namespace, "kcp-namespace", c.KCP.Namespace, "Set KCP namespace")
	fs.StringVar(&c.KCP.RootShardName, "kcp-root-shard-name", c.KCP.RootShardName, "Set KCP root shard name")
	fs.StringVar(&c.KCP.FrontProxyName, "kcp-front-proxy-name", c.KCP.FrontProxyName, "Set KCP front-proxy name")
	fs.StringVar(&c.KCP.FrontProxyPort, "kcp-front-proxy-port", c.KCP.FrontProxyPort, "Set KCP front-proxy port")
	fs.StringVar(&c.KCP.ClusterAdminSecretName, "kcp-cluster-admin-secret-name", c.KCP.ClusterAdminSecretName, "Set cluster-admin secret name")

	fs.BoolVar(&c.Subroutines.ManagedProvider.ProviderResource.Enabled, "subroutines-managed-provider-resource-enabled", c.Subroutines.ManagedProvider.ProviderResource.Enabled, "Enable ManagedProvider provider-resource subroutine")
	fs.BoolVar(&c.Subroutines.ManagedProvider.WaitProvider.Enabled, "subroutines-managed-provider-wait-enabled", c.Subroutines.ManagedProvider.WaitProvider.Enabled, "Enable ManagedProvider wait-provider subroutine")
	fs.BoolVar(&c.Subroutines.ManagedProvider.KubeconfigCopy.Enabled, "subroutines-managed-provider-kubeconfig-enabled", c.Subroutines.ManagedProvider.KubeconfigCopy.Enabled, "Enable ManagedProvider kubeconfig-copy subroutine")
	fs.BoolVar(&c.Subroutines.ManagedProvider.Deploy.Enabled, "subroutines-managed-provider-deploy-enabled", c.Subroutines.ManagedProvider.Deploy.Enabled, "Enable ManagedProvider deploy subroutine")
}
