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

package subroutines

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/platform-mesh-operator/gotemplates"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type DeploymentFuncsTestSuite struct {
	suite.Suite
}

func TestDeploymentFuncsTestSuite(t *testing.T) {
	suite.Run(t, new(DeploymentFuncsTestSuite))
}

func (s *DeploymentFuncsTestSuite) Test_renderTemplatesInValue_StringWithTemplate() {
	templateData := map[string]any{
		"namespace": "prod",
		"config": map[string]any{
			"clusterName": "cluster-1",
			"syncWave":    "10",
		},
	}

	tests := []struct {
		name     string
		input    any
		expected any
	}{
		{
			name:     "simple template",
			input:    "ns-{{ .namespace }}",
			expected: "ns-prod",
		},
		{
			name:     "nested map access",
			input:    "wave-{{ .config.syncWave }}",
			expected: "wave-10",
		},
		{
			name:     "multiple templates in string",
			input:    "{{ .namespace }}-{{ .config.clusterName }}",
			expected: "prod-cluster-1",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result, err := renderTemplatesInValue(tt.input, templateData)
			s.NoError(err)
			s.Equal(tt.expected, result)
		})
	}
}

func (s *DeploymentFuncsTestSuite) Test_renderTemplatesInValue_StringWithoutTemplate() {
	templateData := map[string]any{
		"namespace": "prod",
	}

	tests := []struct {
		name     string
		input    any
		expected any
	}{
		{
			name:     "plain string",
			input:    "plain-text",
			expected: "plain-text",
		},
		{
			name:     "string with single brace",
			input:    "value with { single brace",
			expected: "value with { single brace",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result, err := renderTemplatesInValue(tt.input, templateData)
			s.NoError(err)
			s.Equal(tt.expected, result)
		})
	}
}

func (s *DeploymentFuncsTestSuite) Test_renderTemplatesInValue_MapWithNestedTemplates() {
	templateData := map[string]any{
		"namespace": "prod",
		"config": map[string]any{
			"clusterName": "cluster-1",
		},
	}

	input := map[string]any{
		"name":  "{{ .config.clusterName }}",
		"port":  8080,
		"label": "static-value",
		"nested": map[string]any{
			"ns": "{{ .namespace }}",
		},
	}

	result, err := renderTemplatesInValue(input, templateData)
	s.NoError(err)

	resultMap := result.(map[string]any)
	s.Equal("cluster-1", resultMap["name"])
	s.Equal(8080, resultMap["port"])
	s.Equal("static-value", resultMap["label"])

	nestedMap := resultMap["nested"].(map[string]any)
	s.Equal("prod", nestedMap["ns"])
}

func (s *DeploymentFuncsTestSuite) Test_renderTemplatesInValue_SliceWithTemplates() {
	templateData := map[string]any{
		"namespace": "prod",
		"env":       "production",
	}

	input := []any{"{{ .namespace }}", "static", "{{ .env }}"}

	result, err := renderTemplatesInValue(input, templateData)
	s.NoError(err)

	resultSlice := result.([]any)
	s.Equal("prod", resultSlice[0])
	s.Equal("static", resultSlice[1])
	s.Equal("production", resultSlice[2])
}

func (s *DeploymentFuncsTestSuite) Test_renderTemplatesInValue_NonStringValues() {
	templateData := map[string]any{
		"namespace": "prod",
	}

	tests := []struct {
		name     string
		input    any
		expected any
	}{
		{
			name:     "integer",
			input:    42,
			expected: 42,
		},
		{
			name:     "float",
			input:    3.14,
			expected: 3.14,
		},
		{
			name:     "bool true",
			input:    true,
			expected: true,
		},
		{
			name:     "bool false",
			input:    false,
			expected: false,
		},
		{
			name:     "nil",
			input:    nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result, err := renderTemplatesInValue(tt.input, templateData)
			s.NoError(err)
			s.Equal(tt.expected, result)
		})
	}
}

