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

// Config is the configuration of the marketplace virtual workspace.
type Config struct {
	// ContentForLabel is the label on an APIExport naming the ProviderMetadata
	// it belongs to.
	ContentForLabel string
	// APIExportEndpointSliceName is the APIExportEndpointSlice through which
	// the ProviderMetadata and APIExports of all providers are read.
	APIExportEndpointSliceName string
}

func New() Config {
	return Config{
		ContentForLabel: "ui.platform-mesh.io/content-for",
	}
}

func (c *Config) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.ContentForLabel, "content-for-label", c.ContentForLabel, "Set the content-for label")
	fs.StringVar(
		&c.APIExportEndpointSliceName,
		"resource-apiexport-endpointslice-name",
		c.APIExportEndpointSliceName,
		"Set the resource APIExport EndpointSlice name",
	)
}
