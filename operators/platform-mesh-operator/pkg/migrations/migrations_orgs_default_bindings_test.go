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

package migrations

import (
	"context"
	"errors"

	"github.com/stretchr/testify/mock"

	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/platform-mesh-operator/pkg/subroutines/mocks"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func orgsBinding(name, export, path string) unstructured.Unstructured {
	b := unstructured.Unstructured{}
	b.SetGroupVersionKind(schema.GroupVersionKind{Group: "apis.kcp.io", Version: "v1alpha2", Kind: "APIBinding"})
	b.SetName(name)
	_ = unstructured.SetNestedField(b.Object, export, "spec", "reference", "export", "name")
	_ = unstructured.SetNestedField(b.Object, path, "spec", "reference", "export", "path")
	return b
}

// orgsClientWith returns a root:orgs client mock listing bindings.
func (s *MigrationTestSuite) orgsClientWith(bindings ...unstructured.Unstructured) *mocks.Client {
	orgsClientMock := new(mocks.Client)
	s.helperMock.EXPECT().NewKcpClient(mock.Anything, "root:orgs").Return(orgsClientMock, nil)
	orgsClientMock.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
			ol.(*unstructured.UnstructuredList).Items = bindings
			return nil
		}).Once()
	return orgsClientMock
}

func (s *MigrationTestSuite) Test_orgsDefaultBindings_RemovesDefaultBindings() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	orgsClientMock := s.orgsClientWith(
		orgsBinding("core.platform-mesh.io", corePlatformMeshIOExport, platformMeshSystemPath),
		orgsBinding("core.platform-mesh.io-2ri9i", corePlatformMeshIOExport, platformMeshSystemPath),
		orgsBinding("fga.platform-mesh.io", fgaPlatformMeshIOExport, platformMeshSystemPath),
		orgsBinding("fga.platform-mesh.io-bunj5", fgaPlatformMeshIOExport, platformMeshSystemPath),
		orgsBinding("system.platform-mesh.io-87gcj", "system.platform-mesh.io", platformMeshSystemPath),
		orgsBinding("core.platform-mesh.io-other", corePlatformMeshIOExport, "root:elsewhere"),
	)

	orgsClientMock.EXPECT().Patch(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(acceptDeletionPolicy).Twice()
	var deleted []string
	orgsClientMock.EXPECT().Delete(mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.DeleteOption) error {
			deleted = append(deleted, obj.GetName())
			return nil
		}).Twice()
	orgsClientMock.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, key ctrlruntimeclient.ObjectKey, obj ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
			return apierrors.NewNotFound(schema.GroupResource{Group: "apis.kcp.io", Resource: "apibindings"}, key.Name)
		})

	err := orgsDefaultBindings{}.Run(ctx, Deps{KcpHelper: s.helperMock, Config: &rest.Config{}})
	s.Assert().NoError(err)
	s.Assert().Equal([]string{"core.platform-mesh.io-2ri9i", "fga.platform-mesh.io-bunj5"}, deleted)
}

func (s *MigrationTestSuite) Test_orgsDefaultBindings_LeavesManifestBindingsAlone() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	orgsClientMock := s.orgsClientWith(
		orgsBinding("core.platform-mesh.io", corePlatformMeshIOExport, platformMeshSystemPath),
		orgsBinding("fga.platform-mesh.io", fgaPlatformMeshIOExport, platformMeshSystemPath),
	)

	err := orgsDefaultBindings{}.Run(ctx, Deps{KcpHelper: s.helperMock, Config: &rest.Config{}})
	s.Assert().NoError(err)
	orgsClientMock.AssertNotCalled(s.T(), "Patch", mock.Anything, mock.Anything, mock.Anything)
	orgsClientMock.AssertNotCalled(s.T(), "Delete", mock.Anything, mock.Anything)
}

// Without a successor kcp would hold the deletion forever, or delete the instances.
func (s *MigrationTestSuite) Test_orgsDefaultBindings_KeepsDefaultBindingWithoutSuccessor() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	deleting := orgsBinding("fga.platform-mesh.io", fgaPlatformMeshIOExport, platformMeshSystemPath)
	now := metav1.Now()
	deleting.SetDeletionTimestamp(&now)

	orgsClientMock := s.orgsClientWith(
		orgsBinding("core.platform-mesh.io-2ri9i", corePlatformMeshIOExport, platformMeshSystemPath),
		orgsBinding("core.platform-mesh.io", corePlatformMeshIOExport, "root:elsewhere"),
		deleting,
		orgsBinding("fga.platform-mesh.io-bunj5", fgaPlatformMeshIOExport, platformMeshSystemPath),
	)

	err := orgsDefaultBindings{}.Run(ctx, Deps{KcpHelper: s.helperMock, Config: &rest.Config{}})
	s.Assert().NoError(err)
	orgsClientMock.AssertNotCalled(s.T(), "Patch", mock.Anything, mock.Anything, mock.Anything)
	orgsClientMock.AssertNotCalled(s.T(), "Delete", mock.Anything, mock.Anything)
}

// A kcp that prunes spec.deletionPolicy deletes the instances along with the binding.
func (s *MigrationTestSuite) Test_orgsDefaultBindings_KeepsDefaultBindingWhenPolicyNotAccepted() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	orgsClientMock := s.orgsClientWith(
		orgsBinding("core.platform-mesh.io", corePlatformMeshIOExport, platformMeshSystemPath),
		orgsBinding("core.platform-mesh.io-2ri9i", corePlatformMeshIOExport, platformMeshSystemPath),
	)
	orgsClientMock.EXPECT().Patch(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

	err := orgsDefaultBindings{}.Run(ctx, Deps{KcpHelper: s.helperMock, Config: &rest.Config{}})
	s.Assert().ErrorContains(err, "core.platform-mesh.io-2ri9i")
	orgsClientMock.AssertNotCalled(s.T(), "Delete", mock.Anything, mock.Anything)
}

func (s *MigrationTestSuite) Test_orgsDefaultBindings_WorkspaceNotFound() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	orgsClientMock := new(mocks.Client)
	s.helperMock.EXPECT().NewKcpClient(mock.Anything, "root:orgs").Return(orgsClientMock, nil)
	orgsClientMock.EXPECT().List(mock.Anything, mock.Anything).Return(errors.New("workspace not found"))

	err := orgsDefaultBindings{}.Run(ctx, Deps{KcpHelper: s.helperMock, Config: &rest.Config{}})
	s.Assert().NoError(err)
}
