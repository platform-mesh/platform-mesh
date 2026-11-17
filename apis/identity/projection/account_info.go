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

// Package projection builds public identity metadata without provider-specific
// administration or issuer URL construction.
package projection

import (
	"errors"
	"slices"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/apis/identity/authentication"
	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"

	"github.com/kcp-dev/logicalcluster/v3"
)

// AccountInfo returns a detached public projection. The caller owns updating
// only AccountInfo.spec.oidc, not unrelated account/FGA/cluster metadata.
func AccountInfo(class *pmidentityv1alpha1.IdentityProviderClass, tenant *pmidentityv1alpha1.IdentityTenant) (*pmcorev1alpha1.OIDCInfo, error) {
	if _, err := authentication.Resolve(class, tenant, ""); err != nil {
		return nil, err
	}
	cluster := logicalcluster.From(tenant)
	if !cluster.IsValid() || tenant.UID == "" || tenant.Name == "" {
		return nil, errors.New("public identity projection requires immutable KCP tenant identity")
	}
	out := &pmcorev1alpha1.OIDCInfo{
		IssuerURL: tenant.Status.IssuerURL,
		Identity: &pmcorev1alpha1.OIDCIdentityContext{
			Tenant: pmcorev1alpha1.IdentityTenantReference{
				LogicalCluster: cluster.String(), Name: tenant.Name, UID: tenant.UID,
			},
			ProviderClassRef: class.Name, ProviderClassUID: class.UID,
			ConfigurationRevision: tenant.Status.ObservedConfigurationRevision,
			DiscoveryURL:          tenant.Status.DiscoveryURL,
			IssuerMode:            string(class.Spec.AuthenticationPolicy.IssuerMode),
			SubjectMode:           string(tenant.Status.SubjectMode),
		},
		Clients: make(map[string]pmcorev1alpha1.ClientInfo, len(tenant.Spec.Clients)),
	}
	managed := make(map[string]pmidentityv1alpha1.ManagedClient, len(tenant.Status.Clients))
	for _, client := range tenant.Status.Clients {
		managed[client.Name] = client
	}
	for _, desired := range tenant.Spec.Clients {
		client := managed[desired.Name]
		out.Clients[desired.Name] = pmcorev1alpha1.ClientInfo{
			ClientID: client.ClientID, Purpose: string(desired.Purpose), Scopes: slices.Clone(desired.Scopes),
			CredentialsSecretRef: client.CredentialsSecretRef.DeepCopy(),
		}
	}
	return out, nil
}
