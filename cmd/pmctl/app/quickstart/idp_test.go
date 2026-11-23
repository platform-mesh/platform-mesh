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

	// Kubeconfigs log in with a public client, whose tokens kcp accepts as
	// the client of the portal trusts it.
	kubectlClient, _, _ := unstructured.NestedString(values, "platformMesh", "authentication", "oidc", "kubectlClientID")
	assert.Equal(t, "kubectl", kubectlClient)
	kubectlScopes, _, _ := unstructured.NestedStringSlice(values, "platformMesh", "authentication", "oidc", "kubectlScopes")
	assert.Equal(t, []string{"email", "groups", "audience:server:client_id:platform-mesh"}, kubectlScopes)
	peers, _, _ := unstructured.NestedStringSlice(services, "infra", "values", "dex", "staticClient", "trustedPeers")
	assert.Equal(t, []string{"kubectl"}, peers)
	extraClients, _, _ := unstructured.NestedSlice(services, "infra", "values", "dex", "extraStaticClients")
	assert.Equal(t, []any{map[string]any{"id": "kubectl", "name": "kubectl", "public": true}}, extraClients)

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

// TestWithServiceValues checks that ServiceValues are merged over the values
// quickstart sets itself, and stand alone where it sets none.
func TestWithServiceValues(t *testing.T) {
	o := NewOptions(genericiooptions.NewTestIOStreamsDiscard())
	o.ServiceValues = map[string]map[string]any{
		"infra":   {"kcp": map[string]any{"cacheServer": map[string]any{"replicas": 1}}, "dex": map[string]any{"enabled": false}},
		"gateway": {"resources": map[string]any{"requests": map[string]any{"cpu": "50m"}}},
	}
	o.ServiceDependsOn = map[string][]string{"infra": {"kcp-operator"}}

	set := operatorValues{PlatformMesh: &platformMeshValues{Values: &platformMeshSpecValues{Services: map[string]serviceValues{
		"infra": {Values: infraValues{Dex: dexValues{Enabled: true, Issuer: "https://dex"}}},
	}}}}
	for name, in := range map[string]operatorValues{"over quickstart's": set, "alone": {}} {
		t.Run(name, func(t *testing.T) {
			out, err := o.withServiceValues(in)
			require.NoError(t, err)
			values, err := helmValues(out)
			require.NoError(t, err)
			services, _, _ := unstructured.NestedMap(values, "platformMesh", "values", "services")

			replicas, _, _ := unstructured.NestedFieldNoCopy(services, "infra", "values", "kcp", "cacheServer", "replicas")
			assert.EqualValues(t, 1, replicas)
			cpu, _, _ := unstructured.NestedString(services, "gateway", "values", "resources", "requests", "cpu")
			assert.Equal(t, "50m", cpu)
			dependsOn, _, _ := unstructured.NestedSlice(services, "infra", "dependsOn")
			assert.Equal(t, []any{map[string]any{"name": "kcp-operator"}}, dependsOn)
			dex, _, _ := unstructured.NestedBool(services, "infra", "values", "dex", "enabled")
			assert.False(t, dex)
			if in.PlatformMesh != nil {
				issuer, _, _ := unstructured.NestedString(services, "infra", "values", "dex", "issuer")
				assert.Equal(t, "https://dex", issuer)
			}
		})
	}
}
