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
	"testing"

	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/iam-service/pkg/accountinfo"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
)

type accountInfoRetrieverFunc func(context.Context, multicluster.ClusterName) (*pmcorev1alpha1.AccountInfo, error)

func (f accountInfoRetrieverFunc) Get(ctx context.Context, cluster multicluster.ClusterName) (*pmcorev1alpha1.AccountInfo, error) {
	return f(ctx, cluster)
}

var _ accountinfo.Retriever = accountInfoRetrieverFunc(nil)

func TestNewIdentityTenantResolverValidatesAccountInfoBinding(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		resolver := NewIdentityTenantResolver(accountInfoRetrieverFunc(func(_ context.Context, cluster multicluster.ClusterName) (*pmcorev1alpha1.AccountInfo, error) {
			require.Equal(t, multicluster.ClusterName("root:orgs:acme"), cluster)
			return testAccountInfo(), nil
		}))
		tenant, err := resolver(t.Context(), "acme", "https://issuer.example/realms/acme")
		require.NoError(t, err)
		require.Equal(t, "tenant-cluster", tenant.LogicalCluster)
		require.Equal(t, "acme", tenant.Name)
		require.Equal(t, "tenant-uid", tenant.UID)
	})

	tests := []struct {
		name   string
		change func(*pmcorev1alpha1.AccountInfo)
	}{
		{name: "issuer mismatch", change: func(info *pmcorev1alpha1.AccountInfo) {
			info.Spec.OIDC.IssuerURL = "https://other.example"
		}},
		{name: "missing generic identity", change: func(info *pmcorev1alpha1.AccountInfo) {
			info.Spec.OIDC.Identity = nil
		}},
		{name: "shared issuer", change: func(info *pmcorev1alpha1.AccountInfo) {
			info.Spec.OIDC.Identity.IssuerMode = "Shared"
		}},
		{name: "pairwise subject", change: func(info *pmcorev1alpha1.AccountInfo) {
			info.Spec.OIDC.Identity.SubjectMode = "Pairwise"
		}},
		{name: "tenant cluster mismatch", change: func(info *pmcorev1alpha1.AccountInfo) {
			info.Spec.OIDC.Identity.Tenant.LogicalCluster = "another-cluster"
		}},
		{name: "organization mismatch", change: func(info *pmcorev1alpha1.AccountInfo) {
			info.Spec.Organization.Name = "another-org"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := testAccountInfo()
			test.change(info)
			resolver := NewIdentityTenantResolver(accountInfoRetrieverFunc(func(context.Context, multicluster.ClusterName) (*pmcorev1alpha1.AccountInfo, error) {
				return info, nil
			}))
			_, err := resolver(t.Context(), "acme", "https://issuer.example/realms/acme")
			require.Error(t, err)
		})
	}
}

func TestIdentityTenantResolverReturnsAccountInfoError(t *testing.T) {
	resolver := NewIdentityTenantResolver(accountInfoRetrieverFunc(func(context.Context, multicluster.ClusterName) (*pmcorev1alpha1.AccountInfo, error) {
		return nil, errors.New("unavailable")
	}))
	_, err := resolver(t.Context(), "acme", "https://issuer.example/realms/acme")
	require.ErrorContains(t, err, "read organization AccountInfo")
}

func testAccountInfo() *pmcorev1alpha1.AccountInfo {
	return &pmcorev1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account-info"},
		Spec: pmcorev1alpha1.AccountInfoSpec{
			Organization: pmcorev1alpha1.AccountLocation{
				Name: "acme", GeneratedClusterId: "tenant-cluster",
			},
			OIDC: &pmcorev1alpha1.OIDCInfo{
				IssuerURL: "https://issuer.example/realms/acme",
				Identity: &pmcorev1alpha1.OIDCIdentityContext{
					Tenant: pmcorev1alpha1.IdentityTenantReference{
						LogicalCluster: "tenant-cluster", Name: "acme", UID: types.UID("tenant-uid"),
					},
					ProviderClassRef: "default", ProviderClassUID: types.UID("class-uid"),
					ConfigurationRevision: "config-v1", IssuerMode: "Dedicated", SubjectMode: "Public",
				},
			},
		},
	}
}
