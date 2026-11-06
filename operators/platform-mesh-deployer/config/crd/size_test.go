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

package crd_test

import (
	"encoding/json"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/platform-mesh-deployer/config/crd"

	"sigs.k8s.io/yaml"
)

// lastAppliedMax is the limit on the kubectl.kubernetes.io/last-applied-configuration
// annotation. A CRD over it can only be applied server-side.
const lastAppliedMax = 262144

// TestCRDsFitLastAppliedAnnotation keeps the CRDs client-side applicable. The
// topology templates are separate CRDs precisely because inlining them put
// PlatformMesh far over this limit.
func TestCRDsFitLastAppliedAnnotation(t *testing.T) {
	t.Parallel()

	entries, err := fs.ReadDir(crd.Resources, ".")
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	for _, entry := range entries {
		file := entry.Name()
		if file == "kustomization.yaml" {
			continue
		}
		t.Run(file, func(t *testing.T) {
			t.Parallel()
			raw, err := crd.Resources.ReadFile(file)
			require.NoError(t, err)
			// The annotation holds the applied object as JSON, not YAML.
			var obj any
			require.NoError(t, yaml.Unmarshal(raw, &obj))
			encoded, err := json.Marshal(obj)
			require.NoError(t, err)
			assert.LessOrEqual(t, len(encoded), lastAppliedMax)
		})
	}
}
