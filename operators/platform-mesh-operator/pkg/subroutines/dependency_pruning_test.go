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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/golang-commons/logger/testlogger"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// disabledDependencyProfileYAML has observability wanted but cert-manager off, and
// opentelemetry-operator off too — the second link of the chain, where a service depends
// directly on an infra HelmRelease.
const disabledDependencyProfileYAML = `
infra:
  certManager:
    enabled: false
    name: cert-manager
  prometheusOperatorCRDs:
    enabled: true
    name: prometheus-operator-crds
  opentelemetryOperator:
    enabled: false
    name: opentelemetry-operator
components:
  services:
    observability:
      enabled: true
      dependsOn:
      - name: prometheus-operator-crds
        namespace: test-ns
      - name: opentelemetry-operator
        namespace: test-ns
      values: {}
    portal:
      enabled: true
      dependsOn:
      - name: keycloak-operator
      values: {}
    keycloak-operator:
      enabled: false
      values: {}
`

func (s *TemplateVarsTestSuite) Test_buildComponentsTemplateVars_DropsDependenciesOnDisabledComponents() {
	sub, inst := s.newSubroutineWithProfile(disabledDependencyProfileYAML, config.RemoteClusterConfig{})

	result, _, err := sub.buildComponentsTemplateVars(context.Background(), inst, apiextensionsv1.JSON{})
	s.Require().NoError(err)

	values := result["values"].(map[string]any)
	services := values["services"].(map[string]any)

	observability := services["observability"].(map[string]any)
	s.Equal([]any{
		map[string]any{"name": "prometheus-operator-crds", "namespace": "test-ns"},
	}, observability["dependsOn"],
		"the disabled opentelemetry-operator must be dropped, the enabled CRD release kept")

	portal := services["portal"].(map[string]any)
	s.Empty(portal["dependsOn"],
		"a namespace-less dependency on a disabled component service must be dropped too")
}

// Components can move their releases elsewhere while infra stays in the instance namespace.
// The disabled-set keys must follow the infra render, or the pruning matches nothing.
func (s *TemplateVarsTestSuite) Test_buildComponentsTemplateVars_ResolvesInfraNamespaceIndependently() {
	profile := `
infra:
  opentelemetryOperator:
    enabled: false
    name: opentelemetry-operator
components:
  deploymentNamespace: components-ns
  services:
    observability:
      enabled: true
      dependsOn:
      - name: opentelemetry-operator
        namespace: test-ns
      values: {}
`
	sub, inst := s.newSubroutineWithProfile(profile, config.RemoteClusterConfig{})

	result, _, err := sub.buildComponentsTemplateVars(context.Background(), inst, apiextensionsv1.JSON{})
	s.Require().NoError(err)

	s.Equal("components-ns", result["deploymentNamespace"], "components keep their own namespace")

	services := result["values"].(map[string]any)["services"].(map[string]any)
	s.Empty(services["observability"].(map[string]any)["dependsOn"],
		"the infra release lives in the instance namespace, so the dependency on it must still be dropped")
}

func (s *TemplateVarsTestSuite) Test_buildComponentsTemplateVars_KeepsDependenciesOnEnabledComponents() {
	profile := `
infra:
  prometheusOperatorCRDs:
    enabled: true
    name: prometheus-operator-crds
  opentelemetryOperator:
    enabled: true
    name: opentelemetry-operator
components:
  services:
    observability:
      enabled: true
      dependsOn:
      - name: prometheus-operator-crds
        namespace: test-ns
      - name: opentelemetry-operator
        namespace: test-ns
      values: {}
`
	sub, inst := s.newSubroutineWithProfile(profile, config.RemoteClusterConfig{})

	result, _, err := sub.buildComponentsTemplateVars(context.Background(), inst, apiextensionsv1.JSON{})
	s.Require().NoError(err)

	services := result["values"].(map[string]any)["services"].(map[string]any)
	observability := services["observability"].(map[string]any)
	s.Len(observability["dependsOn"], 2, "nothing may be dropped while every dependency is enabled")
}

// testLogger keeps the pruner's warnings out of the test output but still inspectable.
func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	return testlogger.New().HideLogOutput().Logger
}

// dep builds a dependsOn entry as it arrives from the profile YAML.
func dep(name, namespace string) map[string]any {
	entry := map[string]any{"name": name}
	if namespace != "" {
		entry["namespace"] = namespace
	}
	return entry
}

