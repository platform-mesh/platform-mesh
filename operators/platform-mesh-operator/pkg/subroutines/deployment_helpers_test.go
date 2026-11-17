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
	"github.com/stretchr/testify/suite"

	pmconfig "go.platform-mesh.io/golang-commons/config"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/internal/config"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type DeploymentHelpersTestSuite struct {
	suite.Suite
	log *logger.Logger
}

func TestDeploymentHelpersTestSuite(t *testing.T) {
	suite.Run(t, new(DeploymentHelpersTestSuite))
}

func (s *DeploymentHelpersTestSuite) SetupTest() {
	cfg := logger.DefaultConfig()
	cfg.Level = "debug"
	cfg.NoJSON = true
	cfg.Name = "DeploymentHelpersTestSuite"
	var err error
	s.log, err = logger.New(cfg)
	s.Require().NoError(err)
}

func (s *DeploymentHelpersTestSuite) Test_updateObjectMetadata() {
	tests := []struct {
		name     string
		existing *unstructured.Unstructured
		desired  *unstructured.Unstructured
		validate func(*unstructured.Unstructured)
	}{
		{
			name: "update labels and annotations",
			existing: &unstructured.Unstructured{
				Object: map[string]any{
					"metadata": map[string]any{
						"labels":      map[string]any{"existing": "label"},
						"annotations": map[string]any{"existing": "annotation"},
					},
				},
			},
			desired: &unstructured.Unstructured{
				Object: map[string]any{
					"metadata": map[string]any{
						"labels":      map[string]any{"desired": "label"},
						"annotations": map[string]any{"desired": "annotation"},
					},
				},
			},
			validate: func(obj *unstructured.Unstructured) {
				labels := obj.GetLabels()
				s.Equal("label", labels["desired"])
				s.NotContains(labels, "existing")

				annotations := obj.GetAnnotations()
				s.Equal("annotation", annotations["desired"])
				s.NotContains(annotations, "existing")
			},
		},
		{
			name: "desired has no metadata",
			existing: &unstructured.Unstructured{
				Object: map[string]any{
					"metadata": map[string]any{
						"labels": map[string]any{"existing": "label"},
					},
				},
			},
			desired: &unstructured.Unstructured{
				Object: map[string]any{},
			},
			validate: func(obj *unstructured.Unstructured) {
				// Existing labels should remain if desired has none
				labels := obj.GetLabels()
				s.NotNil(labels, "labels should be preserved")
				s.Equal("label", labels["existing"], "existing label should be preserved")
			},
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			updateObjectMetadata(tt.existing, tt.desired)
			if tt.validate != nil {
				tt.validate(tt.existing)
			}
		})
	}
}

func (s *DeploymentHelpersTestSuite) Test_isZeroValue() {
	tests := []struct {
		name     string
		value    any
		expected bool
	}{
		{
			name:     "nil value",
			value:    nil,
			expected: true,
		},
		{
			name:     "empty string",
			value:    "",
			expected: true,
		},
		{
			name:     "non-empty string",
			value:    "hello",
			expected: false,
		},
		{
			name:     "empty slice",
			value:    []any{},
			expected: true,
		},
		{
			name:     "non-empty slice",
			value:    []any{"a"},
			expected: false,
		},
		{
			name:     "empty map",
			value:    map[string]any{},
			expected: true,
		},
		{
			name:     "non-empty map",
			value:    map[string]any{"key": "value"},
			expected: false,
		},
		{
			name:     "zero int",
			value:    0,
			expected: true,
		},
		{
			name:     "non-zero int",
			value:    42,
			expected: false,
		},
		{
			name:     "false bool",
			value:    false,
			expected: true,
		},
		{
			name:     "true bool",
			value:    true,
			expected: false,
		},
		{
			name:     "zero float",
			value:    0.0,
			expected: true,
		},
		{
			name:     "non-zero float",
			value:    3.14,
			expected: false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result := isZeroValue(tt.value)
			s.Equal(tt.expected, result)
		})
	}
}

