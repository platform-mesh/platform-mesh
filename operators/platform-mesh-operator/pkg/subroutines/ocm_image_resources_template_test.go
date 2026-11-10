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

func Test_OcmImageResourcesTemplate(t *testing.T) {
	data := map[string]any{
		"releaseNamespace": "platform-mesh-system",
		"values": map[string]any{
			"ocm": map[string]any{
				"component":     map[string]any{"name": "platform-mesh"},
				"repo":          map[string]any{"name": "platform-mesh"},
				"referencePath": []any{map[string]any{"name": "github.com/platform-mesh/account-operator"}},
			},
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

	spec := obj["spec"].(map[string]any)
	asf, ok := spec["additionalStatusFields"].(map[string]any)
	require.True(t, ok, "spec.additionalStatusFields must be rendered")
	assert.Equal(t, "resource.access.imageReference.toOCI().registry", asf["registry"])
	assert.Equal(t, "resource.access.imageReference.toOCI().repository", asf["repository"])
	assert.Equal(t, "resource.access.imageReference.toOCI().tag", asf["tag"])
	assert.Equal(t, "resource.access.imageReference.toOCI().digest", asf["digest"])
}