func Test_disabledHelmReleases(t *testing.T) {
	infraProfile := map[string]any{
		"deploymentTechnology": "fluxcd", // not a component, must be ignored
		"ocm": map[string]any{ // no name/enabled pair, must be ignored
			"component": map[string]any{"name": "platform-mesh"},
		},
		"certManager":           map[string]any{"enabled": false, "name": "cert-manager"},
		"opentelemetryOperator": map[string]any{"enabled": true, "name": "opentelemetry-operator"},
	}
	services := map[string]any{
		"observability":     map[string]any{"enabled": true},
		"keycloak-operator": map[string]any{"enabled": false},
		"cnpg-operator":     map[string]any{"enabled": true, "skipHelmRelease": true},
	}

	disabled := disabledHelmReleases(infraProfile, services, "infra-ns", "components-ns")

	assert.Contains(t, disabled, "infra-ns/cert-manager")
	assert.Contains(t, disabled, "components-ns/keycloak-operator")
	assert.Contains(t, disabled, "components-ns/cnpg-operator", "skipHelmRelease means no HelmRelease is rendered")

	assert.NotContains(t, disabled, "infra-ns/opentelemetry-operator")
	assert.NotContains(t, disabled, "components-ns/observability")
	assert.Len(t, disabled, 3)
}

func Test_pruneDisabledDependencies(t *testing.T) {
	tests := []struct {
		name     string
		services map[string]any
		disabled map[string]struct{}
		expect   map[string][]any
	}{
		{
			name: "drops a dependency on a disabled infra component",
			services: map[string]any{
				"observability": map[string]any{
					"enabled": true,
					"dependsOn": []any{
						dep("prometheus-operator-crds", "platform-mesh-system"),
						dep("opentelemetry-operator", "platform-mesh-system"),
					},
				},
			},
			disabled: map[string]struct{}{"platform-mesh-system/opentelemetry-operator": {}},
			expect: map[string][]any{
				"observability": {dep("prometheus-operator-crds", "platform-mesh-system")},
			},
		},
		{
			name: "drops a dependency that omits the namespace, defaulting to the release namespace",
			services: map[string]any{
				"infra": map[string]any{
					"enabled":   true,
					"dependsOn": []any{dep("keycloak-operator", "")},
				},
			},
			disabled: map[string]struct{}{"platform-mesh-system/keycloak-operator": {}},
			expect:   map[string][]any{"infra": {}},
		},
		{
			name: "keeps a dependency in a different namespace",
			services: map[string]any{
				"infra": map[string]any{
					"enabled":   true,
					"dependsOn": []any{dep("keycloak-operator", "other-ns")},
				},
			},
			disabled: map[string]struct{}{"platform-mesh-system/keycloak-operator": {}},
			expect:   map[string][]any{"infra": {dep("keycloak-operator", "other-ns")}},
		},
		{
			name: "keeps a dependency on an unknown target",
			services: map[string]any{
				"infra": map[string]any{
					"enabled":   true,
					"dependsOn": []any{dep("some-external-release", "platform-mesh-system")},
				},
			},
			disabled: map[string]struct{}{"platform-mesh-system/keycloak-operator": {}},
			expect:   map[string][]any{"infra": {dep("some-external-release", "platform-mesh-system")}},
		},
		{
			name: "leaves a service without dependencies untouched",
			services: map[string]any{
				"observability": map[string]any{"enabled": true},
			},
			disabled: map[string]struct{}{"platform-mesh-system/opentelemetry-operator": {}},
			expect:   map[string][]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pruneDisabledDependencies(tt.services, tt.disabled, "platform-mesh-system", testLogger(t))

			for service, want := range tt.expect {
				cfg := tt.services[service].(map[string]any)
				got, found := cfg["dependsOn"]
				require.True(t, found, "dependsOn key must be preserved for %s", service)
				assert.Equal(t, want, got)
			}
			for service := range tt.services {
				if _, tracked := tt.expect[service]; !tracked {
					_, found := tt.services[service].(map[string]any)["dependsOn"]
					assert.False(t, found, "no dependsOn key must be invented for %s", service)
				}
			}
		})
	}
}

