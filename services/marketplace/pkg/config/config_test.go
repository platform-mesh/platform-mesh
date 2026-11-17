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

func TestNewDefaults(t *testing.T) {
	t.Parallel()

	cfg := New()

	require.Equal(t, "ui.platform-mesh.io/content-for", cfg.ContentForLabel)
	require.Equal(t, "", cfg.APIExportEndpointSliceName)
}

func TestAddFlagsParsesValues(t *testing.T) {
	t.Parallel()

	cfg := New()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg.AddFlags(fs)

	err := fs.Parse([]string{
		"--content-for-label=custom.content/for",
		"--resource-apiexport-endpointslice-name=ui.platform-mesh.io",
	})
	require.NoError(t, err)

	require.Equal(t, "custom.content/for", cfg.ContentForLabel)
	require.Equal(t, "ui.platform-mesh.io", cfg.APIExportEndpointSliceName)
	require.Empty(t, fs.Args())
}
