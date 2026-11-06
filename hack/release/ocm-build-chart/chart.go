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
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// unversioned is the Chart.yaml appVersion of charts whose image is built in this repository.
const unversioned = "0.0.0"

// chart is the part of Chart.yaml the command reads.
type chart struct {
	AppVersion   string `yaml:"appVersion"`
	Dependencies []struct {
		Repository string `yaml:"repository"`
	} `yaml:"dependencies"`
}

func readChart(dir string) (chart, error) {
	var meta chart
	err := readYAML(filepath.Join(dir, "Chart.yaml"), &meta)
	return meta, err
}

// appVersion resolves the appVersion the archive and the constructor get.
// override wins, an unversioned chart gets the release's version.
func appVersion(override, chartAppVersion, version string) string {
	if override != "" {
		return override
	}
	if chartAppVersion == unversioned {
		return "v" + strings.TrimPrefix(version, "v")
	}
	return chartAppVersion
}

// readChartEnv resolves charts/<chart>/ocm.yaml, the variables the chart's
// constructor reads beyond the globals, each mapped to a path in values.yaml.
func readChartEnv(dir string) (map[string]string, error) {
	var mapping struct {
		Envsubst map[string]string `yaml:"envsubst"`
	}
	err := readYAML(filepath.Join(dir, "ocm.yaml"), &mapping)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var values map[string]any
	if err := readYAML(filepath.Join(dir, "values.yaml"), &values); err != nil {
		return nil, err
	}

	env := make(map[string]string, len(mapping.Envsubst))
	for name, path := range mapping.Envsubst {
		value, err := lookup(values, path)
		if err != nil {
			return nil, fmt.Errorf("%s/ocm.yaml: %s: %w", dir, name, err)
		}
		env[name] = value
	}

	return env, nil
}

// lookup returns the scalar at a dotted path such as image.tag.
func lookup(values map[string]any, path string) (string, error) {
	node, found, err := unstructured.NestedFieldNoCopy(values, strings.Split(path, ".")...)
	if err != nil {
		return "", fmt.Errorf("reading %s in values.yaml: %w", path, err)
	}

	if !found || node == nil || node == "" {
		return "", fmt.Errorf("nothing at %s in values.yaml", path)
	}

	switch node.(type) {
	case map[string]any, []any:
		return "", fmt.Errorf("%s in values.yaml cannot be used as a version", path)
	default:
		return fmt.Sprint(node), nil
	}
}

func readYAML(path string, into any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, into); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	return nil
}
