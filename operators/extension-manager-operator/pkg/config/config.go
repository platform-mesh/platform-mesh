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

	platformmeshconfig "go.platform-mesh.io/golang-commons/config"
)

type ServerConfig struct {
	ServerPort                  string
	EntityTypeValidationEnabled bool
}

func NewServerConfig() *ServerConfig {
	return &ServerConfig{
		ServerPort:                  "8088",
		EntityTypeValidationEnabled: false,
	}
}

func (c *ServerConfig) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.ServerPort, "server-port", c.ServerPort, "Set the server port")
	fs.BoolVar(&c.EntityTypeValidationEnabled, "entity-type-validation-enabled", c.EntityTypeValidationEnabled, "Enable entityType validation for ContentConfigurations")
}

type OperatorConfig struct {
	KCPAPIExportEndpointSliceName          string
	SubroutinesContentConfigurationEnabled bool
	EntityTypeValidationEnabled            bool
}

func NewOperatorConfig() *OperatorConfig {
	return &OperatorConfig{
		SubroutinesContentConfigurationEnabled: true,
		EntityTypeValidationEnabled:            false,
	}
}

func (c *OperatorConfig) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.KCPAPIExportEndpointSliceName, "kcp-api-export-endpoint-slice-name", c.KCPAPIExportEndpointSliceName, "Optional APIExportEndpointSlice name to reconcile against")
	fs.BoolVar(&c.SubroutinesContentConfigurationEnabled, "subroutines-content-configuration-enabled", c.SubroutinesContentConfigurationEnabled, "Enable the content configuration subroutine")
	fs.BoolVar(&c.EntityTypeValidationEnabled, "entity-type-validation-enabled", c.EntityTypeValidationEnabled, "Enable entityType validation for ContentConfigurations")
}

// Config is everything the operator command is configured with: the flags shared by all
// Platform Mesh services plus the operator's own flags.
type Config struct {
	// Common holds the shared service flags (metrics, health probes, leader election,
	// concurrency, ...). nil is treated as the zero value.
	Common *platformmeshconfig.CommonServiceConfig
	// Operator holds the extension-manager-operator flags. nil means NewOperatorConfig().
	Operator *OperatorConfig
}
