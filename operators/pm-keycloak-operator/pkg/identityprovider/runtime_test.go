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

package identityprovider

import (
	"testing"

	"github.com/stretchr/testify/require"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"

	"github.com/kcp-dev/logicalcluster/v3"
)

func TestValidateTenantCluster(t *testing.T) {
	tests := []struct {
		name    string
		purpose pmidentityv1alpha1.IdentityTenantPurpose
		cluster logicalcluster.Name
		welcome logicalcluster.Name
		wantErr bool
	}{
		{
			name:    "welcome tenant in the trusted welcome workspace",
			purpose: pmidentityv1alpha1.IdentityTenantPurposeWelcome,
			cluster: "welcome-cluster",
			welcome: "welcome-cluster",
		},
		{
			name:    "welcome tenant outside the trusted workspace",
			purpose: pmidentityv1alpha1.IdentityTenantPurposeWelcome,
			cluster: "organization-cluster",
			welcome: "welcome-cluster",
			wantErr: true,
		},
		{
			name:    "organization tenant in another workspace",
			purpose: pmidentityv1alpha1.IdentityTenantPurposeOrganization,
			cluster: "organization-cluster",
			welcome: "welcome-cluster",
		},
		{
			name:    "organization tenant in the welcome workspace",
			purpose: pmidentityv1alpha1.IdentityTenantPurposeOrganization,
			cluster: "welcome-cluster",
			welcome: "welcome-cluster",
			wantErr: true,
		},
		{
			name:    "unsupported purpose",
			purpose: "Other",
			cluster: "welcome-cluster",
			welcome: "welcome-cluster",
			wantErr: true,
		},
		{
			name:    "missing immutable cluster identity",
			purpose: pmidentityv1alpha1.IdentityTenantPurposeWelcome,
			welcome: "welcome-cluster",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTenantCluster(tt.purpose, tt.cluster, tt.welcome)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
