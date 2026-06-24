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

package subroutine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger/testlogger"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

func TestNewIDPSubroutine(t *testing.T) {
	mgr := mocks.NewMockManager(t)
	kcpHelper := mocks.NewMockKCPClientGetter(t)
	cfg := config.Config{}
	cfg.IDP.AdditionalRedirectURLs = []string{"https://example.com/callback"}
	cfg.BaseDomain = "example.com"

	subroutine, err := NewIDPSubroutine(mgr, kcpHelper, cfg)
	require.NoError(t, err)

	assert.NotNil(t, subroutine)
	assert.Equal(t, kcpHelper, subroutine.kcpClientGetter)
	assert.Equal(t, mgr, subroutine.mgr)
	assert.Equal(t, []string{"https://example.com/callback"}, subroutine.additionalRedirectURLs)
	assert.Equal(t, "example.com", subroutine.baseDomain)
}

func TestIDPSubroutine_GetName(t *testing.T) {
	mgr := mocks.NewMockManager(t)
	kcpHelper := mocks.NewMockKCPClientGetter(t)
	cfg := config.Config{}
	cfg.BaseDomain = "example.com"
	subroutine, err := NewIDPSubroutine(mgr, kcpHelper, cfg)
	require.NoError(t, err)

	name := subroutine.GetName()
	assert.Equal(t, "IDPSubroutine", name)
}

