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

package migrate

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// setYAML sets values in a YAML document and returns it. Every key of set
// is a path, its segments separated by dots; mappings on the way that are
// not there are created. The rest of the document, its order and its
// comments stay as they are: a profile is a file people read and edit.
func setYAML(doc string, set map[string]any) (string, error) {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(doc), &root); err != nil {
		return "", err
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return "", errors.New("the document is not a mapping")
	}
	for _, path := range slices.Sorted(maps.Keys(set)) {
		var value yaml.Node
		if err := value.Encode(set[path]); err != nil {
			return "", fmt.Errorf("%s: %w", path, err)
		}
		node := root.Content[0]
		segments := strings.Split(path, ".")
		for i, segment := range segments {
			child := mappingValue(node, segment)
			last := i == len(segments)-1
			switch {
			case last && child != nil:
				// In place, so that the comments of the key stay.
				value.HeadComment, value.LineComment, value.FootComment = child.HeadComment, child.LineComment, child.FootComment
				*child = value
			case last:
				node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: segment}, &value)
			case child == nil:
				child = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: segment}, child)
				node = child
			case child.Kind != yaml.MappingNode:
				return "", fmt.Errorf("%s: %s is not a mapping", path, strings.Join(segments[:i+1], "."))
			default:
				node = child
			}
		}
	}

	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&root); err != nil {
		return "", err
	}
	if err := encoder.Close(); err != nil {
		return "", err
	}
	return out.String(), nil
}

// mappingValue returns the value of key in a mapping node, or nil.
func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}
