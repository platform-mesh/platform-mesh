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

func TestNewOperatorConfig(t *testing.T) {
	cfg := NewOperatorConfig()

	assert.Equal(t, "platform-mesh-system", cfg.KCP.Namespace)
	assert.Equal(t, "root", cfg.KCP.RootShardName)
	assert.Equal(t, "frontproxy", cfg.KCP.FrontProxyName)
	assert.Equal(t, "8443", cfg.KCP.FrontProxyPort)
	assert.Equal(t, "kcp-cluster-admin-client-cert", cfg.KCP.ClusterAdminSecretName)
}

func TestOperatorConfigAddFlags(t *testing.T) {
	cfg := NewOperatorConfig()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg.AddFlags(fs)

	err := fs.Parse([]string{
		"--kcp-url=https://kcp.example.local",
		"--kcp-namespace=custom-ns",
		"--kcp-root-shard-name=custom-root",
		"--kcp-front-proxy-name=custom-proxy",
		"--kcp-front-proxy-port=7443",
		"--kcp-cluster-admin-secret-name=custom-admin-secret",
	})

	assert.NoError(t, err)
	assert.Equal(t, "https://kcp.example.local", cfg.KCP.Url)
	assert.Equal(t, "custom-ns", cfg.KCP.Namespace)
	assert.Equal(t, "custom-root", cfg.KCP.RootShardName)
	assert.Equal(t, "custom-proxy", cfg.KCP.FrontProxyName)
	assert.Equal(t, "7443", cfg.KCP.FrontProxyPort)
	assert.Equal(t, "custom-admin-secret", cfg.KCP.ClusterAdminSecretName)
}
