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

	kcpcorev1alpha1 "github.com/kcp-dev/sdk/apis/core/v1alpha1"
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

// Locks in the race where the migration applied provider-fga.platform-mesh.io into a workspace
// the system:apibindings initializer was still populating, colliding with its default binding.
func (s *MigrationTestSuite) Test_providerAPIExportSplit_SkipsInitializingWorkspace() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	providersClientMock := new(mocks.Client)
	s.helperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers").Return(providersClientMock, nil)
	providersClientMock.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
			list := ol.(*kcptenancyv1alpha1.WorkspaceList)
			list.Items = []kcptenancyv1alpha1.Workspace{{
				ObjectMeta: metav1.ObjectMeta{Name: "wildwest"},
				Status:     kcptenancyv1alpha1.WorkspaceStatus{Phase: kcpcorev1alpha1.LogicalClusterPhaseInitializing},
			}}
			return nil
		})

	err := providerAPIExportSplit{}.Run(ctx, Deps{KcpHelper: s.helperMock, Config: &rest.Config{}, Instance: &pmcorev1alpha1.PlatformMesh{}})
	s.Assert().NoError(err)
	s.helperMock.AssertNotCalled(s.T(), "NewKcpClient", mock.Anything, "root:providers:wildwest")
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

	// provider-fga.platform-mesh.io and core.platform-mesh.io, no ui.platform-mesh.io since opt-in is off.
	providerClientMock.EXPECT().Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Twice()

	err := migrateLegacyProviderBinding(ctx, s.helperMock, &rest.Config{}, "wildwest", "", false, true)
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

	// provider-fga.platform-mesh.io, core.platform-mesh.io and ui.platform-mesh.io bindings, nothing else.
	var appliedNames []string
	providerClientMock.EXPECT().Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, obj runtime.ApplyConfiguration, opts ...ctrlruntimeclient.ApplyOption) error {
			if named, ok := obj.(interface{ GetName() string }); ok {
				appliedNames = append(appliedNames, named.GetName())
			}
			return nil
		}).Times(3)

	err := migrateLegacyProviderBinding(ctx, s.helperMock, &rest.Config{}, "wildwest", "root:platform-mesh-system", true, true)
	s.Assert().NoError(err)
	s.Assert().JSONEq(`{"spec":{"deletionPolicy":"WaitForSuccessor"}}`, string(patchedPolicy))
	s.Assert().ElementsMatch([]string{"provider-fga.platform-mesh.io", "core.platform-mesh.io", "ui.platform-mesh.io"}, appliedNames)
}

func providerBindingTo(export string) unstructured.Unstructured {
	b := unstructured.Unstructured{}
	b.SetGroupVersionKind(schema.GroupVersionKind{Group: "apis.kcp.io", Version: "v1alpha2", Kind: "APIBinding"})
	b.SetName(export)
	_ = unstructured.SetNestedField(b.Object, export, "spec", "reference", "export", "name")
	return b
}

// providerUIBinding is a ui.platform-mesh.io binding that already accepts the apiexports claim.
func providerUIBinding() unstructured.Unstructured {
	b := providerBindingTo("ui.platform-mesh.io")
	_ = unstructured.SetNestedField(b.Object, "root:platform-mesh-system", "spec", "reference", "export", "path")
	_ = unstructured.SetNestedSlice(b.Object, uiBindingClaims(), "spec", "permissionClaims")
	return b
}

func (s *MigrationTestSuite) Test_migrateLegacyProviderBinding_AddsProviderFgaToMigratedProvider() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	providerClientMock := new(mocks.Client)
	s.helperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers:wildwest").Return(providerClientMock, nil)

	providerClientMock.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
			list := ol.(*unstructured.UnstructuredList)
			list.Items = []unstructured.Unstructured{providerBindingTo("core.platform-mesh.io"), providerUIBinding()}
			return nil
		}).Once()

	var appliedNames []string
	providerClientMock.EXPECT().Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, obj runtime.ApplyConfiguration, opts ...ctrlruntimeclient.ApplyOption) error {
			if named, ok := obj.(interface{ GetName() string }); ok {
				appliedNames = append(appliedNames, named.GetName())
			}
			return nil
		}).Once()

	err := migrateLegacyProviderBinding(ctx, s.helperMock, &rest.Config{}, "wildwest", "root:platform-mesh-system", true, true)
	s.Assert().NoError(err)
	s.Assert().Equal([]string{"provider-fga.platform-mesh.io"}, appliedNames)
	providerClientMock.AssertNotCalled(s.T(), "Delete", mock.Anything, mock.Anything)
}

func (s *MigrationTestSuite) Test_migrateLegacyProviderBinding_LeavesFullyMigratedProviderAlone() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	providerClientMock := new(mocks.Client)
	s.helperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers:wildwest").Return(providerClientMock, nil)

	providerClientMock.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
			list := ol.(*unstructured.UnstructuredList)
			list.Items = []unstructured.Unstructured{
				providerBindingTo("core.platform-mesh.io"), providerUIBinding(), providerBindingTo("provider-fga.platform-mesh.io"),
			}
			return nil
		}).Once()

	err := migrateLegacyProviderBinding(ctx, s.helperMock, &rest.Config{}, "wildwest", "root:platform-mesh-system", true, true)
	s.Assert().NoError(err)
	providerClientMock.AssertNotCalled(s.T(), "Apply", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	providerClientMock.AssertNotCalled(s.T(), "Delete", mock.Anything, mock.Anything)
}