func (s *DeploymentFuncsTestSuite) Test_renderTemplatesInValue_InvalidTemplate() {
	templateData := map[string]any{
		"namespace": "prod",
	}

	// Invalid template syntax - should return original value without error
	input := "{{ .namespace"
	result, err := renderTemplatesInValue(input, templateData)
	s.NoError(err)
	s.Equal(input, result)
}

func (s *DeploymentFuncsTestSuite) Test_renderTemplatesInValue_MissingVariable() {
	templateData := map[string]any{
		"namespace": "prod",
	}

	// Missing variable - template execution should handle gracefully
	input := "value-{{ .missing }}"
	result, err := renderTemplatesInValue(input, templateData)
	s.NoError(err)
	// Should render with empty value for missing key
	s.Equal("value-<no value>", result)
}

func (s *DeploymentFuncsTestSuite) Test_renderTemplatesInValue_WithTemplateFunctions() {
	templateData := map[string]any{
		"value":   "",
		"present": "exists",
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "default function with empty value",
			input:    "{{ default \"fallback\" .value }}",
			expected: "fallback",
		},
		{
			name:     "default function with present value",
			input:    "{{ default \"fallback\" .present }}",
			expected: "exists",
		},
		{
			name:     "or function",
			input:    "{{ or .value .present }}",
			expected: "exists",
		},
		{
			name:     "not function with empty",
			input:    "{{ if not .value }}empty{{ else }}has-value{{ end }}",
			expected: "empty",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result, err := renderTemplatesInValue(tt.input, templateData)
			s.NoError(err)
			s.Equal(tt.expected, result)
		})
	}
}

func (s *DeploymentFuncsTestSuite) Test_calculateSyncWaves_NilServices() {
	err := calculateSyncWaves(nil)
	s.NoError(err)
}

func (s *DeploymentFuncsTestSuite) Test_calculateSyncWaves_EmptyServices() {
	services := map[string]any{}
	err := calculateSyncWaves(services)
	s.NoError(err)
}

func (s *DeploymentFuncsTestSuite) Test_calculateSyncWaves_NoDependencies() {
	services := map[string]any{
		"serviceA": map[string]any{
			"enabled": true,
		},
		"serviceB": map[string]any{
			"enabled": true,
		},
		"serviceC": map[string]any{
			"enabled": true,
		},
	}

	err := calculateSyncWaves(services)
	s.NoError(err)

	// All services should be at wave 0
	for name, svc := range services {
		svcMap := svc.(map[string]any)
		s.Equal(0, svcMap["syncWave"], "service %s should be wave 0", name)
	}
}

func (s *DeploymentFuncsTestSuite) Test_calculateSyncWaves_WithDependencies() {
	services := map[string]any{
		"database": map[string]any{
			"enabled": true,
		},
		"cache": map[string]any{
			"enabled": true,
		},
		"api": map[string]any{
			"enabled": true,
			"dependsOn": []any{
				map[string]any{"name": "database"},
				map[string]any{"name": "cache"},
			},
		},
		"frontend": map[string]any{
			"enabled": true,
			"dependsOn": []any{
				map[string]any{"name": "api"},
			},
		},
	}

	err := calculateSyncWaves(services)
	s.NoError(err)

	// database and cache should be wave 0
	s.Equal(0, services["database"].(map[string]any)["syncWave"])
	s.Equal(0, services["cache"].(map[string]any)["syncWave"])
	// api depends on database and cache, should be wave 1
	s.Equal(1, services["api"].(map[string]any)["syncWave"])
	// frontend depends on api, should be wave 2
	s.Equal(2, services["frontend"].(map[string]any)["syncWave"])
}

