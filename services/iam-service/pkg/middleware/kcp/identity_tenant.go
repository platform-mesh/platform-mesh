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

package kcp

import (
	"context"
	"errors"
	"fmt"

	pmidentityv1alpha1 "go.platform-mesh.io/apis/identity/v1alpha1"
	"go.platform-mesh.io/golang-commons/idpdirectory"
	"go.platform-mesh.io/iam-service/pkg/accountinfo"

	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	"github.com/kcp-dev/logicalcluster/v3"
)

func NewIdentityTenantResolver(retriever accountinfo.Retriever) func(context.Context, string, string) (idpdirectory.Tenant, error) {
	return func(ctx context.Context, organization, issuer string) (idpdirectory.Tenant, error) {
		if retriever == nil || organization == "" || issuer == "" {
			return idpdirectory.Tenant{}, errors.New("account info retriever, organization and issuer are required")
		}
		info, err := retriever.Get(ctx, multicluster.ClusterName("root:orgs:"+organization))
		if err != nil {
			return idpdirectory.Tenant{}, fmt.Errorf("read organization AccountInfo: %w", err)
		}
		if info.Spec.Organization.Name != organization || info.Spec.OIDC == nil ||
			info.Spec.OIDC.Identity == nil || info.Spec.OIDC.IssuerURL != issuer {
			return idpdirectory.Tenant{}, errors.New("organization AccountInfo does not match the authenticated identity tenant")
		}
		identity := info.Spec.OIDC.Identity
		if identity.ProviderClassRef == "" || identity.ProviderClassUID == "" ||
			identity.IssuerMode != string(pmidentityv1alpha1.IssuerModeDedicated) ||
			identity.SubjectMode != string(pmidentityv1alpha1.SubjectModePublic) {
			return idpdirectory.Tenant{}, errors.New("organization AccountInfo identity profile is unsupported")
		}
		logicalCluster := logicalcluster.Name(identity.Tenant.LogicalCluster)
		if !logicalCluster.IsValid() || identity.Tenant.Name == "" || identity.Tenant.UID == "" ||
			info.Spec.Organization.GeneratedClusterId != identity.Tenant.LogicalCluster {
			return idpdirectory.Tenant{}, errors.New("organization AccountInfo contains an invalid identity tenant reference")
		}
		return idpdirectory.Tenant{
			LogicalCluster: identity.Tenant.LogicalCluster,
			Name:           identity.Tenant.Name,
			UID:            string(identity.Tenant.UID),
		}, nil
	}
}
