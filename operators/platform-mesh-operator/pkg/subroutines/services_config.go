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

package subroutines

import (
	"os"

	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/pkg/merge"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

type serviceEnablementConfig struct {
	Services map[string]*serviceEnablement `json:"services"`
}

type serviceEnablement struct {
	Enabled *bool `json:"enabled"`
}

// applyServiceEnablementFromFile reads the mounted configuration for each render.
// It overrides enabled flags only for services present in the merged profile/spec.
func applyServiceEnablementFromFile(services map[string]any, path string, log *logger.Logger) (map[string]any, error) {
	if path == "" {
		return services, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to read service configuration file %s", path)
	}
	var config serviceEnablementConfig
	if err := yaml.UnmarshalStrict(raw, &config); err != nil {
		return nil, errors.New("Failed to parse service configuration file %s; expected a services map with boolean enabled fields", path)
	}
	if config.Services == nil {
		return nil, errors.New("Service configuration file %s must contain a services map", path)
	}
	// Validate the entire file before changing any service settings.
	for name, service := range config.Services {
		if service == nil || service.Enabled == nil {
			return nil, errors.New("Service configuration file %s: services.%s.enabled must be a boolean", path, name)
		}
	}
	var configuredServices map[string]*serviceEnablement
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(services, &configuredServices); err != nil {
		return nil, errors.Wrap(err, "Failed to decode service enablement configuration")
	}
	overrides := make(map[string]*serviceEnablement)
	for name, setting := range config.Services {
		service := configuredServices[name]
		if service == nil {
			continue
		}
		service.Enabled = setting.Enabled
		overrides[name] = service
	}
	resolved, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&overrides)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to serialize service enablement configuration")
	}
	// Merge only enablement overrides so arbitrary service settings remain intact.
	return merge.MergeMaps(services, resolved, log)
}