func (s *DeploymentFuncsTestSuite) Test_calculateSyncWaves_UserConfiguredPreserved() {
	services := map[string]any{
		"database": map[string]any{
			"enabled": true,
		},
		"api": map[string]any{
			"enabled":  true,
			"syncWave": 5, // user-configured
			"dependsOn": []any{
				map[string]any{"name": "database"},
			},
		},
	}

	err := calculateSyncWaves(services)
	s.NoError(err)

	// database should be wave 0
	s.Equal(0, services["database"].(map[string]any)["syncWave"])
	// api should preserve user-configured wave 5 (not calculated wave 1)
	s.Equal(5, services["api"].(map[string]any)["syncWave"])
}

func (s *DeploymentFuncsTestSuite) Test_calculateSyncWaves_UserConfiguredAsFloat64() {
	// JSON unmarshaling produces float64 for numbers
	services := map[string]any{
		"service": map[string]any{
			"enabled":  true,
			"syncWave": float64(3),
		},
	}

	err := calculateSyncWaves(services)
	s.NoError(err)

	// User-configured value should be preserved (skipped in the final update loop)
	// The syncWave remains as float64(3) since it was user-configured
	s.Equal(float64(3), services["service"].(map[string]any)["syncWave"])
}

func (s *DeploymentFuncsTestSuite) Test_calculateSyncWaves_UserConfiguredAsInt64() {
	services := map[string]any{
		"service": map[string]any{
			"enabled":  true,
			"syncWave": int64(7),
		},
	}

	err := calculateSyncWaves(services)
	s.NoError(err)

	// User-configured value should be preserved (skipped in the final update loop)
	// The syncWave remains as int64(7) since it was user-configured
	s.Equal(int64(7), services["service"].(map[string]any)["syncWave"])
}

func (s *DeploymentFuncsTestSuite) Test_calculateSyncWaves_DependencyNotInServices() {
	services := map[string]any{
		"api": map[string]any{
			"enabled": true,
			"dependsOn": []any{
				map[string]any{"name": "nonexistent-service"},
			},
		},
	}

	err := calculateSyncWaves(services)
	s.NoError(err)

	// api should be wave 0 since dependency doesn't exist
	s.Equal(0, services["api"].(map[string]any)["syncWave"])
}

func (s *DeploymentFuncsTestSuite) Test_calculateSyncWaves_InvalidDependsOnFormat() {
	services := map[string]any{
		"api": map[string]any{
			"enabled":   true,
			"dependsOn": "invalid-string-format", // should be a slice
		},
	}

	err := calculateSyncWaves(services)
	s.NoError(err)

	// Should handle gracefully and set wave 0
	s.Equal(0, services["api"].(map[string]any)["syncWave"])
}

func (s *DeploymentFuncsTestSuite) Test_calculateSyncWaves_DependsOnWithInvalidItems() {
	services := map[string]any{
		"database": map[string]any{
			"enabled": true,
		},
		"api": map[string]any{
			"enabled": true,
			"dependsOn": []any{
				"invalid-string-item", // should be map
				map[string]any{"name": "database"},
			},
		},
	}

	err := calculateSyncWaves(services)
	s.NoError(err)

	// api should still get wave 1 from valid database dependency
	s.Equal(1, services["api"].(map[string]any)["syncWave"])
}

func (s *DeploymentFuncsTestSuite) Test_calculateSyncWaves_DependsOnMissingName() {
	services := map[string]any{
		"api": map[string]any{
			"enabled": true,
			"dependsOn": []any{
				map[string]any{"namespace": "other"}, // missing "name" key
			},
		},
	}

	err := calculateSyncWaves(services)
	s.NoError(err)

	// api should be wave 0 since no valid dependency name
	s.Equal(0, services["api"].(map[string]any)["syncWave"])
}

func (s *DeploymentFuncsTestSuite) Test_calculateSyncWaves_ServiceConfigNotMap() {
	services := map[string]any{
		"stringService": "not-a-map",
		"api": map[string]any{
			"enabled": true,
		},
	}

	err := calculateSyncWaves(services)
	s.NoError(err)

	// api should still work
	s.Equal(0, services["api"].(map[string]any)["syncWave"])
}

