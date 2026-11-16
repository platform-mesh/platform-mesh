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

// Config is the configuration of the contentconfigurations virtual workspace.
type Config struct {
	EntityLabel       string
	ContentForLabel   string
	MainEntityName    string
	AccountEntityName string

	// ResourceSchemaName and ResourceSchemaWorkspace locate the
	// APIResourceSchema of ContentConfiguration the workspace serves.
	ResourceSchemaName      string
	ResourceSchemaWorkspace string
}

func New() Config {
	return Config{
		EntityLabel:             "ui.platform-mesh.ui/entity",
		ContentForLabel:         "ui.platform-mesh.io/content-for",
		MainEntityName:          "main",
		AccountEntityName:       "core_platform-mesh_io_account",
		ResourceSchemaName:      "v250704-6d57f16.contentconfigurations.ui.platform-mesh.io",
		ResourceSchemaWorkspace: "root:openmfp-system",
	}
}

func (c *Config) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.EntityLabel, "entity-label", c.EntityLabel, "Set the entity label")
	fs.StringVar(&c.ContentForLabel, "content-for-label", c.ContentForLabel, "Set the content-for label")
	fs.StringVar(&c.MainEntityName, "main-entity-name", c.MainEntityName, "Set the main entity name")
	fs.StringVar(&c.AccountEntityName, "account-entity-name", c.AccountEntityName, "Set the account entity name")
	fs.StringVar(&c.ResourceSchemaName, "resource-schema-name", c.ResourceSchemaName, "Set the resource schema name")
	fs.StringVar(&c.ResourceSchemaWorkspace, "resource-schema-workspace", c.ResourceSchemaWorkspace, "Set the resource schema workspace")
}
