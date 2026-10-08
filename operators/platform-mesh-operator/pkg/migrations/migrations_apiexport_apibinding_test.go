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
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/platform-mesh-operator/pkg/subroutines/mocks"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kcptenancyv1alpha1 "github.com/kcp-dev/sdk/apis/tenancy/v1alpha1"
)

type MigrationTestSuite struct {
	suite.Suite
	helperMock *mocks.KcpHelper
	log        *logger.Logger
}

func TestMigrationTestSuite(t *testing.T) {
	suite.Run(t, new(MigrationTestSuite))
}

func (s *MigrationTestSuite) SetupTest() {
	s.helperMock = new(mocks.KcpHelper)
	cfg := logger.DefaultConfig()
	cfg.Level = "debug"
	cfg.NoJSON = true
	cfg.Name = "MigrationTestSuite"
	s.log, _ = logger.New(cfg)
}

func (s *MigrationTestSuite) TearDownTest() {
	s.helperMock = nil
}

func legacyCoreBinding() unstructured.Unstructured {
	b := unstructured.Unstructured{}
	b.SetGroupVersionKind(schema.GroupVersionKind{Group: "apis.kcp.io", Version: "v1alpha2", Kind: "APIBinding"})
	b.SetName("core.platform-mesh.io-abc12")
	_ = unstructured.SetNestedField(b.Object, "core.platform-mesh.io", "spec", "reference", "export", "name")
	_ = unstructured.SetNestedSlice(b.Object, []any{
		map[string]any{"resource": "accounts"},
		map[string]any{"resource": "stores"},
		map[string]any{"resource": "authorizationmodels"},
	}, "status", "boundResources")
	return b
}

func (s *MigrationTestSuite) Test_fgaAPIExportSplit_NoLegacyBinding() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	orgsClientMock := new(mocks.Client)
	s.helperMock.EXPECT().NewKcpClient(mock.Anything, "root:orgs").Return(orgsClientMock, nil)

	orgsClientMock.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
			list := ol.(*unstructured.UnstructuredList)
			// A binding to a different/already-trimmed export: nothing to migrate.
			b := unstructured.Unstructured{}
			b.SetName("fga.platform-mesh.io")
			_ = unstructured.SetNestedField(b.Object, "fga.platform-mesh.io", "spec", "reference", "export", "name")
			list.Items = []unstructured.Unstructured{b}
			return nil
		})

	err := fgaAPIExportSplit{}.Run(ctx, Deps{KcpHelper: s.helperMock, Config: &rest.Config{}})
	s.Assert().NoError(err)
	orgsClientMock.AssertNotCalled(s.T(), "Delete", mock.Anything, mock.Anything)
	orgsClientMock.AssertNotCalled(s.T(), "Patch", mock.Anything, mock.Anything, mock.Anything)
}

func (s *MigrationTestSuite) Test_fgaAPIExportSplit_WorkspaceNotFound() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	orgsClientMock := new(mocks.Client)
	s.helperMock.EXPECT().NewKcpClient(mock.Anything, "root:orgs").Return(orgsClientMock, nil)
	orgsClientMock.EXPECT().List(mock.Anything, mock.Anything).Return(errors.New("workspace not found"))

	err := fgaAPIExportSplit{}.Run(ctx, Deps{KcpHelper: s.helperMock, Config: &rest.Config{}})
	s.Assert().NoError(err)
}

func (s *MigrationTestSuite) Test_fgaAPIExportSplit_MigratesLegacyBinding() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	orgsClientMock := new(mocks.Client)
	s.helperMock.EXPECT().NewKcpClient(mock.Anything, "root:orgs").Return(orgsClientMock, nil)

	binding := legacyCoreBinding()

	orgsClientMock.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
			list := ol.(*unstructured.UnstructuredList)
			list.Items = []unstructured.Unstructured{binding}
			return nil
		}).Once()

	// deletionPolicy=WaitForSuccessor patch, then delete; fga.platform-mesh.io adopts
	// Store/AuthorizationModel server-side, so no object listing or finalizer stripping is needed.
	var patchedPolicy []byte
	orgsClientMock.EXPECT().Patch(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, obj ctrlruntimeclient.Object, patch ctrlruntimeclient.Patch, opts ...ctrlruntimeclient.PatchOption) error {
			patchedPolicy, _ = patch.Data(obj)
			return nil
		}).Once()
	orgsClientMock.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil).Once()
	orgsClientMock.EXPECT().Get(mock.Anything, types.NamespacedName{Name: binding.GetName()}, mock.Anything).
		Return(apierrors.NewNotFound(schema.GroupResource{Group: "apis.kcp.io", Resource: "apibindings"}, binding.GetName()))

	err := fgaAPIExportSplit{}.Run(ctx, Deps{KcpHelper: s.helperMock, Config: &rest.Config{}})
	s.Assert().NoError(err)
	s.Assert().JSONEq(`{"spec":{"deletionPolicy":"WaitForSuccessor"}}`, string(patchedPolicy))
}

