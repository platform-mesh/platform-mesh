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

	"go.yaml.in/yaml/v3"
)

// OCMConstructor is the part of a component-constructor.yaml the release commands read.
// Values are as written in the file, e.g. ${VAR} placeholders are not expanded.
type OCMConstructor struct {
	Components []OCMComponent `yaml:"components"`
}

// OCMComponent is a component a constructor declares.
type OCMComponent struct {
	Name       string         `yaml:"name"`
	Version    string         `yaml:"version"`
	Resources  []OCMResource  `yaml:"resources"`
	References []OCMReference `yaml:"componentReferences"`
}

// OCMResource is a resource of an OCMComponent.
type OCMResource struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

// OCMReference is a reference from an OCMComponent to another component.
type OCMReference struct {
	Name          string `yaml:"name"`
	ComponentName string `yaml:"componentName"`
	Version       string `yaml:"version"`
}

// ParseOCMConstructor parses the content of a component-constructor.yaml.
func ParseOCMConstructor(data []byte) (OCMConstructor, error) {
	var constructor OCMConstructor
	if err := yaml.Unmarshal(data, &constructor); err != nil {
		return OCMConstructor{}, fmt.Errorf("parsing OCM constructor: %w", err)
	}
	return constructor, nil
}
