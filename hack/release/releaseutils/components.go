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

package releaseutils

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	// ComponentsFile is the registry of the components built from this
	// repository, relative to the repository root.
	ComponentsFile = "ocm/components.yaml"
	// SkipComponentsEnv names the optional components a build leaves out,
	// separated by commas.
	SkipComponentsEnv = "SKIP_COMPONENTS"
)

// Component is an entry of ocm/components.yaml.
type Component struct {
	Name    string `yaml:"-"`
	Path    string `yaml:"path"`
	Chart   string `yaml:"chart"`
	Image   string `yaml:"image"`
	Library bool   `yaml:"library"`
	// Optional components may be left out of a release.
	Optional bool `yaml:"optional"`
}

// LoadComponents reads ocm/components.yaml; run from the repository root.
func LoadComponents() (map[string]Component, error) {
	data, err := os.ReadFile(ComponentsFile)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Components map[string]Component `yaml:"components"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", ComponentsFile, err)
	}
	for name, c := range doc.Components {
		c.Name = name
		doc.Components[name] = c
	}
	return doc.Components, nil
}

// SkippedComponents returns the components SKIP_COMPONENTS leaves out of this
// build, by name. Only a component marked optional can be skipped: everything
// else is part of every release.
func SkippedComponents() (map[string]Component, error) {
	names := ParseList(os.Getenv(SkipComponentsEnv))
	if len(names) == 0 {
		return nil, nil
	}
	components, err := LoadComponents()
	if err != nil {
		return nil, err
	}
	return selectOptional(components, names)
}

// ParseList splits a comma-separated list, dropping blanks.
func ParseList(list string) []string {
	var out []string
	for item := range strings.SplitSeq(list, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func selectOptional(components map[string]Component, names []string) (map[string]Component, error) {
	skipped := make(map[string]Component, len(names))
	for _, name := range names {
		c, ok := components[name]
		if !ok {
			return nil, fmt.Errorf("%s: %q is not a component in %s", SkipComponentsEnv, name, ComponentsFile)
		}
		if !c.Optional {
			optional := make([]string, 0, len(components))
			for n, c := range components {
				if c.Optional {
					optional = append(optional, n)
				}
			}
			slices.Sort(optional)
			return nil, fmt.Errorf("%s: %q is not optional and cannot be skipped (optional: %s)", SkipComponentsEnv, name, strings.Join(optional, ", "))
		}
		skipped[name] = c
	}
	return skipped, nil
}
