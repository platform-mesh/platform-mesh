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

package resources

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/yaml"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

func TestEmbeddedMarketplaceSchemaPermissionClaimCompatibility(t *testing.T) {
	var resourceSchema kcpapisv1alpha1.APIResourceSchema
	require.NoError(t, yaml.Unmarshal([]byte(ResourceSchema), &resourceSchema))
	require.Len(t, resourceSchema.Spec.Versions, 1)

	var schema apiextensionsv1.JSONSchemaProps
	require.NoError(t, json.Unmarshal(resourceSchema.Spec.Versions[0].Schema.Raw, &schema))

	spec := requireSchemaProperty(t, schema, "spec")
	legacyExport := requireSchemaProperty(t, spec, "apiExport")
	legacySpec := requireSchemaProperty(t, legacyExport, "spec")
	legacyClaims := requireSchemaProperty(t, legacySpec, "permissionClaims")
	require.NotNil(t, legacyClaims.Items)
	require.NotNil(t, legacyClaims.Items.Schema)
	legacyGroup := requireSchemaProperty(t, *legacyClaims.Items.Schema, "group")
	require.NotNil(t, legacyGroup.Default, "legacy list-map key must have a schema default")
	assert.JSONEq(t, `""`, string(legacyGroup.Default.Raw))

	authoritativeClaims := requireSchemaProperty(t, spec, "apiExportPermissionClaims")
	require.NotNil(t, authoritativeClaims.Items)
	require.NotNil(t, authoritativeClaims.Items.Schema)
	assert.ElementsMatch(t, []string{"resource", "verbs"}, authoritativeClaims.Items.Schema.Required)
	authoritativeGroup := requireSchemaProperty(t, *authoritativeClaims.Items.Schema, "group")
	require.NotNil(t, authoritativeGroup.Default, "authoritative core-group claims need a schema default")
	assert.JSONEq(t, `""`, string(authoritativeGroup.Default.Raw))
	authoritativeVerbs := requireSchemaProperty(t, *authoritativeClaims.Items.Schema, "verbs")
	require.NotNil(t, authoritativeVerbs.MinItems)
	assert.EqualValues(t, 1, *authoritativeVerbs.MinItems)
	defaultSelector := requireSchemaProperty(t, *authoritativeClaims.Items.Schema, "defaultSelector")
	assert.Contains(t, defaultSelector.Properties, "matchAll")
	assert.Contains(t, defaultSelector.Properties, "matchLabels")
	assert.Contains(t, defaultSelector.Properties, "matchExpressions")
}

func TestGeneratedMarketplaceAPIExportReferencesEmbeddedSchema(t *testing.T) {
	var resourceSchema kcpapisv1alpha1.APIResourceSchema
	require.NoError(t, yaml.Unmarshal([]byte(ResourceSchema), &resourceSchema))

	apiExportYAML, err := os.ReadFile("apiexport-marketplace.platform-mesh.io.yaml")
	require.NoError(t, err)
	var apiExport kcpapisv1alpha2.APIExport
	require.NoError(t, yaml.Unmarshal(apiExportYAML, &apiExport))

	require.Len(t, apiExport.Spec.Resources, 1)
	assert.Equal(t, resourceSchema.Spec.Group, apiExport.Spec.Resources[0].Group)
	assert.Equal(t, resourceSchema.Spec.Names.Plural, apiExport.Spec.Resources[0].Name)
	assert.Equal(t, resourceSchema.Name, apiExport.Spec.Resources[0].Schema)
	assert.NotNil(t, apiExport.Spec.Resources[0].Storage.CRD)
	assert.Nil(t, apiExport.Spec.Resources[0].Storage.Virtual)
}

func requireSchemaProperty(t *testing.T, schema apiextensionsv1.JSONSchemaProps, name string) apiextensionsv1.JSONSchemaProps {
	t.Helper()

	property, found := schema.Properties[name]
	require.True(t, found, "schema property %q is missing", name)
	return property
}