func Test_pruneDisabledDependencies_MalformedInput(t *testing.T) {
	services := map[string]any{
		"not-a-map":       "string instead of config",
		"wrong-type":      map[string]any{"dependsOn": "should-be-a-list"},
		"entry-not-a-map": map[string]any{"dependsOn": []any{"bare-string"}},
		"entry-without-name": map[string]any{"dependsOn": []any{map[string]any{
			"namespace": "platform-mesh-system",
		}}},
	}
	disabled := map[string]struct{}{"platform-mesh-system/anything": {}}

	assert.NotPanics(t, func() {
		pruneDisabledDependencies(services, disabled, "platform-mesh-system", testLogger(t))
	})

	// Malformed entries are left exactly as they were rather than silently reshaped.
	assert.Equal(t, "should-be-a-list", services["wrong-type"].(map[string]any)["dependsOn"])
	assert.Equal(t, []any{"bare-string"}, services["entry-not-a-map"].(map[string]any)["dependsOn"])
}

func Test_featureToggleConflicts(t *testing.T) {
	services := map[string]any{
		"pm-openfga-operator":      map[string]any{"enabled": true},
		"pm-openfga-authz-webhook": map[string]any{"enabled": false},
		"iam-service":              map[string]any{"enabled": true},
		"pm-keycloak-operator":     map[string]any{"enabled": true},
		"openfga":                  map[string]any{"enabled": false},
		"portal":                   map[string]any{"enabled": true},
		"infra": map[string]any{"enabled": true, "values": map[string]any{
			"kcp":      map[string]any{"webhook": map[string]any{"enabled": true}},
			"keycloak": map[string]any{"operator": map[string]any{"enabled": false}},
		}},
	}

	t.Run("no toggles, no conflicts", func(t *testing.T) {
		assert.Empty(t, featureToggleConflicts(&pmcorev1alpha1.PlatformMesh{}, services))
	})

	t.Run("lists the switches the toggle rules out that are on", func(t *testing.T) {
		inst := &pmcorev1alpha1.PlatformMesh{Spec: pmcorev1alpha1.PlatformMeshSpec{
			FeatureToggles: []pmcorev1alpha1.FeatureToggle{{Name: FeatureDisableOpenFGA}},
		}}
		assert.Equal(t, []string{
			FeatureDisableOpenFGA + ": services.iam-service.enabled",
			FeatureDisableOpenFGA + ": services.infra.values.kcp.webhook.enabled",
			FeatureDisableOpenFGA + ": services.pm-openfga-operator.enabled",
		}, featureToggleConflicts(inst, services), "iam-ui is absent, the webhook and openfga disabled")
	})

	t.Run("both toggles", func(t *testing.T) {
		inst := &pmcorev1alpha1.PlatformMesh{Spec: pmcorev1alpha1.PlatformMeshSpec{
			FeatureToggles: []pmcorev1alpha1.FeatureToggle{{Name: FeatureDisableKeycloak}, {Name: FeatureDisableOpenFGA}},
		}}
		assert.Equal(t, []string{
			FeatureDisableKeycloak + ": services.iam-service.enabled",
			FeatureDisableKeycloak + ": services.pm-keycloak-operator.enabled",
			FeatureDisableOpenFGA + ": services.iam-service.enabled",
			FeatureDisableOpenFGA + ": services.infra.values.kcp.webhook.enabled",
			FeatureDisableOpenFGA + ": services.pm-openfga-operator.enabled",
		}, featureToggleConflicts(inst, services))
	})

	t.Run("a path through a non-map counts as off", func(t *testing.T) {
		inst := &pmcorev1alpha1.PlatformMesh{Spec: pmcorev1alpha1.PlatformMeshSpec{
			FeatureToggles: []pmcorev1alpha1.FeatureToggle{{Name: FeatureDisableKeycloak}},
		}}
		assert.Empty(t, featureToggleConflicts(inst, map[string]any{"infra": map[string]any{"values": "broken"}}))
	})
}

func (s *TemplateVarsTestSuite) Test_buildComponentsTemplateVars_RefusesTogglesConflictingWithProfile() {
	profile := `
infra: {}
components:
  services:
    pm-keycloak-operator:
      enabled: true
      values: {}
    infra:
      enabled: true
      values:
        keycloak:
          operator:
            enabled: true
    portal:
      enabled: true
      values: {}
`
	sub, inst := s.newSubroutineWithProfile(profile, config.RemoteClusterConfig{})
	inst.Spec.FeatureToggles = []pmcorev1alpha1.FeatureToggle{{Name: FeatureDisableKeycloak}}

	_, _, err := sub.buildComponentsTemplateVars(context.Background(), inst, apiextensionsv1.JSON{})
	s.Require().Error(err)
	s.Contains(err.Error(), FeatureDisableKeycloak+": services.infra.values.keycloak.operator.enabled")
	s.Contains(err.Error(), FeatureDisableKeycloak+": services.pm-keycloak-operator.enabled")
}
