package subroutine

import (
	"context"
	"testing"

	kcpv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	accountv1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/golang-commons/logger/testlogger"
	secopv1alpha1 "github.com/platform-mesh/security-operator/api/v1alpha1"
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

func TestNewInviteSubroutine(t *testing.T) {
	orgsClient := mocks.NewMockClient(t)
	mgr := mocks.NewMockManager(t)

	subroutine := NewInviteSubroutine(orgsClient, mgr)

	assert.NotNil(t, subroutine)
	assert.Equal(t, orgsClient, subroutine.orgsClient)
	assert.Equal(t, mgr, subroutine.mgr)
}

func TestInviteSubroutine_GetName(t *testing.T) {
	orgsClient := mocks.NewMockClient(t)
	mgr := mocks.NewMockManager(t)
	subroutine := NewInviteSubroutine(orgsClient, mgr)

	name := subroutine.GetName()
	assert.Equal(t, "InviteInitilizationSubroutine", name)
}

func TestInviteSubroutine_Finalizers(t *testing.T) {
	orgsClient := mocks.NewMockClient(t)
	mgr := mocks.NewMockManager(t)
	subroutine := NewInviteSubroutine(orgsClient, mgr)

	finalizers := subroutine.Finalizers(nil)
	assert.Nil(t, finalizers)
}

func TestInviteSubroutine_Finalize(t *testing.T) {
	orgsClient := mocks.NewMockClient(t)
	mgr := mocks.NewMockManager(t)
	subroutine := NewInviteSubroutine(orgsClient, mgr)

	ctx := context.Background()
	instance := &kcpv1alpha1.LogicalCluster{}

	result, opErr := subroutine.Finalize(ctx, instance)

	assert.Nil(t, opErr)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestInviteSubroutine_Process(t *testing.T) {
	tests := []struct {
		name           string
		setupMocks     func(*mocks.MockClient, *mocks.MockManager, *mocks.MockCluster)
		lc             *kcpv1alpha1.LogicalCluster
		expectedErr    bool
		expectedResult ctrl.Result
	}{
		{
			name: "Empty workspace name - early return",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster) {},
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
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster) {
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
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster) {
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
			name: "Account not of type organization - skip invite creation",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "test"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
						acc := obj.(*accountv1alpha1.Account)
						acc.Spec.Type = accountv1alpha1.AccountTypeAccount // Not organization type
						email := "user@test.io"
						acc.Spec.Creator = &email
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
			name: "Account Creator is nil",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "test"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
						acc := obj.(*accountv1alpha1.Account)
						acc.Spec.Type = accountv1alpha1.AccountTypeOrg
						acc.Spec.Creator = nil
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
			expectedErr:    true,
			expectedResult: ctrl.Result{},
		},
		{
			name: "CreateOrUpdate Ready",
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "acme"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
						acc := obj.(*accountv1alpha1.Account)
						acc.Spec.Type = accountv1alpha1.AccountTypeOrg
						email := "owner@acme.io"
						acc.Spec.Creator = &email
						return nil
					}).Once()
				cluster.EXPECT().GetClient().Return(orgsClient).Maybe()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "acme"}, mock.AnythingOfType("*v1alpha1.Invite")).
					Return(apierrors.NewNotFound(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "invites"}, "acme")).Once()
				orgsClient.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.Invite")).
					RunAndReturn(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
						inv := obj.(*secopv1alpha1.Invite)
						inv.Spec.Email = "owner@acme.io"
						inv.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}
						return nil
					}).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "acme"}, mock.AnythingOfType("*v1alpha1.Invite")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
						inv := obj.(*secopv1alpha1.Invite)
						inv.Spec.Email = "owner@acme.io"
						inv.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}
						return nil
					}).Maybe()
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
			setupMocks: func(orgsClient *mocks.MockClient, mgr *mocks.MockManager, cluster *mocks.MockCluster) {
				mgr.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "beta"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
						acc := obj.(*accountv1alpha1.Account)
						acc.Spec.Type = accountv1alpha1.AccountTypeOrg
						email := "owner@beta.io"
						acc.Spec.Creator = &email
						return nil
					}).Once()
				cluster.EXPECT().GetClient().Return(orgsClient).Maybe()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "beta"}, mock.AnythingOfType("*v1alpha1.Invite")).
					Return(apierrors.NewNotFound(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "invites"}, "beta")).Once()
				orgsClient.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.Invite")).
					RunAndReturn(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
						inv := obj.(*secopv1alpha1.Invite)
						inv.Spec.Email = "owner@beta.io"
						inv.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse}}
						return nil
					}).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "beta"}, mock.AnythingOfType("*v1alpha1.Invite")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
						inv := obj.(*secopv1alpha1.Invite)
						inv.Spec.Email = "owner@beta.io"
						inv.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse}}
						return nil
					}).Maybe()
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orgsClient := mocks.NewMockClient(t)
			mgr := mocks.NewMockManager(t)
			cluster := mocks.NewMockCluster(t)
			subroutine := NewInviteSubroutine(orgsClient, mgr)

			tt.setupMocks(orgsClient, mgr, cluster)

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

func TestGetWorkspaceName(t *testing.T) {
	tests := []struct {
		name     string
		lc       *kcpv1alpha1.LogicalCluster
		expected string
	}{
		{
			name: "valid workspace path",
			lc: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:test",
					},
				},
			},
			expected: "test",
		},
		{
			name: "workspace path with multiple segments",
			lc: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:test:sub",
					},
				},
			},
			expected: "sub",
		},
		{
			name: "missing annotation",
			lc: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			expected: "",
		},
		{
			name: "empty annotation",
			lc: &kcpv1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "",
					},
				},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getWorkspaceName(tt.lc)
			assert.Equal(t, tt.expected, result)
		})
	}
}
