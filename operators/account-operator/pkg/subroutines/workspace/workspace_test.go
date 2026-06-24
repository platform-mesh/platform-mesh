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

package workspace_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/account-operator/pkg/subroutines/mocks"
	"go.platform-mesh.io/account-operator/pkg/subroutines/workspace"
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
	conditionsapi "github.com/kcp-dev/sdk/apis/third_party/conditions/apis/conditions/v1alpha1"
)

func TestGetName(t *testing.T) {
	s, err := workspace.New(nil)
	require.NoError(t, err)
	assert.Equal(t, workspace.WorkspaceSubroutineName, s.GetName())
}

func TestFinalizers(t *testing.T) {
	s, err := workspace.New(nil)
	require.NoError(t, err)
	assert.Equal(t, []string{workspace.WorkspaceSubroutineFinalizer}, s.Finalizers(nil))
}

func TestFinalize(t *testing.T) {
	testCases := []struct {
		name          string
		obj           *pmcorev1alpha1.Account
		k8sMocks      func(m *mocks.Client)
		expectRequeue bool
	}{
		{
			name: "should finalize with workspace already deleted",
			obj: &pmcorev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			k8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(apierrors.NewNotFound(schema.GroupResource{}, "test"))
			},
		},
		{
			name: "should requeue if deletion timestamp is set",
			obj: &pmcorev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			k8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					RunAndReturn(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						ws := obj.(*kcptenancyv1alpha1.Workspace)
						ws.SetDeletionTimestamp(ptr.To(metav1.Now()))

						return nil
					})
			},
			expectRequeue: true,
		},
		{
			name: "should delete if no deletion timestamp is set",
			obj: &pmcorev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			k8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(nil)

				m.EXPECT().
					Delete(mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			},
			expectRequeue: true,
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {

			mgr := mocks.NewManager(t)
			cluster := mocks.NewCluster(t)
			client := mocks.NewClient(t)

			mgr.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil)

			cluster.EXPECT().GetClient().Return(client)

			if test.k8sMocks != nil {
				test.k8sMocks(client)
			}

			s, err := workspace.New(mgr)
			require.NoError(t, err)

			ctx := t.Context()
			ctx = mccontext.WithCluster(ctx, "test")

			result, finalizeErr := s.Finalize(ctx, test.obj)
			assert.NoError(t, finalizeErr)
			if test.expectRequeue {
				assert.Greater(t, result.Requeue().Microseconds(), int64(0))
			}
		})
	}
}

func TestProcess(t *testing.T) {

	scheme := runtime.NewScheme()
	assert.NoError(t, pmcorev1alpha1.AddToScheme(scheme))
	assert.NoError(t, kcptenancyv1alpha1.AddToScheme(scheme))

	testCases := []struct {
		name          string
		obj           *pmcorev1alpha1.Account
		k8sMocks      func(m *mocks.Client)
		orgsK8sMocks  func(m *mocks.Client)
		expectRequeue bool
		expectError   bool
	}{
		{
			name: "shuold create workspace if not exists",
			obj: &pmcorev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: pmcorev1alpha1.AccountSpec{
					Type: pmcorev1alpha1.AccountTypeOrg,
				},
			},
			orgsK8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					RunAndReturn(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						wst := obj.(*kcptenancyv1alpha1.WorkspaceType)

						wst.Status.Conditions = []conditionsapi.Condition{
							{
								Type:   conditionsapi.ReadyCondition,
								Status: corev1.ConditionTrue,
							},
						}

						return nil
					})
			},
			k8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(apierrors.NewNotFound(schema.GroupResource{}, "test"))

				m.EXPECT().Scheme().Return(scheme)

				m.EXPECT().
					Create(mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			},
		},
		{
			name: "create workspace for account if not exists",
			obj: &pmcorev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: pmcorev1alpha1.AccountSpec{
					Type: pmcorev1alpha1.AccountTypeAccount,
				},
			},
			orgsK8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					RunAndReturn(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						wst := obj.(*kcptenancyv1alpha1.WorkspaceType)

						wst.Status.Conditions = []conditionsapi.Condition{
							{
								Type:   conditionsapi.ReadyCondition,
								Status: corev1.ConditionTrue,
							},
						}

						return nil
					})
			},
			k8sMocks: func(m *mocks.Client) {

				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					RunAndReturn(func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
						ai := obj.(*pmcorev1alpha1.AccountInfo)
						ai.Spec.Organization.Name = "org"

						return nil
					}).Once()
				m.EXPECT().
					Update(mock.Anything, mock.MatchedBy(func(o ctrlruntimeclient.Object) bool {
						_, ok := o.(*pmcorev1alpha1.AccountInfo)
						return ok
					})).
					Return(nil).
					Maybe()

				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(apierrors.NewNotFound(schema.GroupResource{}, "test"))

				m.EXPECT().Scheme().Return(scheme)

				m.EXPECT().
					Create(mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			},
		},
		{
			name: "requeue if accountinfo not found",
			obj: &pmcorev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: pmcorev1alpha1.AccountSpec{
					Type: pmcorev1alpha1.AccountTypeAccount,
				},
			},
			k8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(apierrors.NewNotFound(schema.GroupResource{}, "test"))
			},
			expectRequeue: true,
		},
		{
			name: "should fail for any other error in accountinfo",
			obj: &pmcorev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: pmcorev1alpha1.AccountSpec{
					Type: pmcorev1alpha1.AccountTypeAccount,
				},
			},
			k8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(errors.New("some-error"))
			},
			expectError: true,
		},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			mgr := mocks.NewManager(t)
			cluster := mocks.NewCluster(t)
			orgsCluster := mocks.NewCluster(t)
			orgsClient := mocks.NewClient(t)

			if test.orgsK8sMocks != nil {
				test.orgsK8sMocks(orgsClient)
			}

			mgr.EXPECT().
				GetCluster(mock.Anything, multicluster.ClusterName("root:orgs")).
				Return(orgsCluster, nil).
				Maybe()
			mgr.EXPECT().
				GetCluster(mock.Anything, mock.Anything).
				Return(cluster, nil)

			client := mocks.NewClient(t)
			cluster.EXPECT().GetClient().Return(client)
			orgsCluster.EXPECT().GetClient().Return(orgsClient).Maybe()

			if test.k8sMocks != nil {
				test.k8sMocks(client)
			}

			s, err := workspace.New(mgr)
			require.NoError(t, err)

			ctx := t.Context()
			ctx = mccontext.WithCluster(ctx, "test")

			result, processErr := s.Process(ctx, test.obj)
			if test.expectError {
				assert.Error(t, processErr)
			} else {
				assert.NoError(t, processErr)
			}
			if test.expectRequeue {
				assert.Greater(t, result.Requeue().Microseconds(), int64(0))
			}
		})
	}
}
