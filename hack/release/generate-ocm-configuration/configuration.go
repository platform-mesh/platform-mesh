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

package main

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"go.yaml.in/yaml/v3"
)

// ocmConfiguration preserves resource definitions, scalar types and key order as YAML nodes.
type ocmConfiguration struct {
	ImageResources yaml.Node `yaml:"imageResources"`
}

// serviceConfiguration associates a chart service name with its image definitions.
type serviceConfiguration struct {
	Name           string
	ImageResources yaml.Node
}

// loadServices joins services declared in values.yaml with component OCM definitions.
func loadServices(chartsDir string) ([]serviceConfiguration, error) {
	configurations, err := loadConfigurations(chartsDir)
	if err != nil {
		return nil, err
	}
	valuesPath := filepath.Join(chartsDir, operatorChart, "values.yaml")
	names, err := readServiceNames(valuesPath)
	if err != nil {
		return nil, err
	}

	// Every declared service needs image definitions, including services disabled by default.
	services := make([]serviceConfiguration, 0, len(names))
	for _, name := range names {
		configuration, ok := configurations[name]
		if !ok {
			return nil, fmt.Errorf("service %q in %s has no imageResources in an ocm.yaml", name, valuesPath)
		}
		services = append(services, serviceConfiguration{
			Name:           name,
			ImageResources: configuration.ImageResources,
		})
	}
	return services, nil
}

// readServiceNames includes every service, even when its default is false.
// Helm resolves enablement at install time.
func readServiceNames(path string) ([]string, error) {
	var values struct {
		OCM struct {
			Services map[string]bool `yaml:"services"`
		} `yaml:"ocm"`
	}
	if err := readYAML(path, &values); err != nil {
		return nil, err
	}
	if len(values.OCM.Services) == 0 {
		return nil, fmt.Errorf("%s has no ocm.services", path)
	}
	// Stable ordering keeps generated output reproducible.
	return slices.Sorted(maps.Keys(values.OCM.Services)), nil
}

// loadConfigurations collects image definitions from charts and third-party components.
func loadConfigurations(chartsDir string) (map[string]ocmConfiguration, error) {
	// Third-party directory names can differ from the service names used by profiles.
	sources := []struct {
		directory     string
		nameOverrides map[string]string
	}{
		{directory: chartsDir},
		{directory: "third_party", nameOverrides: map[string]string{"cloudnative-pg": "cnpg-operator"}},
	}

	// Missing directories and OCM files are optional; declared services are checked later.
	configurations := make(map[string]ocmConfiguration)
	for _, source := range sources {
		entries, err := os.ReadDir(source.directory)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", source.directory, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(source.directory, entry.Name(), "ocm.yaml")
			configuration, err := readConfiguration(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			// OCM files without image definitions do not contribute to this ConfigMap.
			if len(configuration.ImageResources.Content) == 0 {
				continue
			}
			name := entry.Name()
			if override, ok := source.nameOverrides[name]; ok {
				name = override
			}
			// Reject duplicate ownership instead of silently replacing a component's images.
			if _, exists := configurations[name]; exists {
				return nil, fmt.Errorf("%s: duplicate OCM configuration for service %q", path, name)
			}
			configurations[name] = configuration
		}
	}
	return configurations, nil
}

// readConfiguration validates the imageResources section of one OCM file.
func readConfiguration(path string) (ocmConfiguration, error) {
	var configuration ocmConfiguration
	if err := readYAML(path, &configuration); err != nil {
		return ocmConfiguration{}, err
	}
	// An absent or null section means the file has no images to include.
	resources := configuration.ImageResources
	if resources.Kind == 0 || resources.Tag == "!!null" {
		return ocmConfiguration{}, nil
	}
	if resources.Kind != yaml.SequenceNode {
		return ocmConfiguration{}, fmt.Errorf("%s: imageResources must be a YAML sequence", path)
	}
	return configuration, nil
}

// readYAML decodes a file while preserving its path in error messages.
func readYAML(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, value); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	return nil
}