func (s *DeploymentHelpersTestSuite) Test_templateFuncMap_default() {
	funcMap := templateFuncMap()
	defaultFunc := funcMap["default"].(func(any, any) any)

	tests := []struct {
		name         string
		defaultValue any
		actualValue  any
		expected     any
	}{
		{
			name:         "use default when value is nil",
			defaultValue: "default",
			actualValue:  nil,
			expected:     "default",
		},
		{
			name:         "use default when value is empty string",
			defaultValue: "default",
			actualValue:  "",
			expected:     "default",
		},
		{
			name:         "use actual value when non-empty",
			defaultValue: "default",
			actualValue:  "actual",
			expected:     "actual",
		},
		{
			name:         "use default when value is empty slice",
			defaultValue: []string{"default"},
			actualValue:  []any{},
			expected:     []string{"default"},
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result := defaultFunc(tt.defaultValue, tt.actualValue)
			s.Equal(tt.expected, result)
		})
	}
}

func (s *DeploymentHelpersTestSuite) Test_templateFuncMap_toYaml() {
	funcMap := templateFuncMap()
	toYamlFunc := funcMap["toYaml"].(func(any) (string, error))

	tests := []struct {
		name        string
		value       any
		expected    string
		expectError bool
	}{
		{
			name:     "simple map",
			value:    map[string]any{"key": "value"},
			expected: "key: value\n",
		},
		{
			name:     "nested map",
			value:    map[string]any{"outer": map[string]any{"inner": "value"}},
			expected: "outer:\n  inner: value\n",
		},
		{
			name:     "slice",
			value:    []string{"a", "b", "c"},
			expected: "- a\n- b\n- c\n",
		},
		{
			name:     "string",
			value:    "simple",
			expected: "simple\n",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result, err := toYamlFunc(tt.value)
			if tt.expectError {
				s.Error(err)
			} else {
				s.NoError(err)
				s.Equal(tt.expected, result)
			}
		})
	}
}

func (s *DeploymentHelpersTestSuite) Test_templateFuncMap_nindent() {
	funcMap := templateFuncMap()
	nindentFunc := funcMap["nindent"].(func(int, string) string)

	tests := []struct {
		name     string
		spaces   int
		input    string
		expected string
	}{
		{
			name:     "empty string",
			spaces:   4,
			input:    "",
			expected: "",
		},
		{
			name:     "single line",
			spaces:   2,
			input:    "hello",
			expected: "\n  hello\n",
		},
		{
			name:     "multiple lines",
			spaces:   4,
			input:    "line1\nline2\nline3",
			expected: "\n    line1\n    line2\n    line3\n",
		},
		{
			name:     "lines with trailing newline",
			spaces:   2,
			input:    "line1\nline2\n",
			expected: "\n  line1\n  line2\n",
		},
		{
			name:     "lines with empty lines at start",
			spaces:   2,
			input:    "\n\nline1",
			expected: "\n  line1\n",
		},
		{
			name:     "zero spaces",
			spaces:   0,
			input:    "hello",
			expected: "\nhello\n",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			result := nindentFunc(tt.spaces, tt.input)
			s.Equal(tt.expected, result)
		})
	}
}

