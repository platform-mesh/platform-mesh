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
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"
	"go.platform-mesh.io/subroutines"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
)

func TestNewInviteSubroutine(t *testing.T) {
	mgr := mocks.NewMockManager(t)
	kcpHelper := mocks.NewMockKCPClientGetter(t)

	subroutine, err := NewInviteSubroutine(mgr, kcpHelper)
	require.NoError(t, err)
	assert.NotNil(t, subroutine)
	assert.Equal(t, kcpHelper, subroutine.kcpClientGetter)
	assert.Equal(t, mgr, subroutine.mgr)
}

func TestInviteSubroutine_GetName(t *testing.T) {
	mgr := mocks.NewMockManager(t)
	kcpHelper := mocks.NewMockKCPClientGetter(t)
	subroutine, err := NewInviteSubroutine(mgr, kcpHelper)
	require.NoError(t, err)

	name := subroutine.GetName()
	assert.Equal(t, "InviteInitializationSubroutine", name)
}

func TestInviteSubroutine_Initialize(t *testing.T) {
	tests := []struct {
		name                   string
		setupMocks             func(wsClient *mocks.MockClient, orgsClient *mocks.MockClient, mgr *mocks.MockManager, kcpHelper *mocks.MockKCPClientGetter)
		lc                     *kcpcorev1alpha1.LogicalCluster
		expectedErr            bool
		expectedResult         subroutines.Result
		expectedRequeueMessage string
	}{
		{
			name: "Empty workspace name - early return",
			setupMocks: func(_ *mocks.MockClient, _ *mocks.MockClient, _ *mocks.MockManager, _ *mocks.MockKCPClientGetter) {
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
			name: "kcp client getter error",
			setupMocks: func(_ *mocks.MockClient, _ *mocks.MockClient, _ *mocks.MockManager, kcpHelper *mocks.MockKCPClientGetter) {
				kcpHelper.EXPECT().NewClientFromContext(mock.Anything).Return(nil, assert.AnError).Once()
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
			setupMocks: func(wsClient *mocks.MockClient, orgsClient *mocks.MockClient, mgr *mocks.MockManager, kcpHelper *mocks.MockKCPClientGetter) {
				kcpHelper.EXPECT().NewClientFromContext(mock.Anything).Return(wsClient, nil).Once()
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
			name: "Account not of type organization - skip invite creation",
			setupMocks: func(wsClient *mocks.MockClient, orgsClient *mocks.MockClient, mgr *mocks.MockManager, kcpHelper *mocks.MockKCPClientGetter) {
				kcpHelper.EXPECT().NewClientFromContext(mock.Anything).Return(wsClient, nil).Once()
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, "root:orgs").Return(orgsClient, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "test"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						acc := obj.(*pmcorev1alpha1.Account)
						acc.Spec.Type = pmcorev1alpha1.AccountTypeAccount // Not organization type
						email := "user@test.io"
						acc.Spec.Creator = &email
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
			name: "CreateOrUpdate Ready",
			setupMocks: func(wsClient *mocks.MockClient, orgsClient *mocks.MockClient, mgr *mocks.MockManager, kcpHelper *mocks.MockKCPClientGetter) {
				kcpHelper.EXPECT().NewClientFromContext(mock.Anything).Return(wsClient, nil).Once()
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, "root:orgs").Return(orgsClient, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "acme"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						acc := obj.(*pmcorev1alpha1.Account)
						acc.Spec.Type = pmcorev1alpha1.AccountTypeOrg
						email := "owner@acme.io"
						acc.Spec.Creator = &email
						return nil
					}).Once()
				wsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "acme"}, mock.AnythingOfType("*v1alpha1.Invite")).
					Return(apierrors.NewNotFound(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "invites"}, "acme")).Once()
				wsClient.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.Invite")).
					RunAndReturn(func(_ context.Context, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.CreateOption) error {
						inv := obj.(*pmcorev1alpha1.Invite)
						inv.Spec.Email = "owner@acme.io"
						inv.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}
						return nil
					}).Once()
				wsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "acme"}, mock.AnythingOfType("*v1alpha1.Invite")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						inv := obj.(*pmcorev1alpha1.Invite)
						inv.Spec.Email = "owner@acme.io"
						inv.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}
						return nil
					}).Maybe()
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
			setupMocks: func(wsClient *mocks.MockClient, orgsClient *mocks.MockClient, mgr *mocks.MockManager, kcpHelper *mocks.MockKCPClientGetter) {
				kcpHelper.EXPECT().NewClientFromContext(mock.Anything).Return(wsClient, nil).Once()
				kcpHelper.EXPECT().NewClientForLogicalCluster(mock.Anything, "root:orgs").Return(orgsClient, nil).Once()
				orgsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "beta"}, mock.AnythingOfType("*v1alpha1.Account")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						acc := obj.(*pmcorev1alpha1.Account)
						acc.Spec.Type = pmcorev1alpha1.AccountTypeOrg
						email := "owner@beta.io"
						acc.Spec.Creator = &email
						return nil
					}).Once()
				wsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "beta"}, mock.AnythingOfType("*v1alpha1.Invite")).
					Return(apierrors.NewNotFound(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "invites"}, "beta")).Once()
				wsClient.EXPECT().Create(mock.Anything, mock.AnythingOfType("*v1alpha1.Invite")).
					RunAndReturn(func(_ context.Context, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.CreateOption) error {
						inv := obj.(*pmcorev1alpha1.Invite)
						inv.Spec.Email = "owner@beta.io"
						inv.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse}}
						return nil
					}).Once()
				wsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "beta"}, mock.AnythingOfType("*v1alpha1.Invite")).
					RunAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlruntimeclient.Object, _ ...ctrlruntimeclient.GetOption) error {
						inv := obj.(*pmcorev1alpha1.Invite)
						inv.Spec.Email = "owner@beta.io"
						inv.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse}}
						return nil
					}).Maybe()
			},
			lc: &kcpcorev1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kcp.io/path": "root:orgs:beta",
					},
				},
			},
			expectedErr:            false,
			expectedRequeueMessage: "invite resource is not ready yet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wsClient := mocks.NewMockClient(t)
			orgsClient := mocks.NewMockClient(t)
			mgr := mocks.NewMockManager(t)
			kcpHelper := mocks.NewMockKCPClientGetter(t)
			subroutine, err := NewInviteSubroutine(mgr, kcpHelper)
			require.NoError(t, err)

			tt.setupMocks(wsClient, orgsClient, mgr, kcpHelper)

			l := testlogger.New()
			ctx := l.WithContext(context.Background())

			result, err := subroutine.Initialize(ctx, tt.lc)

			if tt.expectedErr {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
			}
			if tt.expectedRequeueMessage != "" {
				assert.True(t, result.IsStopWithRequeue(),
					"expected StopWithRequeue result")
				assert.Contains(t, result.Message(), tt.expectedRequeueMessage)
				assert.Greater(t, result.Requeue(), time.Duration(0))
			} else {
				assert.Equal(t, tt.expectedResult, result)
			}
		})
	}
}

func TestGetWorkspaceName(t *testing.T) {
	tests := []struct {
		name     string
		lc       *kcpcorev1alpha1.LogicalCluster
		expected string
	}{
		{
			name: "valid workspace path",
			lc: &kcpcorev1alpha1.LogicalCluster{
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
			lc: &kcpcorev1alpha1.LogicalCluster{
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
			lc: &kcpcorev1alpha1.LogicalCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			expected: "",
		},
		{
			name: "empty annotation",
			lc: &kcpcorev1alpha1.LogicalCluster{
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