func (s *DeploymentFuncsTestSuite) Test_calculateSyncWaves_ChainedDependencies() {
	// A -> B -> C -> D (chain of 4)
	services := map[string]any{
		"serviceD": map[string]any{
			"enabled": true,
		},
		"serviceC": map[string]any{
			"enabled": true,
			"dependsOn": []any{
				map[string]any{"name": "serviceD"},
			},
		},
		"serviceB": map[string]any{
			"enabled": true,
			"dependsOn": []any{
				map[string]any{"name": "serviceC"},
			},
		},
		"serviceA": map[string]any{
			"enabled": true,
			"dependsOn": []any{
				map[string]any{"name": "serviceB"},
			},
		},
	}

	err := calculateSyncWaves(services)
	s.NoError(err)

	s.Equal(0, services["serviceD"].(map[string]any)["syncWave"])
	s.Equal(1, services["serviceC"].(map[string]any)["syncWave"])
	s.Equal(2, services["serviceB"].(map[string]any)["syncWave"])
	s.Equal(3, services["serviceA"].(map[string]any)["syncWave"])
}

func (s *DeploymentFuncsTestSuite) Test_calculateSyncWaves_DiamondDependency() {
	//     A
	//    / \
	//   B   C
	//    \ /
	//     D
	services := map[string]any{
		"serviceD": map[string]any{
			"enabled": true,
		},
		"serviceB": map[string]any{
			"enabled": true,
			"dependsOn": []any{
				map[string]any{"name": "serviceD"},
			},
		},
		"serviceC": map[string]any{
			"enabled": true,
			"dependsOn": []any{
				map[string]any{"name": "serviceD"},
			},
		},
		"serviceA": map[string]any{
			"enabled": true,
			"dependsOn": []any{
				map[string]any{"name": "serviceB"},
				map[string]any{"name": "serviceC"},
			},
		},
	}

	err := calculateSyncWaves(services)
	s.NoError(err)

	s.Equal(0, services["serviceD"].(map[string]any)["syncWave"])
	s.Equal(1, services["serviceB"].(map[string]any)["syncWave"])
	s.Equal(1, services["serviceC"].(map[string]any)["syncWave"])
	s.Equal(2, services["serviceA"].(map[string]any)["syncWave"])
}

// ---- buildRuntimeTemplateVars and buildComponentsTemplateVars tests ----

type TemplateVarsTestSuite struct {
	suite.Suite
	scheme *runtime.Scheme
}

func TestTemplateVarsTestSuite(t *testing.T) {
	suite.Run(t, new(TemplateVarsTestSuite))
}

func (s *TemplateVarsTestSuite) SetupSuite() {
	s.scheme = runtime.NewScheme()
	s.Require().NoError(clientgoscheme.AddToScheme(s.scheme))
	s.Require().NoError(pmcorev1alpha1.AddToScheme(s.scheme))
}

// newSubroutineWithProfile creates a DeploymentSubroutine backed by a fake
// clientRuntime that already contains a profile ConfigMap for the given inst.
func (s *TemplateVarsTestSuite) newSubroutineWithProfile(profileYAML string, remoteRuntime config.RemoteClusterConfig) (*DeploymentSubroutine, *pmcorev1alpha1.PlatformMesh) {
	return newSubroutineWithProfile(s.T(), profileYAML, "test-pm", "test-ns", remoteRuntime)
}

