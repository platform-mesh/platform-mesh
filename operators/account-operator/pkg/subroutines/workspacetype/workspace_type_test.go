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

package workspacetype_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"go.platform-mesh.io/account-operator/pkg/subroutines/mocks"
	"go.platform-mesh.io/account-operator/pkg/subroutines/workspacetype"
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

func TestName(t *testing.T) {
	s := workspacetype.New(nil)
	assert.Equal(t, workspacetype.SubroutineName, s.GetName())
}

func TestFinalizer(t *testing.T) {
	s := workspacetype.New(nil)
	assert.Equal(t, []string{workspacetype.SubroutineFinalizer}, s.Finalizers(&pmcorev1alpha1.Account{Spec: pmcorev1alpha1.AccountSpec{Type: pmcorev1alpha1.AccountTypeOrg}}))
}

func TestFinalize(t *testing.T) {
	testCases := []struct {
		name        string
		obj         *pmcorev1alpha1.Account
		k8sMocks    func(client *mocks.Client)
		expectError bool
	}{
		{
			name: "should delete both workspacetypes",
			obj: &pmcorev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: pmcorev1alpha1.AccountSpec{
					Type: pmcorev1alpha1.AccountTypeOrg,
				},
			},
			k8sMocks: func(client *mocks.Client) {
				client.EXPECT().
					Delete(mock.Anything, mock.Anything, mock.Anything).
					Return(nil).
					Twice()
			},
		},
		{
			name: "should ignore not found errors",
			obj: &pmcorev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: pmcorev1alpha1.AccountSpec{
					Type: pmcorev1alpha1.AccountTypeOrg,
				},
			},
			k8sMocks: func(client *mocks.Client) {
				client.EXPECT().
					Delete(mock.Anything, mock.Anything, mock.Anything).
					Return(apierrors.NewNotFound(schema.GroupResource{}, "not found")).
					Twice()
			},
		},
		{
			name: "should error out in case of other errors",
			obj: &pmcorev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: pmcorev1alpha1.AccountSpec{
					Type: pmcorev1alpha1.AccountTypeOrg,
				},
			},
			k8sMocks: func(client *mocks.Client) {
				client.EXPECT().
					Delete(mock.Anything, mock.Anything, mock.Anything).
					Return(errors.New("some error"))
			},
			expectError: true,
		},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			cl := mocks.NewClient(t)
			cluster := mocks.NewCluster(t)
			mgr := mocks.NewManager(t)
			if test.k8sMocks != nil {
				test.k8sMocks(cl)
			}
			mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("root:orgs")).Return(cluster, nil)
			cluster.EXPECT().GetClient().Return(cl)

			s := workspacetype.New(mgr)

			ctx := t.Context()

			_, finalizeErr := s.Finalize(ctx, test.obj)
			if test.expectError {
				assert.Error(t, finalizeErr)
			} else {
				assert.NoError(t, finalizeErr)
			}
		})
	}
}

func TestProcess(t *testing.T) {
	testCases := []struct {
		name        string
		obj         *pmcorev1alpha1.Account
		k8sMocks    func(client *mocks.Client)
		expectError bool
	}{
		{
			name: "",
			obj: &pmcorev1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: pmcorev1alpha1.AccountSpec{
					Type: pmcorev1alpha1.AccountTypeOrg,
				},
			},
			k8sMocks: func(client *mocks.Client) {
				client.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(apierrors.NewNotFound(schema.GroupResource{}, "not found")).
					Twice()

				client.EXPECT().
					Create(mock.Anything, mock.Anything, mock.Anything).
					Return(nil).
					Twice()
			},
		},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			cl := mocks.NewClient(t)
			cluster := mocks.NewCluster(t)
			mgr := mocks.NewManager(t)
			if test.k8sMocks != nil {
				test.k8sMocks(cl)
			}
			mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("root:orgs")).Return(cluster, nil).Twice()
			cluster.EXPECT().GetClient().Return(cl).Twice()

			s := workspacetype.New(mgr)

			_, processErr := s.Process(t.Context(), test.obj)
			if test.expectError {
				assert.Error(t, processErr)
			} else {
				assert.NoError(t, processErr)
			}
		})
	}
}

func TestProcess_PreservesAuthenticationConfigurations(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, kcptenancyv1alpha1.AddToScheme(scheme))

	existingAuthConfigs := []kcptenancyv1alpha1.AuthenticationConfigurationReference{
		{Name: "existing-auth-config"},
	}

	existingOrgWst := &kcptenancyv1alpha1.WorkspaceType{
		ObjectMeta: metav1.ObjectMeta{Name: "test-org"},
		Spec: kcptenancyv1alpha1.WorkspaceTypeSpec{
			AuthenticationConfigurations: existingAuthConfigs,
		},
	}
	existingAccWst := &kcptenancyv1alpha1.WorkspaceType{
		ObjectMeta: metav1.ObjectMeta{Name: "test-acc"},
		Spec: kcptenancyv1alpha1.WorkspaceTypeSpec{
			AuthenticationConfigurations: existingAuthConfigs,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(existingOrgWst, existingAccWst).
		Build()

	cluster := mocks.NewCluster(t)
	mgr := mocks.NewManager(t)
	mgr.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("root:orgs")).Return(cluster, nil).Twice()
	cluster.EXPECT().GetClient().Return(fakeClient).Twice()

	s := workspacetype.New(mgr)

	account := &pmcorev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec:       pmcorev1alpha1.AccountSpec{Type: pmcorev1alpha1.AccountTypeOrg},
	}

	_, err := s.Process(t.Context(), account)
	require.Nil(t, err)

	updatedOrgWst := &kcptenancyv1alpha1.WorkspaceType{}
	require.NoError(t, fakeClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "test-org"}, updatedOrgWst))
	assert.Equal(t, existingAuthConfigs, updatedOrgWst.Spec.AuthenticationConfigurations,
		"AuthenticationConfigurations should be preserved on org workspace type")

	updatedAccWst := &kcptenancyv1alpha1.WorkspaceType{}
	require.NoError(t, fakeClient.Get(t.Context(), ctrlruntimeclient.ObjectKey{Name: "test-acc"}, updatedAccWst))
	assert.Equal(t, existingAuthConfigs, updatedAccWst.Spec.AuthenticationConfigurations,
		"AuthenticationConfigurations should be preserved on account workspace type")
}
