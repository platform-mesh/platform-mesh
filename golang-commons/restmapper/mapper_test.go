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

package restmapper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	programGK  = schema.GroupKind{Group: "", Kind: "Program"}
	programGVR = schema.GroupVersionResource{Group: "", Version: "v1alpha1", Resource: "programs"}
	programGVK = schema.GroupVersionKind{Group: "", Version: "v1alpha1", Kind: "Program"}

	staticMappings = map[schema.GroupKind]GVRMapping{
		programGK: {GVR: programGVR, Scope: meta.RESTScopeNameNamespace},
	}
)

func TestNewStatic_EmptyReturnsNil(t *testing.T) {
	assert.Nil(t, NewStatic(nil))
	assert.Nil(t, NewStatic(map[schema.GroupKind]GVRMapping{}))
}

func TestNewStatic_RegistersKind(t *testing.T) {
	m := NewStatic(staticMappings)
	require.NotNil(t, m)

	mapping, err := m.RESTMapping(programGK)
	require.NoError(t, err)
	assert.Equal(t, programGVR, mapping.Resource)
}

func TestNewStatic_NamespacedScope(t *testing.T) {
	m := NewStatic(staticMappings)
	mapping, err := m.RESTMapping(programGK)
	require.NoError(t, err)
	assert.Equal(t, meta.RESTScopeNameNamespace, mapping.Scope.Name())
}

func TestNewStatic_RootScope(t *testing.T) {
	orgGK := schema.GroupKind{Group: "platform.io", Kind: "Organization"}
	m := NewStatic(map[schema.GroupKind]GVRMapping{
		orgGK: {
			GVR:   schema.GroupVersionResource{Group: "platform.io", Version: "v1", Resource: "organizations"},
			Scope: meta.RESTScopeNameRoot,
		},
	})
	require.NotNil(t, m)
	mapping, err := m.RESTMapping(orgGK)
	require.NoError(t, err)
	assert.Equal(t, meta.RESTScopeNameRoot, mapping.Scope.Name())
}

// emptyMapper is a DefaultRESTMapper with no registered kinds, so all lookups
// return *meta.NoResourceMatchError or *meta.NoKindMatchError (IsNoMatchError).
func emptyMapper() *meta.DefaultRESTMapper {
	return meta.NewDefaultRESTMapper(nil)
}

func TestMultiRESTMapper_KindFor_PrimaryWins(t *testing.T) {
	static := NewStatic(staticMappings)
	primary := meta.NewDefaultRESTMapper([]schema.GroupVersion{programGVK.GroupVersion()})
	primary.Add(programGVK, meta.RESTScopeNamespace)

	fm := meta.MultiRESTMapper{primary, static}
	gvk, err := fm.KindFor(programGVR)
	require.NoError(t, err)
	assert.Equal(t, programGVK, gvk)
}

func TestMultiRESTMapper_KindFor_FallsBackOnNoMatch(t *testing.T) {
	static := NewStatic(staticMappings)
	fm := meta.MultiRESTMapper{emptyMapper(), static}

	gvk, err := fm.KindFor(programGVR)
	require.NoError(t, err)
	assert.Equal(t, programGVK, gvk)
}

func TestMultiRESTMapper_ResourceSingularizer_FallsBackOnNoMatch(t *testing.T) {
	static := NewStatic(staticMappings)
	fm := meta.MultiRESTMapper{emptyMapper(), static}

	singular, err := fm.ResourceSingularizer("programs")
	require.NoError(t, err)
	assert.Equal(t, "program", singular)
}

func TestMultiRESTMapper_RESTMapping_FallsBackOnNoMatch(t *testing.T) {
	static := NewStatic(staticMappings)
	fm := meta.MultiRESTMapper{emptyMapper(), static}

	mapping, err := fm.RESTMapping(programGK)
	require.NoError(t, err)
	assert.Equal(t, programGVR, mapping.Resource)
}

func TestMultiRESTMapper_RESTMappings_FallsBackOnNoMatch(t *testing.T) {
	static := NewStatic(staticMappings)
	fm := meta.MultiRESTMapper{emptyMapper(), static}

	mappings, err := fm.RESTMappings(programGK)
	require.NoError(t, err)
	require.Len(t, mappings, 1)
	assert.Equal(t, programGVR, mappings[0].Resource)
}