// newSubroutineWithProfile creates a DeploymentSubroutine backed by a fake clientRuntime
// that already contains a profile ConfigMap for the instance it returns. Package-level so
// both the suite and plain Test_ functions can stand up the same subroutine.
func newSubroutineWithProfile(t *testing.T, profileYAML, name, namespace string, remoteRuntime config.RemoteClusterConfig) (*DeploymentSubroutine, *pmcorev1alpha1.PlatformMesh) {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, pmcorev1alpha1.AddToScheme(scheme))

	inst := &pmcorev1alpha1.PlatformMesh{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: pmcorev1alpha1.PlatformMeshSpec{},
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      inst.Name + defaultProfileConfigMapSuffix,
			Namespace: inst.Namespace,
		},
		Data: map[string]string{
			profileConfigMapKey: profileYAML,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(inst, cm).
		Build()

	return &DeploymentSubroutine{
		clientRuntime: fakeClient,
		clientInfra:   fakeClient,
		cfg:           &pmconfig.CommonServiceConfig{},
		cfgOperator:   &config.OperatorConfig{RemoteRuntime: remoteRuntime},
		gotemplates:   gotemplates.Resources,
	}, inst
}

// minimalProfileYAML is a valid profile with empty infra and components sections.
const minimalProfileYAML = `
infra:
  baseDomain: example.com
components:
  services: {}
`

func (s *TemplateVarsTestSuite) Test_buildRuntimeTemplateVars_BasicMerge() {
	profileYAML := `
infra:
  baseDomain: profile.example.com
  infraKey: fromProfile
components:
  services: {}
`
	sub, inst := s.newSubroutineWithProfile(profileYAML, config.RemoteClusterConfig{})

	templateVars := apiextensionsv1.JSON{Raw: []byte(`{"templateKey":"fromTemplateVars"}`)}
	result, err := sub.buildRuntimeTemplateVars(context.Background(), inst, templateVars)

	s.Require().NoError(err)
	s.Equal("profile.example.com", result["baseDomain"])
	s.Equal("fromProfile", result["infraKey"])
	s.Equal("fromTemplateVars", result["templateKey"])
}

func (s *TemplateVarsTestSuite) Test_buildRuntimeTemplateVars_TemplateVarsOverrideProfile() {
	profileYAML := `
infra:
  baseDomain: profile.example.com
  sharedKey: fromProfile
components:
  services: {}
`
	sub, inst := s.newSubroutineWithProfile(profileYAML, config.RemoteClusterConfig{})

	templateVars := apiextensionsv1.JSON{Raw: []byte(`{"sharedKey":"fromTemplateVars"}`)}
	result, err := sub.buildRuntimeTemplateVars(context.Background(), inst, templateVars)

	s.Require().NoError(err)
	// templateVars should win over profile
	s.Equal("fromTemplateVars", result["sharedKey"])
}

func (s *TemplateVarsTestSuite) Test_buildRuntimeTemplateVars_SpecValuesOverride() {
	sub, inst := s.newSubroutineWithProfile(minimalProfileYAML, config.RemoteClusterConfig{})

	specValues := map[string]any{"specKey": "fromSpec"}
	raw, err := json.Marshal(specValues)
	s.Require().NoError(err)
	inst.Spec.Values = apiextensionsv1.JSON{Raw: raw}

	result, err := sub.buildRuntimeTemplateVars(context.Background(), inst, apiextensionsv1.JSON{})

	s.Require().NoError(err)
	s.Equal("fromSpec", result["specKey"])
}

func (s *TemplateVarsTestSuite) Test_buildRuntimeTemplateVars_OCMConfigMerged() {
	sub, inst := s.newSubroutineWithProfile(minimalProfileYAML, config.RemoteClusterConfig{})

	inst.Spec.OCM = &pmcorev1alpha1.OCMConfig{
		Repo:      &pmcorev1alpha1.RepoConfig{Name: "my-repo"},
		Component: &pmcorev1alpha1.ComponentConfig{Name: "my-component"},
		ReferencePath: []pmcorev1alpha1.ReferencePathElement{
			{Name: "path-element"},
		},
	}

	result, err := sub.buildRuntimeTemplateVars(context.Background(), inst, apiextensionsv1.JSON{})

	s.Require().NoError(err)
	ocm, ok := result["ocm"].(map[string]any)
	s.Require().True(ok, "expected ocm key in result")
	repo, ok := ocm["repo"].(map[string]any)
	s.Require().True(ok, "expected repo in ocm")
	s.Equal("my-repo", repo["name"])
	component, ok := ocm["component"].(map[string]any)
	s.Require().True(ok, "expected component in ocm")
	s.Equal("my-component", component["name"])
	refs, ok := ocm["referencePath"].([]any)
	s.Require().True(ok, "expected referencePath in ocm")
	s.Len(refs, 1)
}

