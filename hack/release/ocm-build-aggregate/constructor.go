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

	"go.yaml.in/yaml/v3"

	"go.platform-mesh.io/platform-mesh/hack/release/releaseutils"
)

// withoutReferences returns the constructor without the component references
// named after one of the skipped components, and the names it dropped. The
// rest of the document is kept as it is written: versions stay the strings
// they are.
func withoutReferences(constructor []byte, skipped map[string]releaseutils.Component) ([]byte, []string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(constructor, &doc); err != nil {
		return nil, nil, err
	}

	var dropped []string
	if len(doc.Content) > 0 {
		components := mappingValue(doc.Content[0], "components")
		if components != nil {
			for _, component := range components.Content {
				references := mappingValue(component, "componentReferences")
				if references == nil {
					continue
				}
				kept := references.Content[:0]
				for _, reference := range references.Content {
					name := mappingValue(reference, "name")
					if name != nil {
						if _, skip := skipped[name.Value]; skip {
							dropped = append(dropped, name.Value)
							continue
						}
					}
					kept = append(kept, reference)
				}
				references.Content = kept
			}
		}
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, nil, err
	}
	return out, dropped, nil
}

// mappingValue returns the value of key in a mapping node, or nil.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
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