func (s *DeploymentHelpersTestSuite) Test_preserveExistingArgoSourceFields() {
	tests := []struct {
		name                   string
		existingApp            *unstructured.Unstructured
		objMap                 map[string]any
		expectedRepoURL        string
		expectedTargetRevision string
	}{
		{
			name:        "app does not exist - nothing to preserve",
			existingApp: nil,
			objMap: map[string]any{
				"spec": map[string]any{
					"source": map[string]any{
						"repoURL":        "https://new-repo.git",
						"targetRevision": "v1.0.0",
					},
				},
			},
			expectedRepoURL:        "https://new-repo.git",
			expectedTargetRevision: "v1.0.0",
		},
		{
			name: "existing app has placeholder values - should not preserve",
			existingApp: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "argoproj.io/v1alpha1",
					"kind":       "Application",
					"metadata": map[string]any{
						"name":      "test-app",
						"namespace": "argocd",
					},
					"spec": map[string]any{
						"source": map[string]any{
							"repoURL":        argoPlaceholderRepoURL,
							"targetRevision": argoPlaceholderRepoURL,
						},
					},
				},
			},
			objMap: map[string]any{
				"spec": map[string]any{
					"source": map[string]any{
						"repoURL":        "https://new-repo.git",
						"targetRevision": "v1.0.0",
					},
				},
			},
			expectedRepoURL:        "https://new-repo.git",
			expectedTargetRevision: "v1.0.0",
		},
		{
			name: "existing app has real values different from new - should preserve",
			existingApp: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "argoproj.io/v1alpha1",
					"kind":       "Application",
					"metadata": map[string]any{
						"name":      "test-app",
						"namespace": "argocd",
					},
					"spec": map[string]any{
						"source": map[string]any{
							"repoURL":        "https://existing-repo.git",
							"targetRevision": "v0.9.0",
						},
					},
				},
			},
			objMap: map[string]any{
				"spec": map[string]any{
					"source": map[string]any{
						"repoURL":        "https://new-repo.git",
						"targetRevision": "v1.0.0",
					},
				},
			},
			expectedRepoURL:        "https://existing-repo.git",
			expectedTargetRevision: "", // deleted so ResourceSubroutine's value is preserved
		},
		{
			name: "existing app has same values as new - no preservation needed",
			existingApp: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "argoproj.io/v1alpha1",
					"kind":       "Application",
					"metadata": map[string]any{
						"name":      "test-app",
						"namespace": "argocd",
					},
					"spec": map[string]any{
						"source": map[string]any{
							"repoURL":        "https://same-repo.git",
							"targetRevision": "v1.0.0",
						},
					},
				},
			},
			objMap: map[string]any{
				"spec": map[string]any{
					"source": map[string]any{
						"repoURL":        "https://same-repo.git",
						"targetRevision": "v1.0.0",
					},
				},
			},
			expectedRepoURL:        "https://same-repo.git",
			expectedTargetRevision: "v1.0.0",
		},
		{
			name: "existing app has empty repoURL - should not preserve",
			existingApp: &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "argoproj.io/v1alpha1",
					"kind":       "Application",
					"metadata": map[string]any{
						"name":      "test-app",
						"namespace": "argocd",
					},
					"spec": map[string]any{
						"source": map[string]any{
							"repoURL":        "",
							"targetRevision": "",
						},
					},
				},
			},
			objMap: map[string]any{
				"spec": map[string]any{
					"source": map[string]any{
						"repoURL":        "https://new-repo.git",
						"targetRevision": "v1.0.0",
					},
				},
			},
			expectedRepoURL:        "https://new-repo.git",
			expectedTargetRevision: "v1.0.0",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			ctx := context.Background()

			var fakeClient ctrlruntimeclient.Client
			if tt.existingApp != nil {
				fakeClient = fake.NewClientBuilder().
					WithObjects(tt.existingApp).
					Build()
			} else {
				fakeClient = fake.NewClientBuilder().Build()
			}

			cfg := pmconfig.CommonServiceConfig{}
			operatorCfg := config.OperatorConfig{}

			sub := &DeploymentSubroutine{
				clientInfra: fakeClient,
				cfg:         &cfg,
				cfgOperator: &operatorCfg,
			}

			sub.preserveExistingArgoSourceFields(ctx, tt.objMap, "test-app", "argocd", s.log)

			spec := tt.objMap["spec"].(map[string]any)
			source := spec["source"].(map[string]any)

			s.Equal(tt.expectedRepoURL, source["repoURL"])

			actualTargetRevision, hasTargetRevision := source["targetRevision"]
			if tt.expectedTargetRevision == "" {
				s.False(hasTargetRevision, "targetRevision should have been deleted to preserve existing")
			} else {
				s.True(hasTargetRevision, "targetRevision should be present")
				s.Equal(tt.expectedTargetRevision, actualTargetRevision)
			}
		})
	}
}

