package workspace_test

import (
	"context"
	"errors"
	"testing"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	conditionsapi "github.com/kcp-dev/kcp/sdk/apis/third_party/conditions/apis/conditions/v1alpha1"
	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
	"github.com/platform-mesh/account-operator/pkg/subroutines/workspace"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
)

func TestGetName(t *testing.T) {
	s := workspace.New(nil, nil)
	assert.Equal(t, workspace.WorkspaceSubroutineName, s.GetName())
}

func TestFinalizers(t *testing.T) {
	s := workspace.New(nil, nil)
	assert.Equal(t, []string{workspace.WorkspaceSubroutineFinalizer}, s.Finalizers(nil))
}

func TestFinalize(t *testing.T) {
	testCases := []struct {
		name          string
		obj           runtimeobject.RuntimeObject
		k8sMocks      func(m *mocks.Client)
		expectRequeue bool
	}{
		{
			name: "should finalize with workspace already deleted",
			obj: &corev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			k8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(kerrors.NewNotFound(schema.GroupResource{}, "test"))
			},
		},
		{
			name: "should requeue if deletion timestamp is set",
			obj: &corev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
			},
			k8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					RunAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						ws := obj.(*kcptenancyv1alpha.Workspace)
						ws.SetDeletionTimestamp(ptr.To(metav1.Now()))

						return nil
					})
			},
			expectRequeue: true,
		},
		{
			name: "should delete if no deletion timestamp is set",
			obj: &corev1alpha1.Account{
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

			s := workspace.New(mgr, nil)

			ctx := t.Context()

			ctx = mccontext.WithCluster(ctx, "test")

			result, err := s.Finalize(ctx, test.obj)
			assert.Nil(t, err)
			if test.expectRequeue {
				assert.Greater(t, result.RequeueAfter.Microseconds(), int64(0))
			}
		})
	}
}

func TestProcess(t *testing.T) {

	scheme := runtime.NewScheme()
	assert.NoError(t, corev1alpha1.AddToScheme(scheme))
	assert.NoError(t, kcptenancyv1alpha.AddToScheme(scheme))

	testCases := []struct {
		name          string
		obj           runtimeobject.RuntimeObject
		k8sMocks      func(m *mocks.Client)
		orgsK8sMocks  func(m *mocks.Client)
		expectRequeue bool
		expectError   bool
	}{
		{
			name: "shuold create workspace if not exists",
			obj: &corev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: corev1alpha1.AccountSpec{
					Type: corev1alpha1.AccountTypeOrg,
				},
			},
			orgsK8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					RunAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						wst := obj.(*kcptenancyv1alpha.WorkspaceType)

						wst.Status.Conditions = []conditionsapi.Condition{
							{
								Type:   conditionsapi.ReadyCondition,
								Status: v1.ConditionTrue,
							},
						}

						return nil
					})
			},
			k8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(kerrors.NewNotFound(schema.GroupResource{}, "test"))

				m.EXPECT().Scheme().Return(scheme)

				m.EXPECT().
					Create(mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			},
		},
		{
			name: "create workspace for account if not exists",
			obj: &corev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: corev1alpha1.AccountSpec{
					Type: corev1alpha1.AccountTypeAccount,
				},
			},
			orgsK8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					RunAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						wst := obj.(*kcptenancyv1alpha.WorkspaceType)

						wst.Status.Conditions = []conditionsapi.Condition{
							{
								Type:   conditionsapi.ReadyCondition,
								Status: v1.ConditionTrue,
							},
						}

						return nil
					})
			},
			k8sMocks: func(m *mocks.Client) {

				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					RunAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						ai := obj.(*corev1alpha1.AccountInfo)
						ai.Spec.Organization.Name = "org"

						return nil
					}).Once()

				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(kerrors.NewNotFound(schema.GroupResource{}, "test"))

				m.EXPECT().Scheme().Return(scheme)

				m.EXPECT().
					Create(mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			},
		},
		{
			name: "requeue if accountinfo not found",
			obj: &corev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: corev1alpha1.AccountSpec{
					Type: corev1alpha1.AccountTypeAccount,
				},
			},
			k8sMocks: func(m *mocks.Client) {
				m.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(kerrors.NewNotFound(schema.GroupResource{}, "test"))
			},
			expectRequeue: true,
		},
		{
			name: "should fail for any other error in accountinfo",
			obj: &corev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: corev1alpha1.AccountSpec{
					Type: corev1alpha1.AccountTypeAccount,
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
			orgsClient := mocks.NewClient(t)
			if test.orgsK8sMocks != nil {
				test.orgsK8sMocks(orgsClient)
			}

			mgr := mocks.NewManager(t)
			cluster := mocks.NewCluster(t)

			mgr.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil)

			client := mocks.NewClient(t)
			cluster.EXPECT().GetClient().Return(client)

			if test.k8sMocks != nil {
				test.k8sMocks(client)
			}

			s := workspace.New(mgr, orgsClient)

			ctx := t.Context()
			ctx = mccontext.WithCluster(ctx, "test")

			result, err := s.Process(ctx, test.obj)
			if test.expectError {
				assert.Error(t, err.Err())
			} else {
				assert.Nil(t, err)
			}
			if test.expectRequeue {
				assert.Greater(t, result.RequeueAfter.Microseconds(), int64(0))
			}
		})
	}
}
