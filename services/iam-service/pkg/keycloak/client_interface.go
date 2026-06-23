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
	"context"

	keycloakClient "go.platform-mesh.io/iam-service/pkg/keycloak/client"
)

// KeycloakClientInterface defines the subset of Keycloak client methods we use
type KeycloakClientInterface interface {
	GetUsersWithResponse(ctx context.Context, realm string, params *keycloakClient.GetUsersParams, reqEditors ...keycloakClient.RequestEditorFn) (*keycloakClient.GetUsersResponse, error)
}

// Ensure the generated client implements our interface
var _ KeycloakClientInterface = (*keycloakClient.ClientWithResponses)(nil)
