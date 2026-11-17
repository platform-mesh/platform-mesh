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

package keycloak

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmkeycloakv1alpha1 "go.platform-mesh.io/apis/keycloak/v1alpha1"
	keycloakresources "go.platform-mesh.io/apis/static-assets/resources/pm-keycloak-operator"
	"go.platform-mesh.io/platform-mesh/tests/framework/fixtures"
	"go.platform-mesh.io/platform-mesh/tests/framework/kcp"
	"go.platform-mesh.io/platform-mesh/tests/framework/scheme"

	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const keycloakExport = "keycloak.platform-mesh.io"

// IntegrationSuite shares one platform-mesh-system with the keycloak export
// bound between the tests.
type IntegrationSuite struct {
	suite.Suite

	// systemClient is the client of :root:platform-mesh-system.
	systemClient ctrlruntimeclient.Client
}

func TestIntegrationSuite(t *testing.T) {
	suite.Run(t, new(IntegrationSuite))
}

func (s *IntegrationSuite) SetupSuite() {
	t := s.T()
	env, err := kcp.Default(t.Context())
	require.NoError(t, err)
	platform := fixtures.Install(t, env,
		fixtures.WithScheme(scheme.New(t, pmcorev1alpha1.AddToScheme, pmkeycloakv1alpha1.AddToScheme)),
		fixtures.WithResources(keycloakresources.Resources, "*.yaml"),
		fixtures.WithBindings(keycloakExport),
	)
	s.systemClient = platform.SystemClient
}
