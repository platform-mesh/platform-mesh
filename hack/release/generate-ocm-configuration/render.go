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
	"bytes"
	"fmt"
	"text/template"

	"go.yaml.in/yaml/v3"

	_ "embed"
)

//go:embed ocm.yaml.gotmpl
var configMapTemplate string

// Unique service markers let YAML encoding finish before unquoted Helm expressions are inserted.
// The index keeps markers distinct; they are replaced before the generated template is written.
const enabledPlaceholderFormat = "__PM_OCM_ENABLED_%d__"

// serviceDocument is one entry in the YAML consumed by the operator.
type serviceDocument struct {
	Enabled        string    `yaml:"enabled"`
	ImageResources yaml.Node `yaml:"imageResources"`
}

// configMapDocument wraps the service YAML in a Kubernetes ConfigMap.
type configMapDocument struct {
	APIVersion string               `yaml:"apiVersion"`
	Kind       string               `yaml:"kind"`
	Metadata   map[string]string    `yaml:"metadata"`
	Data       map[string]yaml.Node `yaml:"data"`
}

// renderConfigMap serializes the service data without reading or writing files.
func renderConfigMap(services []serviceConfiguration) ([]byte, error) {
	// Helm resolves each OCM service's enabled flag from .Values.ocm.services.
	configuration := make(map[string]serviceDocument, len(services))
	for index, service := range services {
		configuration[service.Name] = serviceDocument{
			// Plain placeholders keep the encoder from quoting Helm expressions.
			Enabled:        fmt.Sprintf(enabledPlaceholderFormat, index),
			ImageResources: service.ImageResources,
		}
	}
	// Serialize the service document before embedding it as ConfigMap data.
	serviceData, err := encodeYAML(configuration)
	if err != nil {
		return nil, fmt.Errorf("encoding OCM services: %w", err)
	}
	// Literal style lets the YAML encoder handle indentation of the embedded document.
	document := configMapDocument{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Metadata: map[string]string{
			"name":      "platform-mesh-ocm-configuration",
			"namespace": "platform-mesh-system",
		},
		Data: map[string]yaml.Node{
			"ocm-configuration.yaml": {
				Kind:  yaml.ScalarNode,
				Tag:   "!!str",
				Style: yaml.LiteralStyle,
				Value: string(serviceData),
			},
		},
	}
	data, err := encodeYAML(document)
	if err != nil {
		return nil, fmt.Errorf("encoding ConfigMap: %w", err)
	}

	// Expand template placeholders after YAML encoding has handled indentation.
	data, err = expandEnablementExpressions(data, services)
	if err != nil {
		return nil, err
	}

	// The wrapper adds the Helm guard and license header to the encoded YAML.
	// Alternate delimiters leave Helm expressions untouched.
	tmpl, err := template.New("ocm.yaml").Delims("[[", "]]").Parse(configMapTemplate)
	if err != nil {
		return nil, fmt.Errorf("parsing ConfigMap template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, string(data)); err != nil {
		return nil, fmt.Errorf("rendering ConfigMap template: %w", err)
	}
	return buf.Bytes(), nil
}

// expandEnablementExpressions inserts unquoted Helm expressions into the encoded YAML.
func expandEnablementExpressions(data []byte, services []serviceConfiguration) ([]byte, error) {
	for index, service := range services {
		placeholder := []byte(fmt.Sprintf(enabledPlaceholderFormat, index))
		// A unique occurrence prevents replacement from changing image resource data.
		if bytes.Count(data, placeholder) != 1 {
			return nil, fmt.Errorf("service %q: enablement placeholder must occur exactly once", service.Name)
		}
		expression := fmt.Sprintf("{{ index .Values.ocm.services %q }}", service.Name)
		data = bytes.Replace(data, placeholder, []byte(expression), 1)
	}
	return data, nil
}

// encodeYAML uses consistent formatting for the service document and its ConfigMap.
func encodeYAML(value any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
