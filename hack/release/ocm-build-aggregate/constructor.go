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
	"fmt"
	"os"
	"slices"

	"go.yaml.in/yaml/v3"

	"go.platform-mesh.io/platform-mesh/hack/release/releaseutils"
)

// constructorFile is a component-constructor.yaml, as far as this command
// changes it. Everything else is kept in Rest as the YAML it was written as,
// so writing the file back loses nothing and no value changes its type: the
// ${VAR} placeholders ocm expands stay as they are, and an unquoted 1.20 does
// not become 1.2. Comments and the order of the keys are not kept.
type constructorFile struct {
	Components []constructorComponent `yaml:"components"`
	Rest       map[string]yaml.Node   `yaml:",inline"`
}

// constructorComponent is one component of a constructorFile.
type constructorComponent struct {
	References []componentReference `yaml:"componentReferences,omitempty"`
	Rest       map[string]yaml.Node `yaml:",inline"`
}

// componentReference is a reference from one component to another.
type componentReference struct {
	Name string               `yaml:"name"`
	Rest map[string]yaml.Node `yaml:",inline"`
}

// withoutReferences returns the constructor without the component references
// named after one of the skipped components, and the names it dropped.
func withoutReferences(constructor []byte, skipped map[string]releaseutils.Component) ([]byte, []string, error) {
	var file constructorFile
	if err := yaml.Unmarshal(constructor, &file); err != nil {
		return nil, nil, err
	}

	var dropped []string
	for i := range file.Components {
		component := &file.Components[i]
		component.References = slices.DeleteFunc(component.References, func(ref componentReference) bool {
			_, skip := skipped[ref.Name]
			if skip {
				dropped = append(dropped, ref.Name)
			}
			return skip
		})
	}

	out, err := yaml.Marshal(&file)
	if err != nil {
		return nil, nil, err
	}
	return out, dropped, nil
}

// selectedConstructor returns the path of the constructor to build: the one
// in the repository, or, when components are skipped, a copy without the
// references to them, which cleanup removes.
func selectedConstructor(skipped map[string]releaseutils.Component) (path string, dropped []string, cleanup func(), err error) {
	if len(skipped) == 0 {
		return constructor, nil, func() {}, nil
	}
	data, err := os.ReadFile(constructor)
	if err != nil {
		return "", nil, nil, err
	}
	filtered, dropped, err := withoutReferences(data, skipped)
	if err != nil {
		return "", nil, nil, fmt.Errorf("parsing %s: %w", constructor, err)
	}
	tmp, err := os.CreateTemp("", "component-constructor-*.yaml")
	if err != nil {
		return "", nil, nil, err
	}
	cleanup = func() { _ = os.Remove(tmp.Name()) }
	if _, err := tmp.Write(filtered); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", nil, nil, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", nil, nil, err
	}
	return tmp.Name(), dropped, cleanup, nil
}