func (s *DeploymentHelpersTestSuite) Test_preserveExistingArgoSourceFields_GetError() {
	ctx := context.Background()

	fakeClient := fake.NewClientBuilder().Build()

	cfg := pmconfig.CommonServiceConfig{}
	operatorCfg := config.OperatorConfig{}

	sub := &DeploymentSubroutine{
		clientInfra: fakeClient,
		cfg:         &cfg,
		cfgOperator: &operatorCfg,
	}

	objMap := map[string]any{
		"spec": map[string]any{
			"source": map[string]any{
				"repoURL":        "https://new-repo.git",
				"targetRevision": "v1.0.0",
			},
		},
	}

	sub.preserveExistingArgoSourceFields(ctx, objMap, "nonexistent-app", "argocd", s.log)

	spec := objMap["spec"].(map[string]any)
	source := spec["source"].(map[string]any)
	s.Equal("https://new-repo.git", source["repoURL"])
	s.Equal("v1.0.0", source["targetRevision"])
}

func (s *DeploymentHelpersTestSuite) Test_mergeImageVersionsIntoHelmReleaseValues_unsuspend() {
	tests := []struct {
		name          string
		isUnsuspended bool
		specSuspend   *bool // nil = field absent from template
		expectSuspend any   // nil = key absent
	}{
		{
			name:          "unsuspended in store → suspend overridden to false",
			isUnsuspended: true,
			specSuspend:   boolPtr(true),
			expectSuspend: false,
		},
		{
			name:          "not unsuspended in store → suspend kept as true",
			isUnsuspended: false,
			specSuspend:   boolPtr(true),
			expectSuspend: true,
		},
		{
			name:          "unsuspended in store but template has no suspend field → suspend not set",
			isUnsuspended: true,
			specSuspend:   nil,
			expectSuspend: nil,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			store := NewImageVersionStore()
			store.Set("platform-mesh-system", "openfga", "postgresql.image.tag", "17.9.0")
			if tt.isUnsuspended {
				store.SetUnsuspended("platform-mesh-system", "openfga")
			}

			sub := &DeploymentSubroutine{
				imageVersionStore: store,
				cfg:               &pmconfig.CommonServiceConfig{},
				cfgOperator:       &config.OperatorConfig{},
			}

			spec := map[string]any{"values": map[string]any{}}
			if tt.specSuspend != nil {
				spec["suspend"] = *tt.specSuspend
			}
			obj := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "helm.toolkit.fluxcd.io/v2",
				"kind":       "HelmRelease",
				"metadata":   map[string]any{"name": "openfga", "namespace": "platform-mesh-system"},
				"spec":       spec,
			}}

			sub.mergeImageVersionsIntoHelmReleaseValues(obj, "openfga", "platform-mesh-system", s.log)

			resultSpec := obj.Object["spec"].(map[string]any)
			resultSuspend, hasSuspend := resultSpec["suspend"]
			if tt.expectSuspend == nil {
				s.False(hasSuspend, "expected suspend key to be absent")
			} else {
				s.True(hasSuspend, "expected suspend key to be present")
				s.Equal(tt.expectSuspend, resultSuspend)
			}
		})
	}
}

func (s *DeploymentHelpersTestSuite) Test_mergeImageVersionsIntoHelmReleaseValues_localizesCoordinates() {
	store := NewImageVersionStore()
	store.Set("platform-mesh-system", "keycloak", "image.tag", "1.2.3-localized")
	store.Set("platform-mesh-system", "keycloak", "image.registry", "registry.internal.example.com")
	store.Set("platform-mesh-system", "keycloak", "image.repository", "platform-mesh/keycloak")
	store.Set("platform-mesh-system", "keycloak", "image.digest", "sha256:abc")

	sub := &DeploymentSubroutine{imageVersionStore: store}

	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "helm.toolkit.fluxcd.io/v2",
		"kind":       "HelmRelease",
		"metadata":   map[string]any{"name": "keycloak", "namespace": "platform-mesh-system"},
		"spec": map[string]any{"values": map[string]any{
			"image": map[string]any{
				"registry":   "ghcr.io/platform-mesh",
				"repository": "upstream-images/keycloak",
				"tag":        "0.0.0",
			},
		}},
	}}

	sub.mergeImageVersionsIntoHelmReleaseValues(obj, "keycloak", "platform-mesh-system", s.log)

	get := func(path ...string) string {
		v, _, _ := unstructured.NestedString(obj.Object, path...)
		return v
	}
	s.Equal("registry.internal.example.com", get("spec", "values", "image", "registry"))
	s.Equal("platform-mesh/keycloak", get("spec", "values", "image", "repository"))
	s.Equal("1.2.3-localized", get("spec", "values", "image", "tag"))
	s.Equal("sha256:abc", get("spec", "values", "image", "digest"))
}

