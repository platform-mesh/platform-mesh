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

import "github.com/spf13/pflag"

type OperatorConfig struct {
	Kcp            KcpConfig
	WebhookEnabled bool
}

type KcpConfig struct {
	Enabled                    bool
	ApiExportEndpointSliceName string
}

func NewOperatorConfig() OperatorConfig {
	return OperatorConfig{
		Kcp: KcpConfig{
			Enabled:                    false,
			ApiExportEndpointSliceName: "resource-sharding",
		},
		WebhookEnabled: true,
	}
}

func (c *OperatorConfig) AddFlags(flags *pflag.FlagSet) {
	flags.BoolVar(&c.Kcp.Enabled, "kcp-enabled", c.Kcp.Enabled, "Enable KCP multicluster provider")
	flags.StringVar(&c.Kcp.ApiExportEndpointSliceName, "kcp-api-export-endpoint-slice-name", c.Kcp.ApiExportEndpointSliceName, "Name of the APIExportEndpointSlice to use for KCP")
	flags.BoolVar(&c.WebhookEnabled, "webhook-enabled", c.WebhookEnabled, "Enable mutating admission webhook registration")
}
