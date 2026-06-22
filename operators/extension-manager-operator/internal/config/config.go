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

type ServerConfig struct {
	ServerPort string
}

func NewServerConfig() *ServerConfig {
	return &ServerConfig{
		ServerPort: "8088",
	}
}

func (c *ServerConfig) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.ServerPort, "server-port", c.ServerPort, "Set the server port")
}

type OperatorConfig struct {
	KCPAPIExportEndpointSliceName          string
	SubroutinesContentConfigurationEnabled bool
}

func NewOperatorConfig() *OperatorConfig {
	return &OperatorConfig{
		SubroutinesContentConfigurationEnabled: true,
	}
}

func (c *OperatorConfig) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.KCPAPIExportEndpointSliceName, "kcp-api-export-endpoint-slice-name", c.KCPAPIExportEndpointSliceName, "Optional APIExportEndpointSlice name to reconcile against")
	fs.BoolVar(&c.SubroutinesContentConfigurationEnabled, "subroutines-content-configuration-enabled", c.SubroutinesContentConfigurationEnabled, "Enable the content configuration subroutine")
}
