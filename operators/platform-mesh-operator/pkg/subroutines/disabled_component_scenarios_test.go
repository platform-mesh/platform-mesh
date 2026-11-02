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
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

// What happens when a component is switched off? These render every FluxCD HelmRelease the
// operator would create, from the profile we actually ship, and check nothing is left waiting
// on a release that is never deployed.

// shippedProfilePath is what the kind e2e suite applies — the closest thing in-tree to a
// production config.
const shippedProfilePath = "../../test/e2e/kind/yaml/platform-mesh-resource/default-profile.yaml"

// platformMeshNamespace is where the CR lives in a real install. The shipped profile writes
// explicit "namespace: platform-mesh-system" dependsOn entries, so this has to match.
const platformMeshNamespace = "platform-mesh-system"

func loadShippedProfile(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(shippedProfilePath)
	require.NoError(t, err)

	var cm corev1.ConfigMap
	require.NoError(t, yaml.Unmarshal(raw, &cm))
	profile, ok := cm.Data[profileConfigMapKey]
	require.True(t, ok, "%s must contain key %s", shippedProfilePath, profileConfigMapKey)
	return profile
}

// componentSection: infra components sit under infra, services under components.services.
func componentSection(t *testing.T, profile map[string]any, section string) map[string]any {
	t.Helper()
	if section == "infra" {
		infra, ok := profile["infra"].(map[string]any)
		require.True(t, ok, "profile must have an infra section")
		return infra
	}
	components, ok := profile["components"].(map[string]any)
	require.True(t, ok, "profile must have a components section")
	services, ok := components["services"].(map[string]any)
	require.True(t, ok, "profile must have components.services")
	return services
}

// setComponentEnabled flips <section>.<component>.enabled, as an operator would.
func setComponentEnabled(t *testing.T, profileYAML, section, component string, enabled bool) string {
	t.Helper()
	var profile map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(profileYAML), &profile))

	cfg, ok := componentSection(t, profile, section)[component].(map[string]any)
	require.True(t, ok, "%s.%s must exist in the shipped profile", section, component)
	cfg["enabled"] = enabled

	out, err := yaml.Marshal(profile)
	require.NoError(t, err)
	return string(out)
}

func disableComponent(t *testing.T, profileYAML, section, component string) string {
	t.Helper()
	return setComponentEnabled(t, profileYAML, section, component, false)
}

// withObservabilityStack enables the observability stack (otel operator, prometheus CRDs and the
// observability release). The e2e profile ships it disabled so the kind suite fits on the default
// CI runner, but the dependency gates around it still need to be exercised.
func withObservabilityStack(t *testing.T, profileYAML string) string {
	t.Helper()
	profileYAML = setComponentEnabled(t, profileYAML, "infra", "prometheusOperatorCRDs", true)
	profileYAML = setComponentEnabled(t, profileYAML, "infra", "opentelemetryOperator", true)
	return setComponentEnabled(t, profileYAML, "components", "observability", true)
}

