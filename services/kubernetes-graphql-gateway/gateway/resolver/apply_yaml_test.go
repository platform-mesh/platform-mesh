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

package resolver

import (
	"context"
	"testing"

	"github.com/graphql-go/graphql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// applyYamlFake is a minimal fake for ApplyYaml resolver tests.
// It records Create and Update calls so tests can assert what DryRun options
// were passed, without needing a full fake client.
type applyYamlFake struct {
	ctrlruntimeclient.WithWatch
	existing    *unstructured.Unstructured
	createCalls []ctrlruntimeclient.CreateOptions
	updateCalls []ctrlruntimeclient.UpdateOptions
}

func (f *applyYamlFake) Get(_ context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
	if f.existing == nil {
		return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
	}
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil
	}
	f.existing.DeepCopyInto(u)
	return nil
}

func (f *applyYamlFake) Create(_ context.Context, _ ctrlruntimeclient.Object, opts ...ctrlruntimeclient.CreateOption) error {
	o := &ctrlruntimeclient.CreateOptions{}
	o.ApplyOptions(opts)
	f.createCalls = append(f.createCalls, *o)
	return nil
}

func (f *applyYamlFake) Update(_ context.Context, _ ctrlruntimeclient.Object, opts ...ctrlruntimeclient.UpdateOption) error {
	o := &ctrlruntimeclient.UpdateOptions{}
	o.ApplyOptions(opts)
	f.updateCalls = append(f.updateCalls, *o)
	return nil
}

func existingConfigMap() *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("v1")
	u.SetKind("ConfigMap")
	u.SetName("test-cm")
	u.SetNamespace("default")
	u.SetResourceVersion("1")
	_ = unstructured.SetNestedField(u.Object, map[string]any{"key": "original"}, "data")
	return u
}