func legacyProviderCoreBinding() unstructured.Unstructured {
	b := unstructured.Unstructured{}
	b.SetGroupVersionKind(schema.GroupVersionKind{Group: "apis.kcp.io", Version: "v1alpha2", Kind: "APIBinding"})
	b.SetName("core.platform-mesh.io-xyz99")
	_ = unstructured.SetNestedField(b.Object, "core.platform-mesh.io", "spec", "reference", "export", "name")
	_ = unstructured.SetNestedSlice(b.Object, []any{
		map[string]any{"resource": "accounts"},
		map[string]any{"resource": "contentconfigurations"},
		map[string]any{"resource": "providermetadatas"},
	}, "status", "boundResources")
	return b
}

func (s *MigrationTestSuite) Test_providerAPIExportSplit_NoWorkspaces() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	providersClientMock := new(mocks.Client)
	s.helperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers").Return(providersClientMock, nil)
	providersClientMock.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
			list := ol.(*kcptenancyv1alpha1.WorkspaceList)
			list.Items = nil
			return nil
		})

	err := providerAPIExportSplit{}.Run(ctx, Deps{KcpHelper: s.helperMock, Config: &rest.Config{}, Instance: &pmcorev1alpha1.PlatformMesh{}})
	s.Assert().NoError(err)
}

func (s *MigrationTestSuite) Test_providerAPIExportSplit_SkipsSystemWorkspace() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	providersClientMock := new(mocks.Client)
	s.helperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers").Return(providersClientMock, nil)
	providersClientMock.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
			list := ol.(*kcptenancyv1alpha1.WorkspaceList)
			list.Items = []kcptenancyv1alpha1.Workspace{{ObjectMeta: metav1.ObjectMeta{Name: "system"}}}
			return nil
		})

	err := providerAPIExportSplit{}.Run(ctx, Deps{KcpHelper: s.helperMock, Config: &rest.Config{}, Instance: &pmcorev1alpha1.PlatformMesh{}})
	s.Assert().NoError(err)
	s.helperMock.AssertNotCalled(s.T(), "NewKcpClient", mock.Anything, "root:providers:system")
}

func (s *MigrationTestSuite) Test_migrateLegacyProviderBinding_MigratesWithoutUIOptIn() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	providerClientMock := new(mocks.Client)
	s.helperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers:wildwest").Return(providerClientMock, nil)

	binding := legacyProviderCoreBinding()

	providerClientMock.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
			list := ol.(*unstructured.UnstructuredList)
			switch list.GroupVersionKind().Kind {
			case "APIBindingList":
				list.Items = []unstructured.Unstructured{binding}
			case "ContentConfigurationList":
				cc := unstructured.Unstructured{}
				cc.SetName("cowboys-ui")
				list.Items = []unstructured.Unstructured{cc}
			case "ProviderMetadataList":
				list.Items = []unstructured.Unstructured{}
			}
			return nil
		}).Times(3)

	// No finalizers on the ContentConfiguration, so no Patch call is expected for it.
	// No successor will ever exist, so this deletes outright: no deletionPolicy patch, no wait-for-gone.
	providerClientMock.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil).Once()

	// Only core.platform-mesh.io should be re-applied, no ui.platform-mesh.io since opt-in is off.
	providerClientMock.EXPECT().Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

	err := migrateLegacyProviderBinding(ctx, s.helperMock, &rest.Config{}, "wildwest", "", false)
	s.Assert().NoError(err)
	providerClientMock.AssertNotCalled(s.T(), "Patch", mock.Anything, mock.Anything, mock.Anything)
}

func (s *MigrationTestSuite) Test_migrateLegacyProviderBinding_MigratesWithUIOptIn() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	providerClientMock := new(mocks.Client)
	s.helperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers:wildwest").Return(providerClientMock, nil)

	binding := legacyProviderCoreBinding()

	providerClientMock.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
			list := ol.(*unstructured.UnstructuredList)
			list.Items = []unstructured.Unstructured{binding}
			return nil
		}).Once()

	// deletionPolicy=WaitForSuccessor patch, then delete; ui.platform-mesh.io adopts the
	// ContentConfiguration/ProviderMetadata instances server-side, so no capture/restore is needed.
	var patchedPolicy []byte
	providerClientMock.EXPECT().Patch(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, obj ctrlruntimeclient.Object, patch ctrlruntimeclient.Patch, opts ...ctrlruntimeclient.PatchOption) error {
			patchedPolicy, _ = patch.Data(obj)
			return nil
		}).Once()
	providerClientMock.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil).Once()
	providerClientMock.EXPECT().Get(mock.Anything, types.NamespacedName{Name: binding.GetName()}, mock.Anything).
		Return(apierrors.NewNotFound(schema.GroupResource{Group: "apis.kcp.io", Resource: "apibindings"}, binding.GetName()))

	// core.platform-mesh.io binding and ui.platform-mesh.io binding, nothing else.
	var appliedNames []string
	providerClientMock.EXPECT().Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, obj runtime.ApplyConfiguration, opts ...ctrlruntimeclient.ApplyOption) error {
			if named, ok := obj.(interface{ GetName() string }); ok {
				appliedNames = append(appliedNames, named.GetName())
			}
			return nil
		}).Twice()

	err := migrateLegacyProviderBinding(ctx, s.helperMock, &rest.Config{}, "wildwest", "root:platform-mesh-system", true)
	s.Assert().NoError(err)
	s.Assert().JSONEq(`{"spec":{"deletionPolicy":"WaitForSuccessor"}}`, string(patchedPolicy))
	s.Assert().ElementsMatch([]string{"core.platform-mesh.io", "ui.platform-mesh.io"}, appliedNames)
}
