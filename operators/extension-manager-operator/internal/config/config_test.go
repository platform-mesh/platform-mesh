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
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
)

func TestNewServerConfig(t *testing.T) {
	cfg := NewServerConfig()

	assert.Equal(t, "8088", cfg.ServerPort)
}

func TestServerConfig_AddFlags(t *testing.T) {
	cfg := NewServerConfig()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg.AddFlags(fs)

	err := fs.Parse([]string{"--server-port=9090"})

	assert.NoError(t, err)
	assert.Equal(t, "9090", cfg.ServerPort)
}

func TestNewOperatorConfig(t *testing.T) {
	cfg := NewOperatorConfig()

	assert.Equal(t, "", cfg.KCPAPIExportEndpointSliceName)
	assert.True(t, cfg.SubroutinesContentConfigurationEnabled)
}

func TestOperatorConfig_AddFlags(t *testing.T) {
	cfg := NewOperatorConfig()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg.AddFlags(fs)

	err := fs.Parse([]string{
		"--kcp-api-export-endpoint-slice-name=custom.example.io",
		"--subroutines-content-configuration-enabled=false",
	})

	assert.NoError(t, err)
	assert.Equal(t, "custom.example.io", cfg.KCPAPIExportEndpointSliceName)
	assert.False(t, cfg.SubroutinesContentConfigurationEnabled)
}
