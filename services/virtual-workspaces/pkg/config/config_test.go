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
	"github.com/stretchr/testify/require"
)

func TestNewServiceConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg := NewServiceConfig()

	require.Equal(t, "", cfg.Kubeconfig)
	require.Equal(t, "", cfg.ServerURL)
	require.Equal(t, "ui.platform-mesh.ui/entity", cfg.EntityLabel)
	require.Equal(t, "ui.platform-mesh.io/content-for", cfg.ContentForLabel)
	require.Equal(t, "main", cfg.MainEntityName)
	require.Equal(t, "core_platform-mesh_io_account", cfg.AccountEntityName)
	require.Equal(t, "v250704-6d57f16.contentconfigurations.ui.platform-mesh.io", cfg.ResourceSchemaName)
	require.Equal(t, "root:openmfp-system", cfg.ResourceSchemaWorkspace)
	require.Equal(t, "", cfg.ResourceAPIExportEndpointSliceName)
}

func TestServiceConfigAddFlagsParsesValues(t *testing.T) {
	t.Parallel()

	cfg := NewServiceConfig()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg.AddFlags(fs)

	err := fs.Parse([]string{
		"--kubeconfig=/tmp/kubeconfig",
		"--server-url=https://127.0.0.1:8443",
		"--entity-label=custom.entity/label",
		"--content-for-label=custom.content/for",
		"--main-entity-name=home",
		"--account-entity-name=core_platform-mesh_io_customer",
		"--resource-schema-name=v1.contentconfigurations.ui.platform-mesh.io",
		"--resource-schema-workspace=root:orgs",
		"--resource-apiexport-endpointslice-name=ui.platform-mesh.io",
	})
	require.NoError(t, err)

	require.Equal(t, "/tmp/kubeconfig", cfg.Kubeconfig)
	require.Equal(t, "https://127.0.0.1:8443", cfg.ServerURL)
	require.Equal(t, "custom.entity/label", cfg.EntityLabel)
	require.Equal(t, "custom.content/for", cfg.ContentForLabel)
	require.Equal(t, "home", cfg.MainEntityName)
	require.Equal(t, "core_platform-mesh_io_customer", cfg.AccountEntityName)
	require.Equal(t, "v1.contentconfigurations.ui.platform-mesh.io", cfg.ResourceSchemaName)
	require.Equal(t, "root:orgs", cfg.ResourceSchemaWorkspace)
	require.Equal(t, "ui.platform-mesh.io", cfg.ResourceAPIExportEndpointSliceName)
	require.Empty(t, fs.Args())
}