// componentNames lists every component in a section, so we can sweep all of them.
func componentNames(t *testing.T, profileYAML, section string) []string {
	t.Helper()
	var profile map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(profileYAML), &profile))

	names := []string{}
	for name, cfg := range componentSection(t, profile, section) {
		if _, ok := cfg.(map[string]any); ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func newSubroutineForProfile(t *testing.T, profileYAML, namespace string) (*DeploymentSubroutine, *pmcorev1alpha1.PlatformMesh) {
	t.Helper()
	return newSubroutineWithProfile(t, profileYAML, "platform-mesh", namespace, config.RemoteClusterConfig{})
}

// renderedGraph maps each rendered HelmRelease to the releases it depends on.
type renderedGraph map[string][]string

// renderHelmReleaseGraph renders every FluxCD HelmRelease for a profile: infra plus services.
func renderHelmReleaseGraph(t *testing.T, sub *DeploymentSubroutine, inst *pmcorev1alpha1.PlatformMesh) renderedGraph {
	t.Helper()
	ctx := context.Background()

	log := testLogger(t)
	graph := renderedGraph{}

	collect := func(path string, vars map[string]any) {
		objs, err := sub.renderTemplateFile(path, vars, log)
		require.NoError(t, err, "rendering %s", path)
		for _, obj := range objs {
			if obj.GetKind() != "HelmRelease" {
				continue
			}
			namespace := obj.GetNamespace()

			deps := []string{}
			spec, _ := obj.Object["spec"].(map[string]any)
			if raw, ok := spec["dependsOn"].([]any); ok {
				for _, entry := range raw {
					// Same resolution the operator applies when it prunes.
					if key, resolvable := dependencyKey(entry, namespace); resolvable {
						deps = append(deps, key)
					}
				}
			}
			graph[helmReleaseKey(namespace, obj.GetName())] = deps
		}
	}

	// Infra components, using the operator's own file selection for a FluxCD profile.
	infraVars, err := sub.templateVarsFromProfileInfra(ctx, inst, apiextensionsv1.JSON{}, sub.cfgOperator)
	require.NoError(t, err)
	skipFile := deploymentTechFileFilter(deploymentTechFluxCD, log)

	infraDir := filepath.Join("..", "..", "gotemplates", "infra", "infra")
	components, err := os.ReadDir(infraDir)
	require.NoError(t, err)
	for _, component := range components {
		if !component.IsDir() {
			continue
		}
		componentDir := filepath.Join(infraDir, component.Name())
		files, err := ListFiles(componentDir)
		require.NoError(t, err)
		for _, file := range files {
			if skipFile(file) {
				continue
			}
			collect(filepath.Join(componentDir, file), infraVars)
		}
	}

	// Component services: gotemplates/components/infra/helmreleases.yaml
	componentVars, _, err := sub.buildComponentsTemplateVars(ctx, inst, apiextensionsv1.JSON{})
	require.NoError(t, err)
	collect(filepath.Join("..", "..", "gotemplates", "components", "infra", "helmreleases.yaml"), componentVars)

	return graph
}

// The core invariant: every declared dependency must be a release we actually render.
func assertNoDanglingDependencies(t *testing.T, graph renderedGraph) {
	t.Helper()
	for release, deps := range graph {
		for _, dep := range deps {
			_, exists := graph[dep]
			assert.True(t, exists,
				"HelmRelease %s depends on %s, which is never rendered — it would hang in DependencyNotReady", release, dep)
		}
	}
}

func Test_ShippedProfile_HasNoDanglingDependencies(t *testing.T) {
	ns := platformMeshNamespace
	shipped := loadShippedProfile(t)

	sub, inst := newSubroutineForProfile(t, shipped, ns)
	graph := renderHelmReleaseGraph(t, sub, inst)

	require.NotEmpty(t, graph, "the shipped profile must render HelmReleases")
	assertNoDanglingDependencies(t, graph)
	assert.NotContains(t, graph, helmReleaseKey(ns, "observability"),
		"the shipped profile keeps observability off so the e2e fits the default runner")

	// The gates only pay off if the dependency survives while the target is enabled.
	assert.Contains(t, graph[helmReleaseKey(ns, "traefik")], helmReleaseKey(ns, "traefik-crds"))

	sub, inst = newSubroutineForProfile(t, withObservabilityStack(t, shipped), ns)
	graph = renderHelmReleaseGraph(t, sub, inst)
	assertNoDanglingDependencies(t, graph)
	assert.Contains(t, graph[helmReleaseKey(ns, "opentelemetry-operator")], helmReleaseKey(ns, "cert-manager"))
}

// Switching a component off must never leave another release waiting on it.
func Test_DisablingComponent_LeavesRemainingStackDeployable(t *testing.T) {
	ns := platformMeshNamespace
	tests := []struct {
		name      string
		section   string
		component string
		absent    string // release that must no longer be rendered
		dependent string // release that must survive without it ("" to skip)
		keptDep   string // dependency of that dependent that must remain ("" to skip)
	}{
		{
			name:    "cert-manager off while the observability stack is wanted",
			section: "infra", component: "certManager",
			absent: "cert-manager", dependent: "opentelemetry-operator",
		},
		{
			name:    "traefik CRDs managed outside the platform",
			section: "infra", component: "traefikCRDs",
			absent: "traefik-crds", dependent: "traefik",
		},
		{
			name:    "otel operator off: a service depending on an infra release",
			section: "infra", component: "opentelemetryOperator",
			absent: "opentelemetry-operator", dependent: "observability",
		},
		{
			name:    "cluster already runs Prometheus, so its CRDs come from elsewhere",
			section: "infra", component: "prometheusOperatorCRDs",
			absent: "prometheus-operator-crds", dependent: "observability",
		},
		{
			name:    "traefik off entirely",
			section: "infra", component: "traefik",
			absent: "traefik",
		},
		{
			name:    "cluster already runs the whole observability stack",
			section: "components", component: "observability",
			absent: "observability",
		},
	}

	// Every scenario is about a stack that wants observability, which the shipped profile
	// keeps off; switch it on first so the gates under test are actually rendered.
	base := withObservabilityStack(t, loadShippedProfile(t))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := disableComponent(t, base, tt.section, tt.component)
			sub, inst := newSubroutineForProfile(t, profile, ns)
			graph := renderHelmReleaseGraph(t, sub, inst)

			assert.NotContains(t, graph, helmReleaseKey(ns, tt.absent),
				"a disabled component must not be rendered")
			require.NotEmpty(t, graph, "the rest of the platform must still render")

			if tt.dependent != "" {
				require.Contains(t, graph, helmReleaseKey(ns, tt.dependent),
					"the dependent must still be deployed")
				assert.NotContains(t, graph[helmReleaseKey(ns, tt.dependent)], helmReleaseKey(ns, tt.absent),
					"the dependent must not wait for a release that is never deployed")
			}
			if tt.keptDep != "" {
				assert.Contains(t, graph[helmReleaseKey(ns, tt.dependent)], helmReleaseKey(ns, tt.keptDep),
					"dependencies that are still enabled must survive")
			}
			assertNoDanglingDependencies(t, graph)
		})
	}
}

// The cases above are hand-picked; this sweeps every component in the profile, so anything
// added later is covered without remembering to add a case.
func Test_DisablingAnySingleComponent_LeavesNoDanglingDependencies(t *testing.T) {
	shipped := loadShippedProfile(t)
	variants := map[string]string{
		"shipped":            shipped,
		"with-observability": withObservabilityStack(t, shipped),
	}

	for variant, profile := range variants {
		for _, section := range []string{"infra", "components"} {
			for _, component := range componentNames(t, profile, section) {
				t.Run(variant+"/"+section+"/"+component, func(t *testing.T) {
					sub, inst := newSubroutineForProfile(t,
						disableComponent(t, profile, section, component), platformMeshNamespace)
					assertNoDanglingDependencies(t, renderHelmReleaseGraph(t, sub, inst))
				})
			}
		}
	}
}
