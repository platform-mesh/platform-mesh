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

package quickstart

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// TestDexValues checks the additions against the kind profile of the operator
// chart, which they are merged over.
func TestDexValues(t *testing.T) {
	// The profile file is the ConfigMap the chart renders, placeholders aside.
	manifest, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "charts", "platform-mesh-operator", "files", "kind-profile.yaml"))
	require.NoError(t, err)

	o := NewOptions(genericiooptions.NewTestIOStreamsDiscard())
	additions, err := o.dexValues(string(manifest))
	require.NoError(t, err)
	values, err := helmValues(operatorValues{PlatformMesh: additions})
	require.NoError(t, err)

	toggles, _, _ := unstructured.NestedStringSlice(values, "platformMesh", "extraFeatureToggles")
	assert.Equal(t, []string{"feature-disable-keycloak"}, toggles)

	issuer, _, _ := unstructured.NestedString(values, "platformMesh", "authentication", "oidc", "issuerURL")
	assert.Equal(t, "https://pm.127-0-0-1.sslip.io:8443/dex", issuer)

	services, _, _ := unstructured.NestedMap(values, "platformMesh", "values", "services")
	// What the operator refuses the toggle for while it is on
	// (FeatureToggleConflicts of platform-mesh-operator).
	for _, path := range [][]string{
		{"pm-keycloak-operator", "enabled"},
		{"iam-service", "enabled"},
		{"iam-ui", "enabled"},
		{"infra", "values", "keycloak", "operator", "enabled"},
	} {
		enabled, found, err := unstructured.NestedBool(services, path...)
		require.NoError(t, err)
		assert.True(t, found, path)
		assert.False(t, enabled, path)
	}

	redirects, _, _ := unstructured.NestedStringSlice(services, "infra", "values", "dex", "staticClient", "redirectURIs")
	assert.Equal(t, []string{"https://pm.127-0-0-1.sslip.io:8443/callback?storageType=none"}, redirects)

	// The environment of the profile is kept, and the client of Dex added.
	env, _, _ := unstructured.NestedSlice(services, "portal", "values", "extraEnvVars")
	names := map[string]any{}
	for _, e := range env {
		e := e.(map[string]any)
		names[e["name"].(string)] = e["value"]
	}
	assert.Contains(t, names, "NODE_EXTRA_CA_CERTS")
	assert.Equal(t, "platform-mesh", names["OIDC_STATIC_CLIENT_ID"])
	assert.Greater(t, len(env), 4)
}

func TestValidateIDP(t *testing.T) {
	for idp, valid := range map[string]bool{"": true, IDPDex: true, IDPKeycloak: true, "okta": false} {
		o := NewOptions(genericiooptions.NewTestIOStreamsDiscard())
		o.IDP = idp
		require.NoError(t, o.Complete(nil, nil))
		if valid {
			assert.NoError(t, o.Validate(), idp)
		} else {
			assert.Error(t, o.Validate(), idp)
		}
	}
}
