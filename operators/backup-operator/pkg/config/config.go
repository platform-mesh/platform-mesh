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
	"fmt"
	"strings"

	"github.com/spf13/pflag"
)

const DefaultNamespace = "platform-mesh"

type KcpConfig struct {
	ApiExportEndpointSliceName string
}

type OperatorConfig struct {
	Kcp        KcpConfig
	Namespace  string
	Standalone bool
}

func NewOperatorConfig() OperatorConfig {
	return OperatorConfig{
		Kcp: KcpConfig{
			ApiExportEndpointSliceName: "backup.platform-mesh.io",
		},
		Namespace: DefaultNamespace,
	}
}

func (c *OperatorConfig) Validate() error {
	if strings.TrimSpace(c.Namespace) == "" {
		return fmt.Errorf("--namespace must not be empty")
	}
	return nil
}

func (c *OperatorConfig) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.Kcp.ApiExportEndpointSliceName, "kcp-api-export-endpoint-slice-name", c.Kcp.ApiExportEndpointSliceName, "Set APIExportEndpointSlice name")
	fs.StringVar(&c.Namespace, "namespace", c.Namespace, "Namespace in which the operator manages resources")
	fs.BoolVar(&c.Standalone, "standalone", c.Standalone, "Run without KCP: watch the host cluster directly via a single-cluster provider")
}
