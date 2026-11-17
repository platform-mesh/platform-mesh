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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

func TestPreserveProfileCredentials(t *testing.T) {
	const replacement = `
components:
  services:
    infra:
      imageResources:
      - credentials-image
      values:
        keycloak:
          operator:
            admin:
              username: keycloak-admin
              password: ''
              existingSecret: ''
            db:
              host: platform-mesh-pg-rw
              password: ''
              existingSecret: ''
        cnpg:
          roles:
            keycloak:
              password: ''
            openfga:
              password: ''
    pm-keycloak-operator:
      values:
        keycloakSecret: keycloak-initial-admin
        initContainer:
          keycloakUser: temp-admin
        newSetting: retained
`
	tests := []struct {
		name      string
		installed string
		expected  map[string]any
		unchanged bool
		wantError bool
	}{
		{name: "legacy explicit credentials survive operator split", installed: `
components:
  services:
    infra:
      values:
        keycloak:
          operator:
            admin:
              username: legacy-admin
              password: legacy-password
              secret: legacy-client
            db:
              host: old-host
              username: legacy-db-user
              password: database-password
        cnpg:
          roles:
            keycloak:
              username: legacy-db-user
              password: database-password
            openfga:
              username: legacy-openfga-user
              password: openfga-password
    security-operator:
      values:
        keycloakSecret: keycloak-admin
        initContainer:
          keycloakUser: legacy-admin
`, unchanged: false, wantError: false, expected: map[string]any{"components.services.infra.values.keycloak.operator.admin.username": "legacy-admin", "components.services.infra.values.keycloak.operator.admin.password": "legacy-password", "components.services.infra.values.keycloak.operator.admin.secret": "legacy-client", "components.services.infra.values.keycloak.operator.db.password": "database-password", "components.services.infra.values.keycloak.operator.db.username": "legacy-db-user", "components.services.infra.values.cnpg.roles.keycloak.password": "database-password", "components.services.infra.values.cnpg.roles.keycloak.username": "legacy-db-user", "components.services.infra.values.cnpg.roles.openfga.password": "openfga-password", "components.services.infra.values.cnpg.roles.openfga.username": "legacy-openfga-user", "components.services.pm-keycloak-operator.values.keycloakSecret": "keycloak-admin", "components.services.pm-keycloak-operator.values.initContainer.keycloakUser": "legacy-admin"}},
		{name: "external secrets and current operator take precedence", installed: `
components:
  services:
    infra:
      values:
        keycloak:
          operator:
            admin:
              username: external-admin
              existingSecret: external-admin-secret
            db:
              existingSecret: external-db-secret
    pm-keycloak-operator:
      values:
        keycloakSecret: external-admin-secret
        initContainer:
          keycloakUser: external-admin
    security-operator:
      values:
        keycloakSecret: stale-secret
        initContainer:
          keycloakUser: stale-user
`, unchanged: false, wantError: false, expected: map[string]any{"components.services.infra.values.keycloak.operator.admin.username": "external-admin", "components.services.infra.values.keycloak.operator.admin.existingSecret": "external-admin-secret", "components.services.infra.values.keycloak.operator.db.existingSecret": "external-db-secret", "components.services.pm-keycloak-operator.values.keycloakSecret": "external-admin-secret", "components.services.pm-keycloak-operator.values.initContainer.keycloakUser": "external-admin"}},
		{name: "native credentials remain native", installed: `
components:
  services:
    infra:
      imageResources:
      - credentials-image
      values:
        keycloak:
          operator:
            admin:
              username: keycloak-admin
              password: ''
              existingSecret: ''
            db:
              host: platform-mesh-pg-rw
              password: ''
              existingSecret: ''
        cnpg:
          roles:
            keycloak:
              password: ''
            openfga:
              password: ''
    pm-keycloak-operator:
      values:
        keycloakSecret: keycloak-initial-admin
        initContainer:
          keycloakUser: temp-admin
        newSetting: retained
`, unchanged: true, wantError: false, expected: map[string]any{}},
		{name: "unconfigured credentials preserve fresh defaults", installed: `
components:
  services:
    infra:
      values:
        keycloak:
          operator:
            admin:
              username: unused
`, unchanged: true, wantError: false, expected: map[string]any{}},
		{name: "invalid profile fails before replacement", installed: `components: [`, unchanged: false, wantError: true, expected: map[string]any{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := preserveProfileCredentials(tt.installed, replacement)
			if tt.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.unchanged {
				require.Equal(t, replacement, result)
				return
			}
			var actual map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(result), &actual))
			for path, expected := range tt.expected {
				value, found, err := unstructured.NestedFieldCopy(actual, strings.Split(path, ".")...)
				require.NoError(t, err)
				require.True(t, found, "missing field %s", path)
				require.Equal(t, expected, value, "field %s", path)
			}
			for path, expected := range map[string]any{
				"components.services.infra.values.keycloak.operator.db.host": "platform-mesh-pg-rw",
				"components.services.pm-keycloak-operator.values.newSetting": "retained",
			} {
				value, found, err := unstructured.NestedFieldCopy(actual, strings.Split(path, ".")...)
				require.NoError(t, err)
				require.True(t, found)
				require.Equal(t, expected, value)
			}
			_, found, err := unstructured.NestedMap(actual, "components", "services", "security-operator")
			require.NoError(t, err)
			require.False(t, found)
			again, err := preserveProfileCredentials(result, replacement)
			require.NoError(t, err)
			require.Equal(t, result, again)
		})
	}
}