func TestApplyYamlResolver(t *testing.T) {
	const namedYAML = `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
  namespace: default
data:
  key: changed`

	const generateNameYAML = `apiVersion: v1
kind: ConfigMap
metadata:
  generateName: test-cm-
  namespace: default`

	t.Run("named/create: no dryRun calls Create without DryRun", func(t *testing.T) {
		fk := &applyYamlFake{}
		result, err := New(fk, nil).ApplyYaml()(graphql.ResolveParams{
			Context: t.Context(),
			Args:    map[string]any{YamlArg: namedYAML},
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, fk.createCalls, 1)
		assert.Empty(t, fk.createCalls[0].DryRun)
	})

	t.Run("named/create: dryRun=true calls Create with DryRun All", func(t *testing.T) {
		fk := &applyYamlFake{}
		result, err := New(fk, nil).ApplyYaml()(graphql.ResolveParams{
			Context: t.Context(),
			Args:    map[string]any{YamlArg: namedYAML, DryRunArg: true},
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, fk.createCalls, 1)
		assert.Equal(t, []string{"All"}, fk.createCalls[0].DryRun)
	})

	t.Run("named/update: no dryRun calls Update without DryRun", func(t *testing.T) {
		fk := &applyYamlFake{existing: existingConfigMap()}
		result, err := New(fk, nil).ApplyYaml()(graphql.ResolveParams{
			Context: t.Context(),
			Args:    map[string]any{YamlArg: namedYAML},
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, fk.updateCalls, 1)
		assert.Empty(t, fk.updateCalls[0].DryRun)
	})

	t.Run("named/update: dryRun=true calls Update with DryRun All", func(t *testing.T) {
		fk := &applyYamlFake{existing: existingConfigMap()}
		result, err := New(fk, nil).ApplyYaml()(graphql.ResolveParams{
			Context: t.Context(),
			Args:    map[string]any{YamlArg: namedYAML, DryRunArg: true},
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, fk.updateCalls, 1)
		assert.Equal(t, []string{"All"}, fk.updateCalls[0].DryRun)
	})

	t.Run("generateName: no dryRun calls Create without DryRun", func(t *testing.T) {
		fk := &applyYamlFake{}
		result, err := New(fk, nil).ApplyYaml()(graphql.ResolveParams{
			Context: t.Context(),
			Args:    map[string]any{YamlArg: generateNameYAML},
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, fk.createCalls, 1)
		assert.Empty(t, fk.createCalls[0].DryRun)
	})

	t.Run("generateName: dryRun=true calls Create with DryRun All", func(t *testing.T) {
		fk := &applyYamlFake{}
		result, err := New(fk, nil).ApplyYaml()(graphql.ResolveParams{
			Context: t.Context(),
			Args:    map[string]any{YamlArg: generateNameYAML, DryRunArg: true},
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, fk.createCalls, 1)
		assert.Equal(t, []string{"All"}, fk.createCalls[0].DryRun)
	})
}

func TestParseAndValidateYAML(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "valid single document",
			yaml: `apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
  namespace: default
data:
  key: value
`,
		},
		{
			name: "valid document starting with ---",
			yaml: `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
data:
  key: value
`,
		},
		{
			name:    "invalid YAML syntax",
			yaml:    `{not: valid: yaml: [`,
			wantErr: "invalid YAML",
		},
		{
			name: "missing apiVersion",
			yaml: `kind: ConfigMap
metadata:
  name: my-config
`,
			wantErr: "apiVersion is required",
		},
		{
			name: "missing kind",
			yaml: `apiVersion: v1
metadata:
  name: my-config
`,
			wantErr: "kind is required",
		},
		{
			name: "missing metadata",
			yaml: `apiVersion: v1
kind: ConfigMap
`,
			wantErr: "metadata is required",
		},
		{
			name: "missing metadata.name and metadata.generateName",
			yaml: `apiVersion: v1
kind: ConfigMap
metadata:
  namespace: default
`,
			wantErr: "metadata.name or metadata.generateName is required",
		},
		{
			name: "valid with generateName instead of name",
			yaml: `apiVersion: v1
kind: ConfigMap
metadata:
  generateName: my-config-
  namespace: default
data:
  key: value
`,
		},
		{
			name: "multi-document YAML",
			yaml: `apiVersion: v1
kind: ConfigMap
metadata:
  name: first
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: second
`,
			wantErr: "multi-document YAML is not supported",
		},
		{
			name: "multi-document with leading separator",
			yaml: `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: first
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: second
`,
			wantErr: "multi-document YAML is not supported",
		},
		{
			name:    "empty YAML",
			yaml:    "",
			wantErr: "invalid YAML",
		},
		{
			name: "grouped apiVersion",
			yaml: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-deploy
spec:
  replicas: 1
`,
		},
		{
			name: "cluster-scoped resource without namespace",
			yaml: `apiVersion: v1
kind: Namespace
metadata:
  name: my-namespace
`,
		},
		{
			name: "non-string apiVersion",
			yaml: `apiVersion: 123
kind: ConfigMap
metadata:
  name: my-config
`,
			wantErr: "apiVersion is required and must be a string",
		},
		{
			name: "non-string kind",
			yaml: `apiVersion: v1
kind: 456
metadata:
  name: my-config
`,
			wantErr: "kind is required and must be a string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseAndValidateYAML(tt.yaml)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)

				// Verify parsed content preserves key fields
				assert.NotEmpty(t, result["apiVersion"])
				assert.NotEmpty(t, result["kind"])
				metadata, ok := result["metadata"].(map[string]any)
				require.True(t, ok)
				assert.True(t, metadata["name"] != nil || metadata["generateName"] != nil)
			}
		})
	}
}

func TestParseAndValidateYAML_ParsedContent(t *testing.T) {
	yamlStr := `apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
  namespace: default
  labels:
    app: test
data:
  key1: value1
  key2: value2
`
	result, err := parseAndValidateYAML(yamlStr)
	require.NoError(t, err)

	assert.Equal(t, "v1", result["apiVersion"])
	assert.Equal(t, "ConfigMap", result["kind"])

	metadata := result["metadata"].(map[string]any)
	assert.Equal(t, "my-config", metadata["name"])
	assert.Equal(t, "default", metadata["namespace"])

	labels := metadata["labels"].(map[string]any)
	assert.Equal(t, "test", labels["app"])

	data := result["data"].(map[string]any)
	assert.Equal(t, "value1", data["key1"])
	assert.Equal(t, "value2", data["key2"])
}