func boolPtr(b bool) *bool { return &b }

func Test_pruneHelmReleases(t *testing.T) {
	t.Parallel()

	helmReleaseGVK := schema.GroupVersionKind{
		Group:   "helm.toolkit.fluxcd.io",
		Version: "v2",
		Kind:    "HelmRelease",
	}
	helmRelease := func(namespace, name string, operatorCreated bool) *unstructured.Unstructured {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(helmReleaseGVK)
		obj.SetNamespace(namespace)
		obj.SetName(name)
		if operatorCreated {
			obj.SetLabels(map[string]string{operatorCreatedLabel: "true"})
		}
		return obj
	}

	all := []string{
		"components-ns/iam-service",
		"components-ns/managed-provider",
		"components-ns/security-operator",
		"infra-ns/cert-manager",
		"other-ns/security-operator",
	}

	cases := map[string]struct {
		infraNamespace      string
		infraRendered       map[string]struct{}
		componentsNamespace string
		componentsRendered  map[string]struct{}
		wantErr             bool
		want                []string
	}{
		"deletes operator-created releases that were not rendered": {
			infraNamespace:      "infra-ns",
			componentsNamespace: "components-ns",
			componentsRendered:  map[string]struct{}{"components-ns/iam-service": {}},
			want: []string{
				"components-ns/iam-service",
				"components-ns/managed-provider",
				"other-ns/security-operator",
			},
		},
		"keeps everything that is rendered": {
			infraNamespace:      "infra-ns",
			infraRendered:       map[string]struct{}{"infra-ns/cert-manager": {}},
			componentsNamespace: "components-ns",
			componentsRendered: map[string]struct{}{
				"components-ns/iam-service":       {},
				"components-ns/security-operator": {},
			},
			want: all,
		},
		"a shared namespace keeps the releases of both sets": {
			infraNamespace:      "components-ns",
			infraRendered:       map[string]struct{}{"components-ns/iam-service": {}},
			componentsNamespace: "components-ns",
			componentsRendered:  map[string]struct{}{"components-ns/security-operator": {}},
			want:                all,
		},
		"deletes nothing when nothing was rendered": {
			infraNamespace:      "infra-ns",
			componentsNamespace: "components-ns",
			want:                all,
		},
		"refuses an empty namespace": {
			componentsNamespace: "components-ns",
			componentsRendered:  map[string]struct{}{"components-ns/iam-service": {}},
			wantErr:             true,
			want:                all,
		},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()

			fakeClient := fake.NewClientBuilder().
				WithObjects(
					helmRelease("components-ns", "iam-service", true),
					helmRelease("components-ns", "managed-provider", false),
					helmRelease("components-ns", "security-operator", true),
					helmRelease("infra-ns", "cert-manager", true),
					helmRelease("other-ns", "security-operator", true),
				).
				Build()

			err := pruneHelmReleases(t.Context(), fakeClient, cas.infraNamespace, cas.infraRendered, cas.componentsNamespace, cas.componentsRendered, testLogger(t))
			if cas.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			list := &unstructured.UnstructuredList{}
			list.SetGroupVersionKind(helmReleaseGVK.GroupVersion().WithKind("HelmReleaseList"))
			require.NoError(t, fakeClient.List(t.Context(), list))

			got := make([]string, 0, len(list.Items))
			for i := range list.Items {
				got = append(got, helmReleaseKey(list.Items[i].GetNamespace(), list.Items[i].GetName()))
			}
			assert.ElementsMatch(t, cas.want, got, "only unkept operator-created releases in the given namespaces may go")
		})
	}
}
