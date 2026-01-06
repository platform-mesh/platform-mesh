package subroutine

import (
	"context"
	"testing"

	kcpv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	accountv1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/golang-commons/logger/testlogger"
	secopv1alpha1 "github.com/platform-mesh/security-operator/api/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/config"
	"github.com/platform-mesh/security-operator/internal/subroutine/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestNewIDPSubroutine(t *testing.T) {
	orgsClient := mocks.NewMockClient(t)
	mgr := mocks.NewMockManager(t)
	cfg := config.Config{}
	cfg.IDP.AdditionalRedirectURLs = []string{"https://example.com/callback"}
	cfg.BaseDomain = "example.com"

	subroutine := NewIDPSubroutine(orgsClient, mgr, cfg)

	assert.NotNil(t, subroutine)
	assert.Equal(t, orgsClient, subroutine.orgsClient)
	assert.Equal(t, mgr, subroutine.mgr)
	assert.Equal(t, []string{"https://example.com/callback"}, subroutine.additionalRedirectURLs)
	assert.Equal(t, "example.com", subroutine.baseDomain)
}

func TestIDPSubroutine_GetName(t *testing.T) {
	orgsClient := mocks.NewMockClient(t)
	mgr := mocks.NewMockManager(t)
	cfg := config.Config{}
	cfg.BaseDomain = "example.com"
	subroutine := NewIDPSubroutine(orgsClient, mgr, cfg)

	name := subroutine.GetName()
	assert.Equal(t, "IDPSubroutine", name)
}

func TestIDPSubroutine_Finalizers(t *testing.T) {
	orgsClient := mocks.NewMockClient(t)
	mgr := mocks.NewMockManager(t)
	cfg := config.Config{}
	cfg.BaseDomain = "example.com"
	subroutine := NewIDPSubroutine(orgsClient, mgr, cfg)

	finalizers := subroutine.Finalizers(nil)
	assert.Nil(t, finalizers)
}

func TestIDPSubroutine_Finalize(t *testing.T) {
	orgsClient := mocks.NewMockClient(t)
	mgr := mocks.NewMockManager(t)
	cfg := config.Config{}
	cfg.BaseDomain = "example.com"
	subroutine := NewIDPSubroutine(orgsClient, mgr, cfg)

	ctx := context.Background()
	instance := &kcpv1alpha1.LogicalCluster{}

	result, opErr := subroutine.Finalize(ctx, instance)

	assert.Nil(t, opErr)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestIDPSubroutine_Process(t *testing.T) {
	tests := []struct {
		name           string
		setupMocks     func(*mocks.MockClient, *mocks.MockManager, *mocks.MockCluster, config.Config)
		lc             *kcpv1alpha1.LogicalCluster
		expectedErr    bool
		expectedResult ctrl.Result
	}{
		{
			name: "Empty workspace name - early return",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, cfg config.Config) {
			},
			lc: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			expectedErr:    true,
			expectedResult: ctrl.Result{},
		},
		{
			name: "ClusterFromContext error",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, cfg config.Config) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(nil, assert.AnError).Once()
			},
			lc: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:test",
					},
				},
			},
			expectedErr:    true,
			expectedResult: ctrl.Result{},
		},
		{
			name: "Account Get error",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, cfg config.Config) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "test"}, mock.AnythingOfType("*v1alpha1.Account")).
					Return(assert.AnError).Once()
			},
			lc: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:test",
					},
				},
			},
			expectedErr:    true,
			expectedResult: ctrl.Result{},
		},
		{
			name: "Account not of type organization - skip idp creation",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, cfg config.Config) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "test"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
						acc := obj.(*accountv1alpha1.Account)
						acc.Spec.Type = accountv1alpha1.AccountTypeAccount // Not organization type
						return nil
					}).Once()
			},
			lc: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:test",
					},
				},
			},
			expectedErr:    false,
			expectedResult: ctrl.Result{},
		},
		{
			name: "CreateOrUpdate and Ready",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, cfg config.Config) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "acme"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
						acc := obj.(*accountv1alpha1.Account)
						acc.Spec.Type = accountv1alpha1.AccountTypeOrg
						return nil
					}).Once()
				cluster.EXPECT().GetClient().Return(orgsClient).Maybe()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "acme"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					Return(apierrors.NewNotFound(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "identityproviderconfigurations"}, "acme")).Once()
				orgsClient.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					RunAndReturn(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
						idp := obj.(*secopv1alpha1.IdentityProviderConfiguration)
						assert.Len(t, idp.Spec.Clients, 1)
						assert.Equal(t, "acme", idp.Spec.Clients[0].ClientName)
						assert.Equal(t, "portal-client-secret-acme-acme", idp.Spec.Clients[0].SecretRef.Name)
						assert.Equal(t, "default", idp.Spec.Clients[0].SecretRef.Namespace)
						idp.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}
						return nil
					}).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "acme"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
						idp := obj.(*secopv1alpha1.IdentityProviderConfiguration)
						idp.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}
						return nil
					}).Once()
			},
			lc: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:acme",
					},
				},
			},
			expectedErr:    false,
			expectedResult: ctrl.Result{},
		},
		{
			name: "CreateOrUpdate NotReady",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, cfg config.Config) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "beta"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
						acc := obj.(*accountv1alpha1.Account)
						acc.Spec.Type = accountv1alpha1.AccountTypeOrg
						return nil
					}).Once()
				cluster.EXPECT().GetClient().Return(orgsClient).Maybe()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "beta"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					Return(apierrors.NewNotFound(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "identityproviderconfigurations"}, "beta")).Once()
				orgsClient.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					RunAndReturn(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
						idp := obj.(*secopv1alpha1.IdentityProviderConfiguration)
						assert.Len(t, idp.Spec.Clients, 1)
						assert.Equal(t, "beta", idp.Spec.Clients[0].ClientName)
						assert.Equal(t, "portal-client-secret-beta-beta", idp.Spec.Clients[0].SecretRef.Name)
						assert.Equal(t, "default", idp.Spec.Clients[0].SecretRef.Namespace)
						idp.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse}}
						return nil
					}).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "beta"}, mock.AnythingOfType("*v1alpha1.IdentityProviderConfiguration")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
						idp := obj.(*secopv1alpha1.IdentityProviderConfiguration)
						idp.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse}}
						return nil
					}).Once()
			},
			lc: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:beta",
					},
				},
			},
			expectedErr:    true,
			expectedResult: ctrl.Result{},
		},
		{
			name: "Get IDP resource error",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster, cfg config.Config) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "gamma"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
						acc := obj.(*accountv1alpha1.Account)
						acc.Spec.Type = accountv1alpha1.AccountTypeOrg
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
			lc: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:gamma",
					},
				},
			},
			expectedErr:    true,
			expectedResult: ctrl.Result{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orgsClient := mocks.NewMockClient(t)
			mgr := mocks.NewMockManager(t)
			cluster := mocks.NewMockCluster(t)
			cfg := config.Config{}
			cfg.IDP.AdditionalRedirectURLs = []string{}
			cfg.BaseDomain = "example.com"
			subroutine := NewIDPSubroutine(orgsClient, mgr, cfg)

			tt.setupMocks(orgsClient, mgr, cluster, cfg)

			l := testlogger.New()
			ctx := l.WithContext(context.Background())

			result, opErr := subroutine.Process(ctx, tt.lc)

			if tt.expectedErr {
				assert.NotNil(t, opErr)
			} else {
				assert.Nil(t, opErr)
			}
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}