func (s *TemplateVarsTestSuite) Test_buildRuntimeTemplateVars_KubeConfigDisabled() {
	sub, inst := s.newSubroutineWithProfile(minimalProfileYAML, config.RemoteClusterConfig{})

	result, err := sub.buildRuntimeTemplateVars(context.Background(), inst, apiextensionsv1.JSON{})

	s.Require().NoError(err)
	s.Equal(false, result["kubeConfigEnabled"])
	_, hasSecretName := result["kubeConfigSecretName"]
	s.False(hasSecretName, "kubeConfigSecretName should not be set when remote runtime disabled")
}

func (s *TemplateVarsTestSuite) Test_buildRuntimeTemplateVars_KubeConfigEnabled() {
	remoteRuntime := config.RemoteClusterConfig{
		Kubeconfig:      "/path/to/kubeconfig",
		InfraSecretName: "infra-secret",
		InfraSecretKey:  "kubeconfig",
	}
	sub, inst := s.newSubroutineWithProfile(minimalProfileYAML, remoteRuntime)

	result, err := sub.buildRuntimeTemplateVars(context.Background(), inst, apiextensionsv1.JSON{})

	s.Require().NoError(err)
	s.Equal(true, result["kubeConfigEnabled"])
	s.Equal("infra-secret", result["kubeConfigSecretName"])
	s.Equal("kubeconfig", result["kubeConfigSecretKey"])
}

func (s *TemplateVarsTestSuite) Test_buildRuntimeTemplateVars_ReleaseNamespace() {
	sub, inst := s.newSubroutineWithProfile(minimalProfileYAML, config.RemoteClusterConfig{})

	result, err := sub.buildRuntimeTemplateVars(context.Background(), inst, apiextensionsv1.JSON{})

	s.Require().NoError(err)
	s.Equal("test-ns", result["releaseNamespace"])
	s.Equal("test-ns", result["helmReleaseNamespace"])
}

func (s *TemplateVarsTestSuite) Test_buildRuntimeTemplateVars_MissingConfigMap() {
	// Subroutine with no ConfigMap in the fake store
	fakeClient := fake.NewClientBuilder().WithScheme(s.scheme).Build()
	operatorCfg := &config.OperatorConfig{}
	cfg := &pmconfig.CommonServiceConfig{}
	sub := &DeploymentSubroutine{
		clientRuntime: fakeClient,
		cfg:           cfg,
		cfgOperator:   operatorCfg,
	}
	inst := &pmcorev1alpha1.PlatformMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pm", Namespace: "test-ns"},
	}

	_, err := sub.buildRuntimeTemplateVars(context.Background(), inst, apiextensionsv1.JSON{})
	s.Error(err, "expected error when profile ConfigMap is missing")
}

func (s *TemplateVarsTestSuite) Test_buildComponentsTemplateVars_BasicProfile() {
	profileYAML := `
infra: {}
components:
  services:
    myservice:
      enabled: true
      namespace: default
`
	sub, inst := s.newSubroutineWithProfile(profileYAML, config.RemoteClusterConfig{})

	result, _, err := sub.buildComponentsTemplateVars(context.Background(), inst, apiextensionsv1.JSON{})

	s.Require().NoError(err)
	values, ok := result["values"].(map[string]any)
	s.Require().True(ok, "expected values key")
	services, ok := values["services"].(map[string]any)
	s.Require().True(ok, "expected services key")
	myService, ok := services["myservice"].(map[string]any)
	s.Require().True(ok, "expected myservice in services")
	s.Equal(true, myService["enabled"])
}