// A binding that accepts only part of the export's claims is patched to the full set.
func (s *MigrationTestSuite) Test_migrateLegacyProviderBinding_CompletesPartialUIClaims() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	providerClientMock := new(mocks.Client)
	s.helperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers:wildwest").Return(providerClientMock, nil)

	uiBinding := providerUIBinding()
	_ = unstructured.SetNestedSlice(uiBinding.Object, uiBindingClaims()[:1], "spec", "permissionClaims")

	providerClientMock.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
			ol.(*unstructured.UnstructuredList).Items = []unstructured.Unstructured{uiBinding, providerBindingTo("provider-fga.platform-mesh.io")}
			return nil
		}).Once()

	var appliedClaims []any
	providerClientMock.EXPECT().Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, obj runtime.ApplyConfiguration, opts ...ctrlruntimeclient.ApplyOption) error {
			u, ok := obj.(runtime.Unstructured)
			s.Require().True(ok)
			appliedClaims, _, _ = unstructured.NestedSlice(u.UnstructuredContent(), "spec", "permissionClaims")
			return nil
		}).Once()

	err := migrateLegacyProviderBinding(ctx, s.helperMock, &rest.Config{}, "wildwest", "root:platform-mesh-system", true, true)
	s.Require().NoError(err)
	s.Assert().Equal(uiBindingClaims(), appliedClaims)
}

// The apiexports claim was added to ui.platform-mesh.io after the split; kcp only copies
// claims into default bindings at creation, so earlier provider bindings must be patched.
func (s *MigrationTestSuite) Test_migrateLegacyProviderBinding_AcceptsUIClaimsOnExistingBinding() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	providerClientMock := new(mocks.Client)
	s.helperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers:wildwest").Return(providerClientMock, nil)

	uiBinding := providerBindingTo("ui.platform-mesh.io")
	uiBinding.SetName("ui.platform-mesh.io-abcde")
	_ = unstructured.SetNestedField(uiBinding.Object, "root:platform-mesh-system", "spec", "reference", "export", "path")

	providerClientMock.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
			list := ol.(*unstructured.UnstructuredList)
			list.Items = []unstructured.Unstructured{
				providerBindingTo("core.platform-mesh.io"), uiBinding, providerBindingTo("provider-fga.platform-mesh.io"),
			}
			return nil
		}).Once()

	var applied *unstructured.Unstructured
	providerClientMock.EXPECT().Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, obj runtime.ApplyConfiguration, opts ...ctrlruntimeclient.ApplyOption) error {
			u, ok := obj.(runtime.Unstructured)
			s.Require().True(ok)
			applied = &unstructured.Unstructured{Object: u.UnstructuredContent()}
			return nil
		}).Once()

	err := migrateLegacyProviderBinding(ctx, s.helperMock, &rest.Config{}, "wildwest", "root:platform-mesh-system", true, true)
	s.Require().NoError(err)
	s.Require().NotNil(applied)
	s.Assert().Equal("ui.platform-mesh.io-abcde", applied.GetName(), "the existing default binding is patched, not a new one created")
	path, _, _ := unstructured.NestedString(applied.Object, "spec", "reference", "export", "path")
	s.Assert().Equal("root:platform-mesh-system", path)
	claims, _, _ := unstructured.NestedSlice(applied.Object, "spec", "permissionClaims")
	s.Assert().Equal(uiBindingClaims(), claims)
	providerClientMock.AssertNotCalled(s.T(), "Delete", mock.Anything, mock.Anything)
}

func (s *MigrationTestSuite) Test_migrateLegacyProviderBinding_KeepsAcceptedClaimsOnCoreBinding() {
	ctx := context.WithValue(context.Background(), keys.LoggerCtxKey, s.log)

	providerClientMock := new(mocks.Client)
	s.helperMock.EXPECT().NewKcpClient(mock.Anything, "root:providers:wildwest").Return(providerClientMock, nil)

	binding := legacyProviderCoreBinding()
	claims := []any{map[string]any{
		"group": "apis.kcp.io", "resource": "apiexports", "state": "Accepted",
		"selector": map[string]any{"matchAll": true}, "verbs": []any{"*"},
	}}
	_ = unstructured.SetNestedSlice(binding.Object, claims, "spec", "permissionClaims")

	providerClientMock.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
			ol.(*unstructured.UnstructuredList).Items = []unstructured.Unstructured{binding}
			return nil
		}).Once()
	providerClientMock.EXPECT().Patch(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	providerClientMock.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil).Once()
	providerClientMock.EXPECT().Get(mock.Anything, types.NamespacedName{Name: binding.GetName()}, mock.Anything).
		Return(apierrors.NewNotFound(schema.GroupResource{Group: "apis.kcp.io", Resource: "apibindings"}, binding.GetName()))

	appliedClaims := map[string][]any{}
	providerClientMock.EXPECT().Apply(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, obj runtime.ApplyConfiguration, opts ...ctrlruntimeclient.ApplyOption) error {
			u, ok := obj.(runtime.Unstructured)
			s.Require().True(ok)
			got, _, _ := unstructured.NestedSlice(u.UnstructuredContent(), "spec", "permissionClaims")
			appliedClaims[u.UnstructuredContent()["metadata"].(map[string]any)["name"].(string)] = got
			return nil
		}).Times(3)

	err := migrateLegacyProviderBinding(ctx, s.helperMock, &rest.Config{}, "wildwest", "root:platform-mesh-system", true, true)
	s.Assert().NoError(err)
	s.Assert().Equal(claims, appliedClaims["core.platform-mesh.io"])
	s.Assert().Equal(uiBindingClaims(), appliedClaims["ui.platform-mesh.io"])
	s.Assert().Empty(appliedClaims["provider-fga.platform-mesh.io"])
}
