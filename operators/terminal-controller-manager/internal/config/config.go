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
	"time"

	"github.com/spf13/pflag"
)

type ToggleConfig struct {
	Enabled bool
}

type SubroutinesConfig struct {
	Lifetime  ToggleConfig
	Pod       ToggleConfig
	Service   ToggleConfig
	HTTPRoute ToggleConfig
}

type KCPConfig struct {
	APIExportEndpointSliceName string
	// Kubeconfig is the path to the kubeconfig file for connecting to kcp.
	// If empty, falls back to in-cluster config.
	Kubeconfig string
}

type TerminalConfig struct {
	Image          string
	Namespace      string
	Lifetime       time.Duration
	HostAliasIP    string
	HostAliasNames []string
}

type GatewayConfig struct {
	Name      string
	Namespace string
	Hostnames []string
}

// OperatorConfig holds the configuration for the terminal-controller-manager
type OperatorConfig struct {
	Subroutines SubroutinesConfig
	Kcp         KCPConfig
	Terminal    TerminalConfig
	Gateway     GatewayConfig
}

func NewOperatorConfig() OperatorConfig {
	return OperatorConfig{
		Subroutines: SubroutinesConfig{
			Lifetime:  ToggleConfig{Enabled: true},
			Pod:       ToggleConfig{Enabled: true},
			Service:   ToggleConfig{Enabled: true},
			HTTPRoute: ToggleConfig{Enabled: true},
		},
		Kcp: KCPConfig{
			APIExportEndpointSliceName: "terminal.platform-mesh.io",
		},
		Terminal: TerminalConfig{
			Image:     "ghcr.io/platform-mesh/terminal:latest",
			Namespace: "terminal-sessions",
			Lifetime:  2 * time.Hour,
		},
		Gateway: GatewayConfig{
			Name:      "k8sapi-gateway",
			Namespace: "platform-mesh-system",
		},
	}
}

func (c *OperatorConfig) AddFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&c.Subroutines.Lifetime.Enabled, "subroutines-lifetime-enabled", c.Subroutines.Lifetime.Enabled, "Enable lifetime subroutine")
	fs.BoolVar(&c.Subroutines.Pod.Enabled, "subroutines-pod-enabled", c.Subroutines.Pod.Enabled, "Enable pod subroutine")
	fs.BoolVar(&c.Subroutines.Service.Enabled, "subroutines-service-enabled", c.Subroutines.Service.Enabled, "Enable service subroutine")
	fs.BoolVar(&c.Subroutines.HTTPRoute.Enabled, "subroutines-httproute-enabled", c.Subroutines.HTTPRoute.Enabled, "Enable HTTPRoute subroutine")

	fs.StringVar(&c.Kcp.APIExportEndpointSliceName, "kcp-api-export-endpoint-slice-name", c.Kcp.APIExportEndpointSliceName, "Set kcp APIExportEndpointSlice name")
	fs.StringVar(&c.Kcp.Kubeconfig, "kcp-kubeconfig", c.Kcp.Kubeconfig, "Path to the kubeconfig file for connecting to kcp")

	fs.StringVar(&c.Terminal.Image, "terminal-image", c.Terminal.Image, "Terminal container image")
	fs.StringVar(&c.Terminal.Namespace, "terminal-namespace", c.Terminal.Namespace, "Runtime namespace for terminal resources")
	fs.DurationVar(&c.Terminal.Lifetime, "terminal-lifetime", c.Terminal.Lifetime, "Maximum lifetime of a terminal session")
	fs.StringVar(&c.Terminal.HostAliasIP, "terminal-host-alias-ip", c.Terminal.HostAliasIP, "Optional host alias IP for terminal pods")
	fs.StringSliceVar(&c.Terminal.HostAliasNames, "terminal-host-alias-names", c.Terminal.HostAliasNames, "Optional host alias names for terminal pods")

	fs.StringVar(&c.Gateway.Name, "gateway-name", c.Gateway.Name, "Gateway name used for terminal HTTPRoutes")
	fs.StringVar(&c.Gateway.Namespace, "gateway-namespace", c.Gateway.Namespace, "Gateway namespace used for terminal HTTPRoutes")
	fs.StringSliceVar(&c.Gateway.Hostnames, "gateway-hostnames", c.Gateway.Hostnames, "Optional hostnames for terminal HTTPRoutes")
}