func (s *TemplateVarsTestSuite) Test_buildComponentsTemplateVars_ReleaseNamespace() {
	sub, inst := s.newSubroutineWithProfile(minimalProfileYAML, config.RemoteClusterConfig{})

	result, _, err := sub.buildComponentsTemplateVars(context.Background(), inst, apiextensionsv1.JSON{})

	s.Require().NoError(err)
	s.Equal("test-ns", result["releaseNamespace"])
}

func (s *TemplateVarsTestSuite) Test_buildComponentsTemplateVars_SpecValuesServices() {
	profileYAML := `
infra: {}
components:
  services:
    base:
      enabled: true
`
	sub, inst := s.newSubroutineWithProfile(profileYAML, config.RemoteClusterConfig{})

	specValues := map[string]any{
		"services": map[string]any{
			"override": map[string]any{"enabled": true},
		},
	}
	raw, err := json.Marshal(specValues)
	s.Require().NoError(err)
	inst.Spec.Values = apiextensionsv1.JSON{Raw: raw}

	result, _, err := sub.buildComponentsTemplateVars(context.Background(), inst, apiextensionsv1.JSON{})

	s.Require().NoError(err)
	values, ok := result["values"].(map[string]any)
	s.Require().True(ok)
	services, ok := values["services"].(map[string]any)
	s.Require().True(ok)
	// Both base (from profile) and override (from spec.Values) should be present
	s.Contains(services, "base")
	s.Contains(services, "override")
}

func (s *TemplateVarsTestSuite) Test_buildComponentsTemplateVars_DeploymentTechnologyDefault() {
	sub, inst := s.newSubroutineWithProfile(minimalProfileYAML, config.RemoteClusterConfig{})

	result, _, err := sub.buildComponentsTemplateVars(context.Background(), inst, apiextensionsv1.JSON{})

	s.Require().NoError(err)
	s.Equal("fluxcd", result["deploymentTechnology"])
}

func (s *TemplateVarsTestSuite) Test_buildComponentsTemplateVars_DeploymentTechnologyFromTemplateVars() {
	sub, inst := s.newSubroutineWithProfile(minimalProfileYAML, config.RemoteClusterConfig{})

	templateVars := apiextensionsv1.JSON{Raw: []byte(`{"deploymentTechnology":"argocd"}`)}
	result, _, err := sub.buildComponentsTemplateVars(context.Background(), inst, templateVars)

	s.Require().NoError(err)
	s.Equal("argocd", result["deploymentTechnology"])
}

func (s *TemplateVarsTestSuite) Test_buildComponentsTemplateVars_DeploymentTechnologyInvalidDefaultsToFluxcd() {
	sub, inst := s.newSubroutineWithProfile(minimalProfileYAML, config.RemoteClusterConfig{})

	templateVars := apiextensionsv1.JSON{Raw: []byte(`{"deploymentTechnology":"helm"}`)}
	result, _, err := sub.buildComponentsTemplateVars(context.Background(), inst, templateVars)

	s.Require().NoError(err)
	s.Equal("fluxcd", result["deploymentTechnology"])
}

func (s *TemplateVarsTestSuite) Test_buildComponentsTemplateVars_KubeConfigEnabled() {
	remoteRuntime := config.RemoteClusterConfig{
		Kubeconfig:      "/path/to/kubeconfig",
		InfraSecretName: "infra-secret",
		InfraSecretKey:  "kubeconfig",
	}
	sub, inst := s.newSubroutineWithProfile(minimalProfileYAML, remoteRuntime)

	result, _, err := sub.buildComponentsTemplateVars(context.Background(), inst, apiextensionsv1.JSON{})

	s.Require().NoError(err)
	s.Equal(true, result["kubeConfigEnabled"])
	s.Equal("infra-secret", result["kubeConfigSecretName"])
	s.Equal("kubeconfig", result["kubeConfigSecretKey"])
}

