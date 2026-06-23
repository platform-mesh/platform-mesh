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

package roles

import (
	"fmt"
	"os"
	"strings"

	"go.platform-mesh.io/golang-commons/errors"
	"gopkg.in/yaml.v3"

	"go.platform-mesh.io/iam-service/pkg/graph"
)

// RoleDefinition represents a single role definition
type RoleDefinition struct {
	ID          string `yaml:"id"`
	DisplayName string `yaml:"displayName"`
	Description string `yaml:"description"`
}

// GroupResourceRoles represents roles for a specific group resource
type GroupResourceRoles struct {
	GroupResource string           `yaml:"groupResource"`
	Roles         []RoleDefinition `yaml:"roles"`
}

// RolesConfig represents the entire roles configuration
type RolesConfig struct {
	Roles []GroupResourceRoles `yaml:"roles"`
}

// RolesRetriever interface for retrieving roles
type RolesRetriever interface {
	GetRoleDefinitions(resourceContext graph.ResourceContext) ([]RoleDefinition, error)
}

// FileBasedRolesRetriever implements RolesRetriever by reading from a YAML file
type FileBasedRolesRetriever struct {
	filePath string
	config   *RolesConfig
}

// NewFileBasedRolesRetriever creates a new file-based roles retriever
func NewFileBasedRolesRetriever(filePath string) (*FileBasedRolesRetriever, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read roles file %s", filePath)
	}

	var config RolesConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal roles YAML from file %s", filePath)
	}

	retriever := &FileBasedRolesRetriever{
		filePath: filePath,
		config:   &config,
	}
	return retriever, nil
}

// GetRoleDefinitions returns the full role definitions for a given group resource
func (r *FileBasedRolesRetriever) GetRoleDefinitions(rctx graph.ResourceContext) ([]RoleDefinition, error) {
	if r.config == nil {
		return nil, errors.New("roles configuration not loaded")
	}

	for _, groupRoles := range r.config.Roles {
		groupResource := strings.TrimPrefix(fmt.Sprintf("%s/%s", rctx.Group, rctx.Kind), "/")
		if groupRoles.GroupResource == groupResource {
			return groupRoles.Roles, nil
		}
	}

	// Return empty slice if no roles found for this group resource
	return []RoleDefinition{}, nil
}

// GetAvailableRoleIDs is a helper function that extracts role IDs from role definitions
func GetAvailableRoleIDs(roleDefinitions []RoleDefinition) []string {
	roleIDs := make([]string, len(roleDefinitions))
	for i, role := range roleDefinitions {
		roleIDs[i] = role.ID
	}
	return roleIDs
}
