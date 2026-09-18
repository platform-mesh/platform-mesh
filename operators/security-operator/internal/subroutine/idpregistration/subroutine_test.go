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

package idpregistration

import (
	"testing"

	"github.com/stretchr/testify/assert"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/pkg/clientreg/keycloak"
	"go.platform-mesh.io/subroutines/conditions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEnsureManagedIdentityProvider(t *testing.T) {
	t.Parallel()

	reg := &pmcorev1alpha1.IdPRegistration{Spec: pmcorev1alpha1.IdPRegistrationSpec{Alias: "corp"}}

	err := ensureManagedIdentityProvider(&keycloak.IdentityProviderRepresentation{
		Config: map[string]string{keycloak.PlatformMeshManagedByKey: keycloak.PlatformMeshManagedByIdPRegistration},
	}, reg)
	assert.NoError(t, err)

	err = ensureManagedIdentityProvider(&keycloak.IdentityProviderRepresentation{
		Config: map[string]string{keycloak.PlatformMeshManagedByKey: "other"},
	}, reg)
	assert.Error(t, err)

	err = ensureManagedIdentityProvider(&keycloak.IdentityProviderRepresentation{}, reg)
	assert.Error(t, err)

	reg.Status.Conditions = []metav1.Condition{{
		Type:   conditions.ReadyCondition,
		Status: metav1.ConditionTrue,
	}}
	err = ensureManagedIdentityProvider(&keycloak.IdentityProviderRepresentation{}, reg)
	assert.NoError(t, err)
}

func TestOwnedByIdPRegistration(t *testing.T) {
	t.Parallel()

	assert.False(t, ownedByIdPRegistration(nil))
	assert.False(t, ownedByIdPRegistration(&keycloak.IdentityProviderRepresentation{}))
	assert.False(t, ownedByIdPRegistration(&keycloak.IdentityProviderRepresentation{
		Config: map[string]string{keycloak.PlatformMeshManagedByKey: "other"},
	}))
	assert.True(t, ownedByIdPRegistration(&keycloak.IdentityProviderRepresentation{
		Config: map[string]string{keycloak.PlatformMeshManagedByKey: keycloak.PlatformMeshManagedByIdPRegistration},
	}))
}

func TestOrganizationAlias(t *testing.T) {
	t.Parallel()

	reg := &pmcorev1alpha1.IdPRegistration{Spec: pmcorev1alpha1.IdPRegistrationSpec{Alias: "dex"}}
	assert.Equal(t, "acme-dex-domains", organizationAlias("acme", reg))
}

func TestSecretNamespace(t *testing.T) {
	t.Parallel()

	s := &subroutine{cfg: &config.Config{}}
	assert.Equal(t, "default", s.secretNamespace())

	s.cfg.IDP.IdPRegistrationSecretNamespace = "secrets"
	assert.Equal(t, "secrets", s.secretNamespace())
}
