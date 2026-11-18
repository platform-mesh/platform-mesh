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

package manageaccountinfo_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/account-operator/pkg/subroutines/manageaccountinfo"
	"go.platform-mesh.io/account-operator/pkg/subroutines/mocks"
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/golang-commons/logger/testlogger"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

// TestManageAccountInfoOIDC covers who decides the OIDC of an AccountInfo:
// the issuer the operator is configured with, or, without one, whoever
// provisions the identity provider of the organization.
func TestManageAccountInfoOIDC(t *testing.T) {
	t.Parallel()

	const (
		parentCluster  = "parent-cluster"
		accountCluster = "account-cluster"
	)

	configured := &pmcorev1alpha1.OIDCInfo{
		IssuerURL: "https://issuer.example/dex",
		Clients:   map[string]pmcorev1alpha1.ClientInfo{"kubectl": {ClientID: "platform-mesh"}},
		Scopes:    []string{"email", "groups"},
	}
	keycloak := &pmcorev1alpha1.OIDCInfo{
		IssuerURL: "https://portal.example/keycloak/realms/acme",
		Clients:   map[string]pmcorev1alpha1.ClientInfo{"kubectl": {ClientID: "generated"}},
	}
	accountInfo := func(oidc *pmcorev1alpha1.OIDCInfo) *pmcorev1alpha1.AccountInfo {
		return &pmcorev1alpha1.AccountInfo{
			ObjectMeta: metav1.ObjectMeta{Name: manageaccountinfo.DefaultAccountInfoName},
			Spec:       pmcorev1alpha1.AccountInfoSpec{OIDC: oidc},
		}
	}

	cases := map[string]struct {
		accountType pmcorev1alpha1.AccountType
		configured  *pmcorev1alpha1.OIDCInfo
		// parent is the AccountInfo next to the Account, existing is the one
		// already in the account's own workspace.
		parent   *pmcorev1alpha1.AccountInfo
		existing *pmcorev1alpha1.AccountInfo
		expected *pmcorev1alpha1.OIDCInfo
	}{
		"organization gets the configured issuer": {
			accountType: pmcorev1alpha1.AccountTypeOrg,
			configured:  configured,
			expected:    configured,
		},
		"organization without a configured issuer has none": {
			accountType: pmcorev1alpha1.AccountTypeOrg,
		},
		"organization without a configured issuer keeps what Keycloak wrote": {
			accountType: pmcorev1alpha1.AccountTypeOrg,
			existing:    accountInfo(keycloak),
			expected:    keycloak,
		},
		"configured issuer replaces what is there": {
			accountType: pmcorev1alpha1.AccountTypeOrg,
			configured:  configured,
			existing:    accountInfo(keycloak),
			expected:    configured,
		},
		"account gets the configured issuer, whatever the parent has": {
			accountType: pmcorev1alpha1.AccountTypeAccount,
			configured:  configured,
			parent:      accountInfo(nil),
			expected:    configured,
		},
		"account without a configured issuer inherits from the parent": {
			accountType: pmcorev1alpha1.AccountTypeAccount,
			parent:      accountInfo(keycloak),
			expected:    keycloak,
		},
	}

	for title, cas := range cases {
		t.Run(title, func(t *testing.T) {
			t.Parallel()

			scheme := runtime.NewScheme()
			require.NoError(t, pmcorev1alpha1.AddToScheme(scheme))
			require.NoError(t, kcptenancyv1alpha1.AddToScheme(scheme))

			parentObjects := []ctrlruntimeclient.Object{&kcptenancyv1alpha1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "acme"},
				Spec: kcptenancyv1alpha1.WorkspaceSpec{
					URL:     "https://kcp.example/clusters/root:orgs:acme",
					Cluster: accountCluster,
				},
				Status: kcptenancyv1alpha1.WorkspaceStatus{Phase: kcpcorev1alpha1.LogicalClusterPhaseReady},
			}}
			if cas.parent != nil {
				parentObjects = append(parentObjects, cas.parent.DeepCopy())
			}
			var accountObjects []ctrlruntimeclient.Object
			if cas.existing != nil {
				accountObjects = append(accountObjects, cas.existing.DeepCopy())
			}

			accountClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(accountObjects...).Build()
			clusterWith := func(cl ctrlruntimeclient.Client) cluster.Cluster {
				c := mocks.NewCluster(t)
				c.EXPECT().GetClient().Return(cl)
				return c
			}
			provider := &Provider{clusters: map[string]cluster.Cluster{
				parentCluster:  clusterWith(fake.NewClientBuilder().WithScheme(scheme).WithObjects(parentObjects...).Build()),
				accountCluster: clusterWith(accountClient),
			}}
			mgr, err := mcmanager.New(&rest.Config{}, provider, mcmanager.Options{})
			require.NoError(t, err)

			sub, err := manageaccountinfo.New(mgr, "", cas.configured)
			require.NoError(t, err)

			ctx := logger.SetLoggerInContext(t.Context(), testlogger.New().Logger)
			ctx = mccontext.WithCluster(ctx, parentCluster)

			_, err = sub.Process(ctx, &pmcorev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{Name: "acme"},
				Spec:       pmcorev1alpha1.AccountSpec{Type: cas.accountType},
			})
			require.NoError(t, err)

			var got pmcorev1alpha1.AccountInfo
			require.NoError(t, accountClient.Get(ctx, ctrlruntimeclient.ObjectKey{Name: manageaccountinfo.DefaultAccountInfoName}, &got))
			assert.Equal(t, cas.expected, got.Spec.OIDC)
		})
	}
}