func (s *TemplateVarsTestSuite) Test_buildComponentsTemplateVars_BaseDomainFields() {
	sub, inst := s.newSubroutineWithProfile(minimalProfileYAML, config.RemoteClusterConfig{})
	inst.Spec.Exposure = &pmcorev1alpha1.ExposureConfig{
		BaseDomain: "my.domain.com",
		Port:       8443,
	}

	result, _, err := sub.buildComponentsTemplateVars(context.Background(), inst, apiextensionsv1.JSON{})

	s.Require().NoError(err)
	s.Equal("my.domain.com", result["baseDomain"])
	s.Equal("8443", result["port"])
	s.Equal("my.domain.com:8443", result["baseDomainWithPort"])
}

func (s *TemplateVarsTestSuite) Test_buildComponentsTemplateVars_BaseDomainWithDefaultPort() {
	sub, inst := s.newSubroutineWithProfile(minimalProfileYAML, config.RemoteClusterConfig{})
	inst.Spec.Exposure = &pmcorev1alpha1.ExposureConfig{
		BaseDomain: "my.domain.com",
	}

	result, _, err := sub.buildComponentsTemplateVars(context.Background(), inst, apiextensionsv1.JSON{})

	s.Require().NoError(err)
	s.Equal("8443", result["port"])
	// When port is 8443 (non-standard), baseDomainWithPort includes the port
	s.Equal("my.domain.com:8443", result["baseDomainWithPort"])
}

// ---- loadProfileSections tests ----

func (s *DeploymentFuncsTestSuite) Test_loadProfileSections_Success() {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = pmcorev1alpha1.AddToScheme(scheme)

	profileYAML := `infra:
  deploymentTechnology: argocd
  certManager:
    enabled: true
components:
  keycloak:
    enabled: true
`
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-mesh-profile", Namespace: "platform-mesh-system"},
		Data:       map[string]string{"profile.yaml": profileYAML},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()
	sub := &DeploymentSubroutine{clientRuntime: cl}

	inst := &pmcorev1alpha1.PlatformMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-mesh", Namespace: "platform-mesh-system"},
	}

	infraYAML, componentsYAML, err := sub.loadProfileSections(context.Background(), inst)
	s.Require().NoError(err)
	s.Contains(infraYAML, "deploymentTechnology")
	s.Contains(infraYAML, "argocd")
	s.Contains(componentsYAML, "keycloak")
}

func (s *DeploymentFuncsTestSuite) Test_loadProfileSections_MissingConfigMap() {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = pmcorev1alpha1.AddToScheme(scheme)

	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	sub := &DeploymentSubroutine{clientRuntime: cl}

	inst := &pmcorev1alpha1.PlatformMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-mesh", Namespace: "platform-mesh-system"},
	}

	_, _, err := sub.loadProfileSections(context.Background(), inst)
	s.Require().Error(err)
}

func (s *DeploymentFuncsTestSuite) Test_loadProfileSections_CustomConfigMapRef() {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = pmcorev1alpha1.AddToScheme(scheme)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "custom-profile", Namespace: "other-ns"},
		Data:       map[string]string{"profile.yaml": "infra:\n  enabled: true\ncomponents:\n  svc: true\n"},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()
	sub := &DeploymentSubroutine{clientRuntime: cl}

	inst := &pmcorev1alpha1.PlatformMesh{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-mesh", Namespace: "platform-mesh-system"},
		Spec: pmcorev1alpha1.PlatformMeshSpec{
			ProfileConfigMap: &pmcorev1alpha1.ConfigMapReference{Name: "custom-profile", Namespace: "other-ns"},
		},
	}

	infraYAML, componentsYAML, err := sub.loadProfileSections(context.Background(), inst)
	s.Require().NoError(err)
	s.Contains(infraYAML, "enabled")
	s.Contains(componentsYAML, "svc")
}
