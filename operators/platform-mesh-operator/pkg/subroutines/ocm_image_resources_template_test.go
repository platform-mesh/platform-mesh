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
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/golang-commons/logger"
)

// renderGotemplateDocs renders a gotemplates/ file through the operator's own
// renderTemplateFile and returns every YAML document as a generic map.
func renderGotemplateDocs(t *testing.T, relPath string, data map[string]any) []map[string]any {
	t.Helper()
	path := filepath.Join("..", "..", "gotemplates", filepath.FromSlash(relPath))

	cfg := logger.DefaultConfig()
	cfg.NoJSON = true
	log, err := logger.New(cfg)
	require.NoError(t, err)

	objs, err := (&DeploymentSubroutine{}).renderTemplateFile(path, data, log)
	require.NoError(t, err)

	out := make([]map[string]any, len(objs))
	for i, obj := range objs {
		out[i] = obj.Object
	}
	return out
}

// baseOcmValues returns a minimal .values.ocm block shared across image-resources tests.
func baseOcmValues() map[string]any {
	return map[string]any{
		"component":     map[string]any{"name": "platform-mesh"},
		"repo":          map[string]any{"name": "platform-mesh"},
		"referencePath": []any{map[string]any{"name": "github.com/platform-mesh/account-operator"}},
	}
}

// assertAdditionalStatusFields verifies the standard OCM image coordinate fields are present.
func assertAdditionalStatusFields(t *testing.T, spec map[string]any) {
	t.Helper()
	asf, ok := spec["additionalStatusFields"].(map[string]any)
	require.True(t, ok, "spec.additionalStatusFields must be rendered")
	assert.Equal(t, "resource.access.imageReference.toOCI().registry", asf["registry"])
	assert.Equal(t, "resource.access.imageReference.toOCI().repository", asf["repository"])
	assert.Equal(t, "resource.access.imageReference.toOCI().tag", asf["tag"])
	assert.Equal(t, "resource.access.imageReference.toOCI().digest", asf["digest"])
}

func Test_OcmImageResourcesTemplate(t *testing.T) {
	data := map[string]any{
		"releaseNamespace": "platform-mesh-system",
		"values": map[string]any{
			"ocm": baseOcmValues(),
			"services": map[string]any{
				"account-operator": map[string]any{
					"enabled": true,
					"imageResources": []any{
						map[string]any{
							"annotations": map[string]any{
								"repo":     "oci",
								"artifact": "image",
								"for":      "account-operator",
							},
						},
					},
				},
			},
		},
	}

	docs := renderGotemplateDocs(t, "components/runtime/ocm-image-resources.yaml", data)
	require.Len(t, docs, 1, "one imageResource must render exactly one Resource")
	obj := docs[0]

	metadata := obj["metadata"].(map[string]any)
	assert.Equal(t, "account-operator-image", metadata["name"])
	annotations := metadata["annotations"].(map[string]any)
	assert.Equal(t, "account-operator", annotations["for"])

	assertAdditionalStatusFields(t, obj["spec"].(map[string]any))
}

// Test_OcmImageResourcesTemplate_Default verifies that an enabled service with NO imageResources
// entry still gets exactly one image Resource synthesized, with the boilerplate defaults.
// This is the core behaviour introduced by the flattening refactor.
func Test_OcmImageResourcesTemplate_Default(t *testing.T) {
	data := map[string]any{
		"releaseNamespace": "platform-mesh-system",
		"values": map[string]any{
			"ocm": baseOcmValues(),
			"services": map[string]any{
				"my-operator": map[string]any{
					"enabled": true,
					// no imageResources key — the default branch must fire
				},
			},
		},
	}

	docs := renderGotemplateDocs(t, "components/runtime/ocm-image-resources.yaml", data)
	require.Len(t, docs, 1, "enabled service with no imageResources must render exactly one default Resource")
	obj := docs[0]

	metadata := obj["metadata"].(map[string]any)
	assert.Equal(t, "my-operator-image", metadata["name"])

	annotations := metadata["annotations"].(map[string]any)
	assert.Equal(t, "oci", annotations["repo"])
	assert.Equal(t, "image", annotations["artifact"])
	assert.Equal(t, "my-operator", annotations["for"])

	spec := obj["spec"].(map[string]any)
	assertAdditionalStatusFields(t, spec)

	// referencePath must include the ocm.referencePath prefix then the service name then "image"
	byRef := spec["resource"].(map[string]any)["byReference"].(map[string]any)
	assert.Equal(t, "image", byRef["resource"].(map[string]any)["name"])
	refPath := byRef["referencePath"].([]any)
	// first segment: ocm.referencePath entry, then service name, then "image"
	require.GreaterOrEqual(t, len(refPath), 2, "referencePath must have at least service + image segments")
	last := refPath[len(refPath)-1].(map[string]any)
	assert.Equal(t, "image", last["name"])
	secondLast := refPath[len(refPath)-2].(map[string]any)
	assert.Equal(t, "my-operator", secondLast["name"])
}

