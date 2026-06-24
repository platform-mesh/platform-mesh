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

package transform

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	sprig "github.com/go-task/slim-sprig/v3"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

const copyFieldPrefix = "__COPY_FIELD__:"

// TemplateData provides context for template evaluation
type TemplateData struct {
	Source map[string]any // Full unstructured source resource
}

// NewTemplateData creates TemplateData from an unstructured resource
func NewTemplateData(source *unstructured.Unstructured) TemplateData {
	return TemplateData{
		Source: source.Object,
	}
}

// customFuncs returns custom template functions for resource transformation
func customFuncs() template.FuncMap {
	return template.FuncMap{
		// getField safely gets a nested field from a map using dot notation
		// Example: getField .Source "spec.displayName"
		"getField": func(obj map[string]any, path string) any {
			parts := strings.Split(path, ".")
			current := any(obj)
			for _, part := range parts {
				if m, ok := current.(map[string]any); ok {
					current = m[part]
				} else {
					return nil
				}
			}
			return current
		},
		// getFieldStr safely gets a nested field as string
		"getFieldStr": func(obj map[string]any, path string) string {
			parts := strings.Split(path, ".")
			current := any(obj)
			for _, part := range parts {
				if m, ok := current.(map[string]any); ok {
					current = m[part]
				} else {
					return ""
				}
			}
			if s, ok := current.(string); ok {
				return s
			}
			return ""
		},
		// copyFrom marks a field to be copied directly from source
		// Example: spec: {{ copyFrom "spec" }}
		// The value will be replaced with the actual source field after template processing
		"copyFrom": func(path string) string {
			return copyFieldPrefix + path
		},
	}
}

// getNestedField retrieves a nested field from a map using dot notation
func getNestedField(obj map[string]any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	current := any(obj)
	for _, part := range parts {
		if m, ok := current.(map[string]any); ok {
			current, ok = m[part]
			if !ok {
				return nil, false
			}
		} else {
			return nil, false
		}
	}
	return current, true
}

// resolveCopyFromMarkers walks the parsed YAML structure and replaces copyFrom markers
// with actual values from the source
func resolveCopyFromMarkers(obj map[string]any, source map[string]any) {
	for key, value := range obj {
		switch v := value.(type) {
		case string:
			if strings.HasPrefix(v, copyFieldPrefix) {
				path := strings.TrimPrefix(v, copyFieldPrefix)
				if sourceValue, ok := getNestedField(source, path); ok {
					obj[key] = sourceValue
				}
			}
		case map[string]any:
			resolveCopyFromMarkers(v, source)
		case []any:
			for i, item := range v {
				if m, ok := item.(map[string]any); ok {
					resolveCopyFromMarkers(m, source)
				} else if s, ok := item.(string); ok && strings.HasPrefix(s, copyFieldPrefix) {
					path := strings.TrimPrefix(s, copyFieldPrefix)
					if sourceValue, found := getNestedField(source, path); found {
						v[i] = sourceValue
					}
				}
			}
		}
	}
}

// EvaluateTemplate evaluates a Go template string with the given data
// Template has access to .Source which contains the full unstructured resource
// Example: "root:orgs:{{ .Source.metadata.namespace }}" or "{{ .Source.metadata.labels.org }}"
func EvaluateTemplate(tmpl string, data TemplateData) (string, error) {
	if tmpl == "" {
		return "", fmt.Errorf("template cannot be empty")
	}

	// Create template with sprig functions and custom functions
	funcMap := sprig.FuncMap()
	for k, v := range customFuncs() {
		funcMap[k] = v
	}

	t, err := template.New("tmpl").Funcs(funcMap).Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	result := buf.String()
	if result == "" {
		return "", fmt.Errorf("template evaluation resulted in empty string")
	}

	return result, nil
}

// EvaluateWorkspaceExpression evaluates a workspace expression template
// This is a convenience wrapper around EvaluateTemplate for workspace path evaluation
func EvaluateWorkspaceExpression(expression string, source *unstructured.Unstructured) (string, error) {
	data := NewTemplateData(source)
	return EvaluateTemplate(expression, data)
}

// ApplyTemplate transforms a source resource using a Go template that outputs YAML
// The template receives .Source which contains the full unstructured source resource
// and should output valid YAML for the target resource.
//
// Example template:
//
//	apiVersion: core.platform-mesh.io/v1alpha1
//	kind: Account
//	metadata:
//	  name: "{{ index .Source.metadata "name" }}"
//	spec:
//	  type: "project"
//	  displayName: "{{ getFieldStr .Source "spec.displayName" }}"
func ApplyTemplate(source *unstructured.Unstructured, tmpl string) (*unstructured.Unstructured, error) {
	if tmpl == "" {
		return nil, fmt.Errorf("template cannot be empty")
	}

	data := NewTemplateData(source)

	// Create template with sprig functions and custom functions
	funcMap := sprig.FuncMap()
	for k, v := range customFuncs() {
		funcMap[k] = v
	}

	t, err := template.New("transform").Funcs(funcMap).Parse(tmpl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse transformation template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to execute transformation template: %w", err)
	}

	output := buf.String()
	if strings.TrimSpace(output) == "" {
		return nil, fmt.Errorf("transformation template produced empty output")
	}

	// Parse YAML output to unstructured
	target := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(buf.Bytes(), &target.Object); err != nil {
		return nil, fmt.Errorf("failed to parse template output as YAML: %w", err)
	}

	// Resolve copyFrom markers with actual source values
	resolveCopyFromMarkers(target.Object, source.Object)

	// Add migration tracking annotations
	annotations := target.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations["migration.platform-mesh.io/source-uid"] = string(source.GetUID())
	annotations["migration.platform-mesh.io/source-name"] = source.GetName()
	annotations["migration.platform-mesh.io/source-namespace"] = source.GetNamespace()
	target.SetAnnotations(annotations)

	return target, nil
}
