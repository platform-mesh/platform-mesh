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

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// renderServiceHelmReleases renders gotemplates/components/infra/helmreleases.yaml for a profile
// and returns the HelmReleases keyed by name.
func renderServiceHelmReleases(t *testing.T, profileYAML string) map[string]*unstructured.Unstructured {
	t.Helper()
	sub, inst := newSubroutineForProfile(t, profileYAML, platformMeshNamespace)

	vars, _, err := sub.buildComponentsTemplateVars(context.Background(), inst, apiextensionsv1.JSON{})
	require.NoError(t, err)

	objs, err := sub.renderTemplateFile("components/infra/helmreleases.yaml", vars, testLogger(t))
	require.NoError(t, err)

	releases := map[string]*unstructured.Unstructured{}
	for _, obj := range objs {
		if obj.GetKind() == "HelmRelease" {
			releases[obj.GetName()] = obj
		}
	}
	return releases
}

// A service's postRenderers block is passed through to the HelmRelease verbatim, so a profile
// can patch chart output the chart itself has no values for. Services without one get no key.
func Test_ServiceHelmRelease_PostRenderersPassThrough(t *testing.T) {
	releases := renderServiceHelmReleases(t, loadShippedProfile(t))

	infra, ok := releases["infra"]
	require.True(t, ok, "the shipped profile must render the infra HelmRelease")

	postRenderers, found, err := unstructured.NestedSlice(infra.Object, "spec", "postRenderers")
	require.NoError(t, err)
	require.True(t, found, "infra carries postRenderers in the shipped profile")
	require.Len(t, postRenderers, 1)

	patches, found, err := unstructured.NestedSlice(postRenderers[0].(map[string]any), "kustomize", "patches")
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, patches, 1)
	target, _, _ := unstructured.NestedMap(patches[0].(map[string]any), "target")
	assert.Equal(t, "CacheServer", target["kind"])
	assert.Equal(t, "cache-server", target["name"])

	kcpOperator, ok := releases["kcp-operator"]
	require.True(t, ok)
	_, found, err = unstructured.NestedSlice(kcpOperator.Object, "spec", "postRenderers")
	require.NoError(t, err)
	assert.False(t, found, "a service without postRenderers must not render the key")
}