// Test_OcmImageResourcesTemplate_DefaultEquivalence verifies that a service whose explicit
// boilerplate imageResources entry is removed produces a Resource byte-equivalent to what the
// old explicit entry produced. This is the render-equivalence check for the flattening.
func Test_OcmImageResourcesTemplate_DefaultEquivalence(t *testing.T) {
	ocm := baseOcmValues()

	withExplicit := map[string]any{
		"releaseNamespace": "platform-mesh-system",
		"values": map[string]any{
			"ocm": ocm,
			"services": map[string]any{
				"account-operator": map[string]any{
					"enabled": true,
					"imageResources": []any{
						map[string]any{
							"annotations": map[string]any{
								"repo":     "oci",
								"artifact": "image",
								"for":      "account-operator",
							},
						},
					},
				},
			},
		},
	}

	withDefault := map[string]any{
		"releaseNamespace": "platform-mesh-system",
		"values": map[string]any{
			"ocm": ocm,
			"services": map[string]any{
				"account-operator": map[string]any{
					"enabled": true,
					// imageResources omitted — default branch synthesises the same Resource
				},
			},
		},
	}

	explicit := renderGotemplateDocs(t, "components/runtime/ocm-image-resources.yaml", withExplicit)
	defaulted := renderGotemplateDocs(t, "components/runtime/ocm-image-resources.yaml", withDefault)

	require.Len(t, explicit, 1)
	require.Len(t, defaulted, 1)

	// Names must match
	explicitMeta := explicit[0]["metadata"].(map[string]any)
	defaultedMeta := defaulted[0]["metadata"].(map[string]any)
	assert.Equal(t, explicitMeta["name"], defaultedMeta["name"])

	// annotations.for must match
	explicitAnnotations := explicitMeta["annotations"].(map[string]any)
	defaultedAnnotations := defaultedMeta["annotations"].(map[string]any)
	assert.Equal(t, explicitAnnotations["for"], defaultedAnnotations["for"])

	// referencePath must be identical in shape
	explicitByRef := explicit[0]["spec"].(map[string]any)["resource"].(map[string]any)["byReference"].(map[string]any)
	defaultedByRef := defaulted[0]["spec"].(map[string]any)["resource"].(map[string]any)["byReference"].(map[string]any)
	assert.Equal(t, explicitByRef["referencePath"], defaultedByRef["referencePath"])
}

// Test_OcmImageResourcesTemplate_DisabledSkipped verifies that a disabled service with no
// imageResources produces no Resources (the {{- if $config.enabled }} guard must still apply).
func Test_OcmImageResourcesTemplate_DisabledSkipped(t *testing.T) {
	data := map[string]any{
		"releaseNamespace": "platform-mesh-system",
		"values": map[string]any{
			"ocm": baseOcmValues(),
			"services": map[string]any{
				"disabled-operator": map[string]any{
					"enabled": false,
				},
			},
		},
	}

	docs := renderGotemplateDocs(t, "components/runtime/ocm-image-resources.yaml", data)
	assert.Empty(t, docs, "disabled service must not render any Resource")
}

// Test_OcmImageResourcesTemplate_MultiEntryPreserved verifies that a service with an explicit
// multi-entry imageResources list renders all entries (defaulting must not collapse the list).
func Test_OcmImageResourcesTemplate_MultiEntryPreserved(t *testing.T) {
	data := map[string]any{
		"releaseNamespace": "platform-mesh-system",
		"values": map[string]any{
			"ocm": baseOcmValues(),
			"services": map[string]any{
				"openfga": map[string]any{
					"enabled": true,
					"imageResources": []any{
						map[string]any{
							"name": "openfga-image",
							"annotations": map[string]any{
								"repo": "oci", "artifact": "image", "for": "openfga", "image-ref": "combined",
							},
						},
						map[string]any{
							"name":         "openfga-postgresql-image",
							"resource":     "postgresql-image",
							"resourceName": "postgresql-image",
							"annotations": map[string]any{
								"repo": "oci", "artifact": "image", "for": "openfga",
								"path": "postgresql.image.tag", "unsuspend": "true",
							},
						},
					},
				},
			},
		},
	}

	docs := renderGotemplateDocs(t, "components/runtime/ocm-image-resources.yaml", data)
	require.Len(t, docs, 2, "multi-entry imageResources must render exactly as many Resources as entries")

	names := []string{
		docs[0]["metadata"].(map[string]any)["name"].(string),
		docs[1]["metadata"].(map[string]any)["name"].(string),
	}
	assert.Contains(t, names, "openfga-image")
	assert.Contains(t, names, "openfga-postgresql-image")
}