func TestIDPSubroutine_Initialize(t *testing.T) {
	tests := []struct {
		name           string
		setupMocks     func(*mocks.MockClient, *mocks.MockManager, *mocks.MockCluster, *mocks.MockKCPClientGetter, config.Config)
		lc             *kcpcorev1alpha1.LogicalCluster
		expectedErr    bool
		expectedResult subroutines.Result
	}{
		{
			name: "Empty workspace name - early return",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, kcpHelper *mocks.MockKCPClientGetter, cfg config.Config) {
			},
			lc: &kcpcorev1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			expectedErr:    true,
			expectedResult: subroutines.OK(),
		},
		{
			name: "ClusterFromContext error",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, kcpHelper *mocks.MockKCPClientGetter, cfg config.Config) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(nil, assert.AnError).Once()
			},
			lc: &kcpcorev1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:test",
					},
				},
			},
			expectedErr:    true,
			expectedResult: subroutines.OK(),
		},
		{
			name: "Account Get error",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, kcpHelper *mocks.MockKCPClientGetter, cfg config.Config) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, "root:orgs").Return(orgsClient, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "test"}, mock.AnythingOfType("*v1alpha1.Account")).
					Return(assert.AnError).Once()
			},
			lc: &kcpcorev1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:test",
					},
				},
			},
			expectedErr:    true,
			expectedResult: subroutines.OK(),
		},
		{
			name: "Account not of type organization - skip idp creation",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, kcpHelper *mocks.MockKCPClientGetter, cfg config.Config) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, "root:orgs").Return(orgsClient, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "test"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						acc := obj.(*pmcorev1alpha1.Account)
						acc.Spec.Type = pmcorev1alpha1.AccountTypeAccount // Not organization type
						return nil
					}).Once()
			},
			lc: &kcpcorev1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:test",
					},
				},
			},
			expectedErr:    false,
			expectedResult: subroutines.OK(),
		},
		{
			name: "CreateOrUpdate and Ready",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, kcpHelper *mocks.MockKCPClientGetter, cfg config.Config) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, "root:orgs").Return(orgsClient, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "acme"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						acc := obj.(*pmcorev1alpha1.Account)
						acc.Spec.Type = pmcorev1alpha1.AccountTypeOrg
						return nil
					}).Once()
				cluster.EXPECT().GetClient().Return(orgsClient).Maybe()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "acme"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					Return(apierrors.NewNotFound(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "identityproviderconfigurations"}, "acme")).Once()
				orgsClient.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					RunAndReturn(func(_ context.Context, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.CreateOption) error {
						idp := obj.(*pmcorev1alpha1.IdentityProviderConfiguration)
						assert.Len(t, idp.Spec.Clients, 2)
						assert.Equal(t, "acme", idp.Spec.Clients[0].ClientName)
						assert.Equal(t, "portal-client-secret-acme-acme", idp.Spec.Clients[0].SecretRef.Name)
						assert.Equal(t, "default", idp.Spec.Clients[0].SecretRef.Namespace)
						assert.Equal(t, "kubectl", idp.Spec.Clients[1].ClientName)
						assert.Equal(t, pmcorev1alpha1.IdentityProviderClientTypePublic, idp.Spec.Clients[1].ClientType)
						assert.Equal(t, []string{"http://localhost:8000", "http://localhost:18000"}, idp.Spec.Clients[1].RedirectURIs)
						idp.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}
						return nil
					}).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "acme"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						idp := obj.(*pmcorev1alpha1.IdentityProviderConfiguration)
						idp.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}
						idp.Status.ManagedClients = map[string]pmcorev1alpha1.ManagedClient{
							"acme":    {ClientID: "acme-client-id"},
							"kubectl": {ClientID: "kubectl-client-id"},
						}
						return nil
					}).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						accountInfo := obj.(*pmcorev1alpha1.AccountInfo)
						accountInfo.Spec.OIDC = &pmcorev1alpha1.OIDCInfo{
							IssuerURL: "https://old.example.com/keycloak/realms/acme",
							Clients: map[string]pmcorev1alpha1.ClientInfo{
								"old-client": {ClientID: "old-client-id"},
							},
						}
						return nil
					}).Once()
				orgsClient.EXPECT().Patch(mock.Anything, mock.AnythingOfType("*v1alpha1.AccountInfo"), mock.Anything).
					Return(nil).Once()
			},
			lc: &kcpcorev1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:acme",
					},
				},
			},
			expectedErr:    false,
			expectedResult: subroutines.OK(),
		},
		{
			name: "CreateOrUpdate NotReady",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, kcpHelper *mocks.MockKCPClientGetter, cfg config.Config) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, "root:orgs").Return(orgsClient, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "beta"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						acc := obj.(*pmcorev1alpha1.Account)
						acc.Spec.Type = pmcorev1alpha1.AccountTypeOrg
						return nil
					}).Once()
				cluster.EXPECT().GetClient().Return(orgsClient).Maybe()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "beta"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					Return(apierrors.NewNotFound(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "identityproviderconfigurations"}, "beta")).Once()
				orgsClient.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					RunAndReturn(func(_ context.Context, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.CreateOption) error {
						idp := obj.(*pmcorev1alpha1.IdentityProviderConfiguration)
						assert.Len(t, idp.Spec.Clients, 2)
						assert.Equal(t, "beta", idp.Spec.Clients[0].ClientName)
						assert.Equal(t, "portal-client-secret-beta-beta", idp.Spec.Clients[0].SecretRef.Name)
						assert.Equal(t, "default", idp.Spec.Clients[0].SecretRef.Namespace)
						assert.Equal(t, "kubectl", idp.Spec.Clients[1].ClientName)
						assert.Equal(t, pmcorev1alpha1.IdentityProviderClientTypePublic, idp.Spec.Clients[1].ClientType)
						assert.Equal(t, []string{"http://localhost:8000", "http://localhost:18000"}, idp.Spec.Clients[1].RedirectURIs)
						idp.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse}}
						return nil
					}).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "beta"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						idp := obj.(*pmcorev1alpha1.IdentityProviderConfiguration)
						idp.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse}}
						return nil
					}).Once()
			},
			lc: &kcpcorev1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:beta",
					},
				},
			},
			expectedErr:    false,
			expectedResult: subroutines.StopWithRequeue(2*time.Second, "idp resource is not ready yet"),
		},
		{
			name: "CreateOrPatch create error",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, kcpHelper *mocks.MockKCPClientGetter, cfg config.Config) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, "root:orgs").Return(orgsClient, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "delta"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						obj.(*pmcorev1alpha1.Account).Spec.Type = pmcorev1alpha1.AccountTypeOrg
						return nil
					}).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "delta"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					Return(apierrors.NewNotFound(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "identityproviderconfigurations"}, "delta")).Once()
				orgsClient.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					Return(assert.AnError).Once()
			},
			lc: &kcpcorev1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"kcp.io/path": "root:orgs:delta"}},
			},
			expectedErr:    true,
			expectedResult: subroutines.OK(),
		},
		{
			name: "IDP ready but no managed clients",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, kcpHelper *mocks.MockKCPClientGetter, cfg config.Config) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, "root:orgs").Return(orgsClient, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "epsilon"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						obj.(*pmcorev1alpha1.Account).Spec.Type = pmcorev1alpha1.AccountTypeOrg
						return nil
					}).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "epsilon"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					Return(apierrors.NewNotFound(schema.GroupResource{}, "epsilon")).Once()
				orgsClient.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).Return(nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "epsilon"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						idp := obj.(*pmcorev1alpha1.IdentityProviderConfiguration)
						idp.Spec.Clients = []pmcorev1alpha1.IdentityProviderClientConfig{{ClientName: "epsilon"}, {ClientName: kubectlClientName}}
						idp.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}
						// ManagedClients intentionally empty
						return nil
					}).Once()
			},
			lc: &kcpcorev1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"kcp.io/path": "root:orgs:epsilon"}},
			},
			expectedErr:    true,
			expectedResult: subroutines.OK(),
		},
		{
			name: "managed client not found in status",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, kcpHelper *mocks.MockKCPClientGetter, cfg config.Config) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, "root:orgs").Return(orgsClient, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "zeta"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						obj.(*pmcorev1alpha1.Account).Spec.Type = pmcorev1alpha1.AccountTypeOrg
						return nil
					}).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "zeta"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					Return(apierrors.NewNotFound(schema.GroupResource{}, "zeta")).Once()
				orgsClient.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).Return(nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "zeta"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						idp := obj.(*pmcorev1alpha1.IdentityProviderConfiguration)
						idp.Spec.Clients = []pmcorev1alpha1.IdentityProviderClientConfig{{ClientName: "zeta"}, {ClientName: kubectlClientName}}
						idp.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}
						// Only "zeta" in managed clients — "kubectl" is missing
						idp.Status.ManagedClients = map[string]pmcorev1alpha1.ManagedClient{"zeta": {ClientID: "zeta-id"}}
						return nil
					}).Once()
			},
			lc: &kcpcorev1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"kcp.io/path": "root:orgs:zeta"}},
			},
			expectedErr:    true,
			expectedResult: subroutines.OK(),
		},
		{
			name: "managed client has empty ClientID",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, kcpHelper *mocks.MockKCPClientGetter, cfg config.Config) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, "root:orgs").Return(orgsClient, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "eta"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						obj.(*pmcorev1alpha1.Account).Spec.Type = pmcorev1alpha1.AccountTypeOrg
						return nil
					}).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "eta"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					Return(apierrors.NewNotFound(schema.GroupResource{}, "eta")).Once()
				orgsClient.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).Return(nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "eta"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						idp := obj.(*pmcorev1alpha1.IdentityProviderConfiguration)
						idp.Spec.Clients = []pmcorev1alpha1.IdentityProviderClientConfig{{ClientName: "eta"}, {ClientName: kubectlClientName}}
						idp.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}
						idp.Status.ManagedClients = map[string]pmcorev1alpha1.ManagedClient{
							"eta":             {ClientID: ""},
							kubectlClientName: {ClientID: "kubectl-id"},
						}
						return nil
					}).Once()
			},
			lc: &kcpcorev1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"kcp.io/path": "root:orgs:eta"}},
			},
			expectedErr:    true,
			expectedResult: subroutines.OK(),
		},
		{
			name: "patchAccountInfo Get error",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, kcpHelper *mocks.MockKCPClientGetter, cfg config.Config) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, "root:orgs").Return(orgsClient, nil).Once()
				cluster.EXPECT().GetClient().Return(orgsClient).Maybe()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "theta"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						obj.(*pmcorev1alpha1.Account).Spec.Type = pmcorev1alpha1.AccountTypeOrg
						return nil
					}).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "theta"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					Return(apierrors.NewNotFound(schema.GroupResource{}, "theta")).Once()
				orgsClient.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).Return(nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "theta"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						idp := obj.(*pmcorev1alpha1.IdentityProviderConfiguration)
						idp.Spec.Clients = []pmcorev1alpha1.IdentityProviderClientConfig{{ClientName: "theta"}, {ClientName: kubectlClientName}}
						idp.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}
						idp.Status.ManagedClients = map[string]pmcorev1alpha1.ManagedClient{
							"theta":           {ClientID: "theta-id"},
							kubectlClientName: {ClientID: "kubectl-id"},
						}
						return nil
					}).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo")).
					Return(assert.AnError).Once()
			},
			lc: &kcpcorev1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"kcp.io/path": "root:orgs:theta"}},
			},
			expectedErr:    true,
			expectedResult: subroutines.OK(),
		},
		{
			name: "accountInfo already up to date - no patch",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, kcpHelper *mocks.MockKCPClientGetter, cfg config.Config) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, "root:orgs").Return(orgsClient, nil).Once()
				cluster.EXPECT().GetClient().Return(orgsClient).Maybe()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "iota"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						obj.(*pmcorev1alpha1.Account).Spec.Type = pmcorev1alpha1.AccountTypeOrg
						return nil
					}).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "iota"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					Return(apierrors.NewNotFound(schema.GroupResource{}, "iota")).Once()
				orgsClient.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).Return(nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "iota"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						idp := obj.(*pmcorev1alpha1.IdentityProviderConfiguration)
						idp.Spec.Clients = []pmcorev1alpha1.IdentityProviderClientConfig{{ClientName: "iota"}, {ClientName: kubectlClientName}}
						idp.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}
						idp.Status.ManagedClients = map[string]pmcorev1alpha1.ManagedClient{
							"iota":            {ClientID: "iota-id"},
							kubectlClientName: {ClientID: "kubectl-id"},
						}
						return nil
					}).Once()
				// AccountInfo already matches the desired state — no Patch expected
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						obj.(*pmcorev1alpha1.AccountInfo).Spec.OIDC = &pmcorev1alpha1.OIDCInfo{
							IssuerURL: "https://example.com/keycloak/realms/iota",
							Clients: map[string]pmcorev1alpha1.ClientInfo{
								"iota":            {ClientID: "iota-id"},
								kubectlClientName: {ClientID: "kubectl-id"},
							},
						}
						return nil
					}).Once()
			},
			lc: &kcpcorev1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"kcp.io/path": "root:orgs:iota"}},
			},
			expectedErr:    false,
			expectedResult: subroutines.OK(),
		},
		{
			name: "patchAccountInfo Patch error",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, kcpHelper *mocks.MockKCPClientGetter, cfg config.Config) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, "root:orgs").Return(orgsClient, nil).Once()
				cluster.EXPECT().GetClient().Return(orgsClient).Maybe()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "kappa"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						obj.(*pmcorev1alpha1.Account).Spec.Type = pmcorev1alpha1.AccountTypeOrg
						return nil
					}).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "kappa"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					Return(apierrors.NewNotFound(schema.GroupResource{}, "kappa")).Once()
				orgsClient.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).Return(nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "kappa"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						idp := obj.(*pmcorev1alpha1.IdentityProviderConfiguration)
						idp.Spec.Clients = []pmcorev1alpha1.IdentityProviderClientConfig{{ClientName: "kappa"}, {ClientName: kubectlClientName}}
						idp.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}
						idp.Status.ManagedClients = map[string]pmcorev1alpha1.ManagedClient{
							"kappa":           {ClientID: "kappa-id"},
							kubectlClientName: {ClientID: "kubectl-id"},
						}
						return nil
					}).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo")).
					Return(nil).Once()
				orgsClient.EXPECT().Patch(mock.Anything, mock.AnythingOfType("*v1alpha1.AccountInfo"), mock.Anything).
					Return(assert.AnError).Once()
			},
			lc: &kcpcorev1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"kcp.io/path": "root:orgs:kappa"}},
			},
			expectedErr:    true,
			expectedResult: subroutines.OK(),
		},
		{
			name: "ensureClient updates existing client in IDP spec",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, kcpHelper *mocks.MockKCPClientGetter, cfg config.Config) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, "root:orgs").Return(orgsClient, nil).Once()
				cluster.EXPECT().GetClient().Return(orgsClient).Maybe()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "lambda"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						obj.(*pmcorev1alpha1.Account).Spec.Type = pmcorev1alpha1.AccountTypeOrg
						return nil
					}).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "lambda"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						idp := obj.(*pmcorev1alpha1.IdentityProviderConfiguration)
						idp.Spec.Clients = []pmcorev1alpha1.IdentityProviderClientConfig{
							{ClientName: "lambda", RedirectURIs: []string{"https://old.example.com/*"}},
						}
						return nil
					}).Once()
				orgsClient.EXPECT().Patch(mock.Anything, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration"), mock.Anything).
					Return(nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "lambda"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						idp := obj.(*pmcorev1alpha1.IdentityProviderConfiguration)
						idp.Spec.Clients = []pmcorev1alpha1.IdentityProviderClientConfig{{ClientName: "lambda"}, {ClientName: kubectlClientName}}
						idp.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}
						idp.Status.ManagedClients = map[string]pmcorev1alpha1.ManagedClient{
							"lambda":          {ClientID: "lambda-id"},
							kubectlClientName: {ClientID: "kubectl-id"},
						}
						return nil
					}).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.AnythingOfType("*v1alpha1.AccountInfo")).
					Return(nil).Once()
				orgsClient.EXPECT().Patch(mock.Anything, mock.AnythingOfType("*v1alpha1.AccountInfo"), mock.Anything).
					Return(nil).Once()
			},
			lc: &kcpcorev1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"kcp.io/path": "root:orgs:lambda"}},
			},
			expectedErr:    false,
			expectedResult: subroutines.OK(),
		},
		{
			name: "Get IDP resource error",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, kcpHelper *mocks.MockKCPClientGetter, cfg config.Config) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, "root:orgs").Return(orgsClient, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "gamma"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						acc := obj.(*pmcorev1alpha1.Account)
						acc.Spec.Type = pmcorev1alpha1.AccountTypeOrg
						return nil
					}).Once()
				cluster.EXPECT().GetClient().Return(orgsClient).Maybe()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "gamma"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					Return(apierrors.NewNotFound(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "identityproviderconfigurations"}, "gamma")).Once()
				orgsClient.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					Return(nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "gamma"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					Return(assert.AnError).Once()
			},
			lc: &kcpcorev1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:gamma",
					},
				},
			},
			expectedErr:    true,
			expectedResult: subroutines.OK(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orgsClient := mocks.NewMockClient(t)
			mgr := mocks.NewMockManager(t)
			cluster := mocks.NewMockCluster(t)
			kcpHelper := mocks.NewMockKCPClientGetter(t)
			cfg := config.Config{}
			cfg.IDP.AdditionalRedirectURLs = []string{}
			cfg.IDP.KubectlClientRedirectURLs = []string{"http://localhost:8000", "http://localhost:18000"}
			cfg.BaseDomain = "example.com"
			subroutine, err := NewIDPSubroutine(mgr, kcpHelper, cfg)
			require.NoError(t, err)

			tt.setupMocks(orgsClient, mgr, cluster, kcpHelper, cfg)

			l := testlogger.New()
			ctx := l.WithContext(context.Background())

			result, err := subroutine.Initialize(ctx, tt.lc)

			if tt.expectedErr {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
			}
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestIDPSubroutine_Process(t *testing.T) {
	mgr := mocks.NewMockManager(t)
	kcpHelper := mocks.NewMockKCPClientGetter(t)
	cfg := config.Config{}
	cfg.IDP.KubectlClientRedirectURLs = []string{"http://localhost:8000"}
	cfg.BaseDomain = "example.com"

	sub, err := NewIDPSubroutine(mgr, kcpHelper, cfg)
	require.NoError(t, err)

	mgr.EXPECT().ClusterFromContext(mock.Anything).Return(nil, assert.AnError).Once()

	lc := &kcpcorev1alpha1.LogicalCluster{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"kcp.io/path": "root:orgs:test"}},
	}

	result, err := sub.Process(context.Background(), lc)
	assert.Error(t, err)
	assert.Equal(t, subroutines.OK(), result)
}
