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
	"fmt"
	"reflect"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// preserveProfileCredentials keeps the credentials of an existing database
// when quickstart replaces the shipped profile. Explicit passwords also keep
// their Helm-managed Secrets in the first upgrade that adds deletion protection.
func preserveProfileCredentials(installed, replacement string) (string, error) {
	var old, next map[string]any
	if err := yaml.Unmarshal([]byte(installed), &old); err != nil {
		return "", fmt.Errorf("reading installed profile: %w", err)
	}
	if err := yaml.Unmarshal([]byte(replacement), &next); err != nil {
		return "", fmt.Errorf("reading replacement profile: %w", err)
	}
	if old == nil || next == nil {
		return "", fmt.Errorf("profiles must be YAML mappings")
	}
	changed := false
	copyField := func(from, to string) error {
		value, found, err := unstructured.NestedFieldCopy(old, strings.Split(from, ".")...)
		if err != nil || !found || value == nil || value == "" {
			return err
		}
		current, _, err := unstructured.NestedFieldCopy(next, strings.Split(to, ".")...)
		if err != nil || reflect.DeepEqual(value, current) {
			return err
		}
		if err := unstructured.SetNestedField(next, value, strings.Split(to, ".")...); err != nil {
			return err
		}
		changed = true
		return nil
	}
	const infra = "components.services.infra.values."
	adminPath := strings.Split(infra+"keycloak.operator.admin", ".")
	admin, _, err := unstructured.NestedMap(old, adminPath...)
	if err != nil {
		return "", err
	}
	adminConfigured := admin["password"] != nil && admin["password"] != "" || admin["existingSecret"] != nil && admin["existingSecret"] != ""
	if adminConfigured {
		if err := copyField(infra+"keycloak.operator.admin", infra+"keycloak.operator.admin"); err != nil {
			return "", err
		}
	}
	for _, field := range []string{"keycloak.operator.db.password", "keycloak.operator.db.existingSecret"} {
		if err := copyField(infra+field, infra+field); err != nil {
			return "", err
		}
	}
	dbPassword, _, err := unstructured.NestedString(old, strings.Split(infra+"keycloak.operator.db.password", ".")...)
	if err != nil {
		return "", err
	}
	if dbPassword != "" {
		if err := copyField(infra+"keycloak.operator.db.username", infra+"keycloak.operator.db.username"); err != nil {
			return "", err
		}
	}
	roles, _, err := unstructured.NestedMap(old, strings.Split(infra+"cnpg.roles", ".")...)
	if err != nil {
		return "", err
	}
	for role := range roles {
		path := infra + "cnpg.roles." + role + ".password"
		if err := copyField(path, path); err != nil {
			return "", err
		}
		password, _, err := unstructured.NestedString(old, strings.Split(path, ".")...)
		if err != nil {
			return "", err
		}
		if password != "" {
			username := infra + "cnpg.roles." + role + ".username"
			if err := copyField(username, username); err != nil {
				return "", err
			}
		}
	}
	const services = "components.services."
	consumer := "pm-keycloak-operator"
	if _, found, err := unstructured.NestedMap(old, "components", "services", consumer); err != nil {
		return "", err
	} else if !found {
		consumer = "security-operator"
	}
	if adminConfigured {
		defaults := map[string]any{
			"keycloakSecret":             "keycloak-admin",
			"initContainer.keycloakUser": "keycloak-admin",
		}
		if secret, ok := admin["existingSecret"].(string); ok && secret != "" {
			defaults["keycloakSecret"] = secret
		}
		if username, ok := admin["username"].(string); ok && username != "" {
			defaults["initContainer.keycloakUser"] = username
		}
		for field, value := range defaults {
			path := strings.Split(services+consumer+".values."+field, ".")
			configured, _, err := unstructured.NestedString(old, path...)
			if err != nil {
				return "", err
			}
			if configured == "" {
				if err := unstructured.SetNestedField(old, value, path...); err != nil {
					return "", err
				}
			}
		}
	}
	target := "pm-keycloak-operator"
	_, targetFound, err := unstructured.NestedMap(next, "components", "services", target)
	if err != nil {
		return "", err
	}
	if !targetFound {
		target = "security-operator"
		_, targetFound, err = unstructured.NestedMap(next, "components", "services", target)
		if err != nil {
			return "", err
		}
	}
	if targetFound {
		for _, field := range []string{"keycloakSecret", "initContainer.keycloakUser"} {
			if err := copyField(services+consumer+".values."+field, services+target+".values."+field); err != nil {
				return "", err
			}
		}
	}
	if !changed {
		return replacement, nil
	}
	data, err := yaml.Marshal(next)
	return string(data), err
}