// The selected credential fields match the v26.10.2 kind profile. Its
// security-operator values omit both admin consumer settings and its admin
// values omit username: all three came from the old charts' defaults.
func TestPreserveProfileCredentialConsumers(t *testing.T) {
	const installed = `{
  "components": {
    "services": {
      "infra": {
        "values": {
          "keycloak": {
            "operator": {
              "admin": {
                "password": "admin",
                "secret": "admin"
              },
              "db": {
                "password": "keycloak-password"
              }
            }
          },
          "cnpg": {
            "roles": {
              "keycloak": {
                "password": "keycloak-password"
              },
              "openfga": {
                "password": "openfga-password"
              }
            }
          }
        }
      },
      "security-operator": {
        "values": {
          "caSecret": "domain-certificate"
        }
      }
    }
  }
}`
	const split = `{"components":{"services":{"infra":{"values":{}},"pm-keycloak-operator":{"values":{"keycloakSecret":"keycloak-initial-admin","initContainer":{"keycloakUser":"temp-admin"}}}}}}`
	const legacy = `{"components":{"services":{"infra":{"values":{}},"security-operator":{"values":{}}}}}`
	const withoutConsumer = `{"components":{"services":{"infra":{"values":{}}}}}`
	tests := []struct {
		name        string
		replacement string
		admin       map[string]any
		consumer    string
		secret      string
		username    string
	}{
		{name: "v26.10.2 implicit defaults move to split operator", replacement: split, consumer: "pm-keycloak-operator", secret: "keycloak-admin", username: "keycloak-admin"},
		{name: "repeated pre-split install keeps the legacy service", replacement: legacy, consumer: "security-operator", secret: "keycloak-admin", username: "keycloak-admin"},
		{name: "profile without a consumer does not add one", replacement: withoutConsumer},
		{name: "explicit admin username supplies absent consumer setting", replacement: split, admin: map[string]any{"password": "configured-password", "username": "configured-user"}, consumer: "pm-keycloak-operator", secret: "keycloak-admin", username: "configured-user"},
		{name: "external admin secret supplies absent consumer setting", replacement: split, admin: map[string]any{"existingSecret": "external-secret", "username": "external-user"}, consumer: "pm-keycloak-operator", secret: "external-secret", username: "external-user"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := installed
			if tt.admin != nil {
				var profile map[string]any
				require.NoError(t, yaml.Unmarshal([]byte(input), &profile))
				require.NoError(t, unstructured.SetNestedMap(profile, tt.admin, "components", "services", "infra", "values", "keycloak", "operator", "admin"))
				data, err := yaml.Marshal(profile)
				require.NoError(t, err)
				input = string(data)
			}
			result, err := preserveProfileCredentials(input, tt.replacement)
			require.NoError(t, err)
			var actual map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(result), &actual))
			if tt.consumer == "" {
				for _, name := range []string{"security-operator", "pm-keycloak-operator"} {
					_, found, err := unstructured.NestedMap(actual, "components", "services", name)
					require.NoError(t, err)
					require.False(t, found)
				}
				return
			}
			secret, _, err := unstructured.NestedString(actual, "components", "services", tt.consumer, "values", "keycloakSecret")
			require.NoError(t, err)
			require.Equal(t, tt.secret, secret)
			username, _, err := unstructured.NestedString(actual, "components", "services", tt.consumer, "values", "initContainer", "keycloakUser")
			require.NoError(t, err)
			require.Equal(t, tt.username, username)
			again, err := preserveProfileCredentials(result, tt.replacement)
			require.NoError(t, err)
			require.Equal(t, result, again)
		})
	}
}
