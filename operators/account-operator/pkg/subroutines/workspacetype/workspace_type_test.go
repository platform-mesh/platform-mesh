package workspacetype_test

import (
	"errors"
	"testing"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
	"github.com/platform-mesh/account-operator/pkg/subroutines/workspacetype"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestName(t *testing.T) {
	s := workspacetype.New(nil)
	assert.Equal(t, workspacetype.WorkspaceTypeSubroutineName, s.GetName())
}

func TestFinalizer(t *testing.T) {
	s := workspacetype.New(nil)
	assert.Equal(t, []string{workspacetype.WorkspaceTypeSubroutineFinalizer}, s.Finalizers(&v1alpha1.Account{Spec: v1alpha1.AccountSpec{Type: v1alpha1.AccountTypeOrg}}))
}

func TestFinalize(t *testing.T) {
	testCases := []struct {
		name        string
		obj         runtimeobject.RuntimeObject
		k8sMocks    func(client *mocks.Client)
		expectError bool
	}{
		{
			name: "should delete both workspacetypes",
			obj: &v1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: v1alpha1.AccountSpec{
					Type: v1alpha1.AccountTypeOrg,
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
			obj: &v1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: v1alpha1.AccountSpec{
					Type: v1alpha1.AccountTypeOrg,
				},
			},
			k8sMocks: func(client *mocks.Client) {
				client.EXPECT().
					Delete(mock.Anything, mock.Anything, mock.Anything).
					Return(kerrors.NewNotFound(schema.GroupResource{}, "not found")).
					Twice()
			},
		},
		{
			name: "should error out in case of other errors",
			obj: &v1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: v1alpha1.AccountSpec{
					Type: v1alpha1.AccountTypeOrg,
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

			client := mocks.NewClient(t)
			if test.k8sMocks != nil {
				test.k8sMocks(client)
			}

			s := workspacetype.New(client)

			ctx := t.Context()

			_, err := s.Finalize(ctx, test.obj)
			if test.expectError {
				assert.Error(t, err.Err())
			} else {
				assert.Nil(t, err)
			}

		})
	}
}

func TestProcess(t *testing.T) {
	testCases := []struct {
		name        string
		obj         runtimeobject.RuntimeObject
		k8sMocks    func(client *mocks.Client)
		expectError bool
	}{
		{
			name: "",
			obj: &v1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: v1alpha1.AccountSpec{
					Type: v1alpha1.AccountTypeOrg,
				},
			},
			k8sMocks: func(client *mocks.Client) {
				client.EXPECT().
					Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(kerrors.NewNotFound(schema.GroupResource{}, "not found")).
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
			client := mocks.NewClient(t)
			if test.k8sMocks != nil {
				test.k8sMocks(client)
			}

			s := workspacetype.New(client)

			_, err := s.Process(t.Context(), test.obj)
			if test.expectError {
				assert.Error(t, err.Err())
			} else {
				assert.Nil(t, err)
			}
		})
	}
}
