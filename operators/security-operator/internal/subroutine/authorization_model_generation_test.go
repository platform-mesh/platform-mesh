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

package subroutine_test

import (
	"context"
	"strings"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	language "github.com/openfga/language/pkg/go/transformer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	pmprovidersv1alpha1 "go.platform-mesh.io/apis/providers/v1alpha1"
	"go.platform-mesh.io/security-operator/internal/config"
	"go.platform-mesh.io/security-operator/internal/subroutine"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

// Collection permissions are evaluated against these parent types. In particular,
// authenticated_user exists only on the account, not on either resource type.
const collectionPermissionsCoreModule = `module core

type user

type role
  relations
    define assignee: [user, user:*]

type core_platform-mesh_io_account
  relations
    define parent: [core_platform-mesh_io_account]
    define owner: [role#assignee] or owner from parent
    define member: [role#assignee] or owner
    define authenticated_user: [role#assignee] or authenticated_user from parent

type core_namespace
  relations
    define parent: [core_platform-mesh_io_account]
    define owner: [role#assignee] or owner from parent
    define member: [role#assignee] or owner or member from parent
`

func TestAuthorizationModelGeneration_CollectionPermissions(t *testing.T) {
	for _, scope := range []apiextensionsv1.ResourceScope{apiextensionsv1.ClusterScoped, apiextensionsv1.NamespaceScoped} {
		t.Run(string(scope), func(t *testing.T) {
			parentType := "core_platform-mesh_io_account"
			readExpression := "authenticated_user or member"
			if scope == apiextensionsv1.NamespaceScoped {
				parentType = "core_namespace"
				readExpression = "owner or member"
			}

			tests := []struct {
				name        string
				permissions *pmprovidersv1alpha1.DefaultPermissions
			}{
				{name: "no ProviderPermissions"},
				{name: "empty overrides", permissions: &pmprovidersv1alpha1.DefaultPermissions{}},
				{name: "create override", permissions: &pmprovidersv1alpha1.DefaultPermissions{Create: "member"}},
				{name: "list override", permissions: &pmprovidersv1alpha1.DefaultPermissions{List: readExpression}},
				{name: "watch override", permissions: &pmprovidersv1alpha1.DefaultPermissions{Watch: readExpression}},
				{
					name: "read overrides",
					permissions: &pmprovidersv1alpha1.DefaultPermissions{
						Get:   strings.Split(readExpression, " or ")[0] + " from parent or member",
						List:  readExpression,
						Watch: readExpression,
					},
				},
				{
					name: "all collection overrides",
					permissions: &pmprovidersv1alpha1.DefaultPermissions{
						Create: "member",
						Get:    strings.Split(readExpression, " or ")[0] + " from parent or member",
						List:   readExpression,
						Watch:  readExpression,
					},
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					ctx := context.Background()
					scheme := runtime.NewScheme()
					require.NoError(t, pmcorev1alpha1.AddToScheme(scheme))
					require.NoError(t, pmprovidersv1alpha1.AddToScheme(scheme))
					require.NoError(t, kcpapisv1alpha1.AddToScheme(scheme))
					require.NoError(t, kcpapisv1alpha2.AddToScheme(scheme))

					accountInfo := &pmcorev1alpha1.AccountInfo{ObjectMeta: metav1.ObjectMeta{Name: "account"}}
					accountInfo.Spec.Organization.Name = "org"
					accountInfo.Spec.Organization.OriginClusterId = "orgs"
					apiExport := &kcpapisv1alpha2.APIExport{
						ObjectMeta: metav1.ObjectMeta{Name: "metadata.dxp.sap.com"},
						Spec: kcpapisv1alpha2.APIExportSpec{Resources: []kcpapisv1alpha2.ResourceSchema{
							{Schema: "v1.components.metadata.dxp.sap.com"},
							{Schema: "v1.commits.metadata.dxp.sap.com"},
						}},
					}
					objects := []ctrlruntimeclient.Object{
						accountInfo, apiExport,
						&pmcorev1alpha1.Store{ObjectMeta: metav1.ObjectMeta{Name: "org"}},
					}
					for _, resource := range []string{"component", "commit"} {
						objects = append(objects, &kcpapisv1alpha1.APIResourceSchema{
							ObjectMeta: metav1.ObjectMeta{Name: "v1." + resource + "s.metadata.dxp.sap.com"},
							Spec: kcpapisv1alpha1.APIResourceSchemaSpec{
								Group: "metadata.dxp.sap.com",
								Names: apiextensionsv1.CustomResourceDefinitionNames{Singular: resource, Plural: resource + "s"},
								Scope: scope,
							},
						})
					}
					if tt.permissions != nil {
						objects = append(objects, &pmprovidersv1alpha1.ProviderPermissions{
							ObjectMeta: metav1.ObjectMeta{Name: "metadata", Generation: 1},
							Spec: pmprovidersv1alpha1.ProviderPermissionsSpec{
								APIExport: pmprovidersv1alpha1.APIExport{Ref: pmprovidersv1alpha1.APIExportReference{Name: apiExport.Name}},
								Permissions: map[string]pmprovidersv1alpha1.ResourcePermissions{
									"component.metadata.dxp.sap.com": {DefaultPermissions: *tt.permissions},
								},
							},
						})
					}
					client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).
						WithStatusSubresource(&pmprovidersv1alpha1.ProviderPermissions{}).Build()
					manager := mocks.NewMockManager(t)
					cluster := mocks.NewMockCluster(t)
					cluster.EXPECT().GetClient().Return(client)
					manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
					manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil)
					processor := subroutine.NewAuthorizationModelGenerationSubroutine(manager, mocks.NewMockLister(t))
					binding := providerBoundBinding(apiExport.Name, "root:providers:metadata", "consumer", "provider")
					_, err := processor.Process(ctx, binding)
					require.NoError(t, err)

					checkModels := func(overrides pmprovidersv1alpha1.DefaultPermissions) {
						t.Helper()
						modules := make([]language.ModuleFile, 0, 3)
						modules = append(modules, language.ModuleFile{Name: "core.fga", Contents: collectionPermissionsCoreModule})
						for _, resource := range []string{"component", "commit"} {
							var model pmcorev1alpha1.AuthorizationModel
							require.NoError(t, client.Get(ctx, types.NamespacedName{Name: "metadata-dxp-sap-com-" + resource + "s-org"}, &model))
							assert.Equal(t, pmcorev1alpha1.WorkspaceStoreRef{Name: "org", Cluster: "orgs"}, model.Spec.StoreRef)
							modules = append(modules, language.ModuleFile{Name: resource + ".fga", Contents: model.Spec.Model})
						}
						model, err := language.TransformModuleFilesToModel(modules, "1.2")
						require.NoError(t, err, "overrides must form a valid combined model with the existing parent relations")
						types := map[string]*openfgav1.TypeDefinition{}
						for _, definition := range model.TypeDefinitions {
							types[definition.Type] = definition
						}
						for _, resource := range []string{"component", "commit"} {
							expected := pmprovidersv1alpha1.DefaultPermissions{}
							if resource == "component" {
								expected = overrides
							}
							for verb, expression := range map[string]string{"create": expected.Create, "list": expected.List, "watch": expected.Watch} {
								if expression == "" {
									expression = "member"
									if verb == "create" {
										expression = "owner"
									}
								}
								relation := verb + "_metadata_dxp_sap_com_" + resource + "s"
								rewrite := types[parentType].Relations[relation]
								require.NotNil(t, rewrite, "collection permission must exist on the correct parent")
								terms := []*openfgav1.Userset{rewrite}
								if rewrite.GetUnion() != nil {
									terms = rewrite.GetUnion().Child
								}
								var relations []string
								for _, term := range terms {
									relations = append(relations, term.GetComputedUserset().GetRelation())
								}
								assert.Equal(t, strings.Split(expression, " or "), relations, relation)
								otherParent := "core_namespace"
								if parentType == otherParent {
									otherParent = "core_platform-mesh_io_account"
								}
								assert.NotContains(t, types[otherParent].Relations, relation)
							}
							resourceType := types["metadata_dxp_sap_com_"+resource]
							for _, verb := range []string{"update", "delete", "patch"} {
								assert.Equal(t, "member", resourceType.Relations[verb].GetComputedUserset().GetRelation(), verb)
							}
							if expected.Get == "" {
								assert.Equal(t, "member", resourceType.Relations["get"].GetComputedUserset().GetRelation())
							} else {
								get := resourceType.Relations["get"].GetUnion().GetChild()
								require.Len(t, get, 2)
								assert.Equal(t, strings.Split(expected.Get, " from parent")[0], get[0].GetTupleToUserset().GetComputedUserset().GetRelation())
								assert.Equal(t, "parent", get[0].GetTupleToUserset().GetTupleset().GetRelation())
								assert.Equal(t, "member", get[1].GetComputedUserset().GetRelation())
							}
							if expected.Watch == "" {
								assert.Equal(t, "member", resourceType.Relations["watch"].GetComputedUserset().GetRelation())
							} else {
								inheritance := resourceType.Relations["watch"].GetTupleToUserset()
								require.NotNil(t, inheritance, "object watch must inherit the parent override")
								assert.Equal(t, "parent", inheritance.Tupleset.Relation)
								assert.Equal(t, "watch_metadata_dxp_sap_com_"+resource+"s", inheritance.ComputedUserset.Relation)
							}
						}
					}

					if tt.permissions == nil {
						checkModels(pmprovidersv1alpha1.DefaultPermissions{})
						return
					}
					checkModels(*tt.permissions)
					var permissions pmprovidersv1alpha1.ProviderPermissions
					require.NoError(t, client.Get(ctx, types.NamespacedName{Name: "metadata"}, &permissions))
					assert.Equal(t, int64(1), permissions.Status.ObservedGeneration)
					assert.True(t, meta.IsStatusConditionTrue(permissions.Status.Conditions, "Applied"))

					// Removing overrides must update existing models back to their defaults.
					permissions.Spec.Permissions = nil
					require.NoError(t, client.Update(ctx, &permissions))
					_, err = processor.Process(ctx, binding)
					require.NoError(t, err)
					checkModels(pmprovidersv1alpha1.DefaultPermissions{})
				})
			}
		})
	}
}

func newApiBinding(name, path string) *kcpapisv1alpha2.APIBinding {
	return &kcpapisv1alpha2.APIBinding{
		Spec:   kcpapisv1alpha2.APIBindingSpec{Reference: kcpapisv1alpha2.BindingReference{Export: &kcpapisv1alpha2.ExportBindingReference{Name: name, Path: path}}},
		Status: kcpapisv1alpha2.APIBindingStatus{APIExportClusterName: "export-cluster"},
	}
}

func bindingWithCluster(name, path, cluster string) *kcpapisv1alpha2.APIBinding {
	b := newApiBinding(name, path)
	if b.Annotations == nil {
		b.Annotations = make(map[string]string)
	}
	b.Annotations["kcp.io/cluster"] = cluster
	return b
}

func bindingWithApiExportCluster(name, path, exportCluster string) *kcpapisv1alpha2.APIBinding {
	b := newApiBinding(name, path)
	b.Status.APIExportClusterName = exportCluster
	return b
}

// selfBoundBinding is a binding whose APIExport lives in its own workspace, as KROaaS
// publishes composite types. It has no path and its export cluster is its own cluster.
func selfBoundBinding(name, cluster string) *kcpapisv1alpha2.APIBinding {
	b := bindingWithCluster(name, "", cluster)
	b.Status.APIExportClusterName = cluster
	return b
}

// providerBoundBinding is a binding whose APIExport lives in a provider workspace.
func providerBoundBinding(name, path, cluster, exportCluster string) *kcpapisv1alpha2.APIBinding {
	b := bindingWithCluster(name, path, cluster)
	b.Status.APIExportClusterName = exportCluster
	return b
}

func mockAccountInfo(cl *mocks.MockClient, orgName, originCluster string) {
	cl.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
		if acc, ok := o.(*pmcorev1alpha1.AccountInfo); ok {
			acc.Spec.Organization.Name = orgName
			acc.Spec.Organization.OriginClusterId = originCluster
		}
		return nil
	}).Once()
}

func TestAuthorizationModelGeneration_Process(t *testing.T) {
	tests := []struct {
		name        string
		binding     *kcpapisv1alpha2.APIBinding
		mockSetup   func(*mocks.MockManager, *mocks.MockLister, *mocks.MockCluster, *mocks.MockClient)
		expectError bool
	}{
		{
			name:    "error on ClusterFromContext in Process",
			binding: newApiBinding("foo", "bar"),
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(nil, assert.AnError)
			},
			expectError: true,
		},
		{
			name:        "early return when accountInfo not found",
			binding:     newApiBinding("foo", "bar"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(
					apierrors.NewNotFound(schema.GroupResource{Group: "account.platform-mesh.org", Resource: "accountinfos"}, "account"))
			},
		},
		{
			name:        "error on getting apiExport",
			binding:     newApiBinding("foo", "bar"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError)
			},
		},
		{
			name:        "error from CreateOrUpdate when creating model",
			binding:     newApiBinding("foo", "bar"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil).Times(4)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if ae, ok := o.(*kcpapisv1alpha2.APIExport); ok {
						ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().List(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, list ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) error {
					if _, ok := list.(*pmprovidersv1alpha1.ProviderPermissionsList); ok {
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if rs, ok := o.(*kcpapisv1alpha1.APIResourceSchema); ok {
						rs.Spec.Group = "group"
						rs.Spec.Names.Plural = "things"
						rs.Spec.Names.Singular = "thing"
						return nil
					}
					return nil
				}).Once()
				// registerAuthorizationModelWithStore: fetch the Store, add its finalizer.
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
				kcpClient.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(
					apierrors.NewNotFound(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "authorizationmodels"}, "things-org")).Once()
				kcpClient.EXPECT().Create(mock.Anything, mock.Anything).Return(assert.AnError).Once()
			},
		},
		{
			name:      "skip core exports in Process (early return, no mocks)",
			binding:   newApiBinding("core.platform-mesh.io", "root"),
			mockSetup: nil,
		},
		{
			name:      "skip ui.platform-mesh.io exports in Process (early return, no mocks)",
			binding:   newApiBinding("ui.platform-mesh.io", "root"),
			mockSetup: nil,
		},
		{
			name:      "skip fga.platform-mesh.io exports in Process (early return, no mocks)",
			binding:   newApiBinding("fga.platform-mesh.io", "root"),
			mockSetup: nil,
		},
		{
			name:      "skip provider-fga.platform-mesh.io exports in Process (early return, no mocks)",
			binding:   newApiBinding("provider-fga.platform-mesh.io", "root"),
			mockSetup: nil,
		},
		{
			name:      "skip kcp.io exports in Process (early return, no mocks)",
			binding:   newApiBinding("tenancy.kcp.io", "root"),
			mockSetup: nil,
		},
		{
			name:    "generate model in Process",
			binding: newApiBinding("foo", "bar"),
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				mockAccountInfo(kcpClient, "org", "origin")
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil).Times(4)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if ae, ok := o.(*kcpapisv1alpha2.APIExport); ok {
						ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().List(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, list ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) error {
					if _, ok := list.(*pmprovidersv1alpha1.ProviderPermissionsList); ok {
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if rs, ok := o.(*kcpapisv1alpha1.APIResourceSchema); ok {
						rs.Spec.Group = "group"
						rs.Spec.Names.Plural = "foos"
						rs.Spec.Names.Singular = "foo"
						rs.Spec.Scope = apiextensionsv1.ClusterScoped
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(nil)
				kcpClient.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Maybe()
				kcpClient.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Maybe()
			},
		},
		{
			name:    "generate model in Process with namespaced scope",
			binding: newApiBinding("foo", "bar"),
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				mockAccountInfo(kcpClient, "org", "origin")
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil).Times(4)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if ae, ok := o.(*kcpapisv1alpha2.APIExport); ok {
						ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().List(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, list ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) error {
					if _, ok := list.(*pmprovidersv1alpha1.ProviderPermissionsList); ok {
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if rs, ok := o.(*kcpapisv1alpha1.APIResourceSchema); ok {
						rs.Spec.Group = "group"
						rs.Spec.Names.Plural = "foos"
						rs.Spec.Names.Singular = "foo"
						rs.Spec.Scope = apiextensionsv1.NamespaceScoped
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(nil)
				kcpClient.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Maybe()
				kcpClient.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Maybe()
			},
		},
		{
			name:        "error on apiExportClient.Get in Process",
			binding:     newApiBinding("foo", "bar"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				mockAccountInfo(kcpClient, "org", "origin")
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError)
			},
		},
		{
			name:        "error on apiExportClient.Get resource schema in Process",
			binding:     newApiBinding("foo", "bar"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				mockAccountInfo(kcpClient, "org", "origin")
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil).Times(3)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if ae, ok := o.(*kcpapisv1alpha2.APIExport); ok {
						ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().List(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, list ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) error {
					if _, ok := list.(*pmprovidersv1alpha1.ProviderPermissionsList); ok {
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError)
			},
		},
		{
			name:    "generate model in Process with longestRelationName > 50",
			binding: newApiBinding("foo", "bar"),
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				mockAccountInfo(kcpClient, "org", "origin")
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil).Times(4)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if ae, ok := o.(*kcpapisv1alpha2.APIExport); ok {
						ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().List(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, list ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) error {
					if _, ok := list.(*pmprovidersv1alpha1.ProviderPermissionsList); ok {
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if rs, ok := o.(*kcpapisv1alpha1.APIResourceSchema); ok {
						rs.Spec.Group = "veryverylonggroup.platform-mesh.org"
						rs.Spec.Names.Plural = "plural"
						rs.Spec.Names.Singular = "singular"
						rs.Spec.Scope = apiextensionsv1.ClusterScoped
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(nil)
				kcpClient.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Maybe()
				kcpClient.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Maybe()
			},
		},
		{
			name:        "error on Get accountInfo in Process (not NotFound)",
			binding:     newApiBinding("foo", "bar"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError)
			},
		},
		{
			name:        "error on GetCluster for APIExport cluster in Process",
			binding:     bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				mockAccountInfo(kcpClient, "org", "origin")
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "export-cluster")).Return(nil, assert.AnError)
			},
		},
		{
			name:    "generate model with ProviderPermissions not found",
			binding: newApiBinding("foo", "bar"),
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				mockAccountInfo(kcpClient, "org", "origin")
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil).Times(4)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if ae, ok := o.(*kcpapisv1alpha2.APIExport); ok {
						ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
						ae.Name = "test-export"
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().List(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, list ctrlruntimeclient.ObjectList, opts ...ctrlruntimeclient.ListOption) error {
					if _, ok := list.(*pmprovidersv1alpha1.ProviderPermissionsList); ok {
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if rs, ok := o.(*kcpapisv1alpha1.APIResourceSchema); ok {
						rs.Spec.Group = "group"
						rs.Spec.Names.Plural = "foos"
						rs.Spec.Names.Singular = "foo"
						rs.Spec.Scope = apiextensionsv1.ClusterScoped
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(nil)
				kcpClient.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Maybe()
				kcpClient.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Maybe()
			},
		},
		{
			// A self-bound export lives in the consumer's own workspace, which is never in
			// providers scope. The model must still be generated from the export's schemas,
			// so no provider cluster is looked up at all: GetCluster is expected twice
			// (binding workspace + APIExport cluster) rather than three times.
			name:        "self-bound export generates a model without a provider lookup",
			binding:     selfBoundBinding("kro-greeting", "consumer-cluster"),
			expectError: false,
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				mockAccountInfo(kcpClient, "org", "origin")
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil).Times(3)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if ae, ok := o.(*kcpapisv1alpha2.APIExport); ok {
						ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
						ae.Name = "kro-greeting"
					}
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if rs, ok := o.(*kcpapisv1alpha1.APIResourceSchema); ok {
						rs.Spec.Group = "kro.run"
						rs.Spec.Names.Plural = "greetings"
						rs.Spec.Names.Singular = "greeting"
						rs.Spec.Scope = "Namespaced"
					}
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(nil)
				kcpClient.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Maybe()
				kcpClient.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Maybe()
			},
		},
		{
			// A provider-backed export must keep failing closed: if its provider cluster
			// cannot be resolved the model is left untouched rather than regenerated
			// without the provider's ProviderPermissions overrides.
			name:        "provider-backed export aborts when the provider cluster is unresolvable",
			binding:     providerBoundBinding("some-export", "root:providers:p", "consumer-cluster", "provider-cluster"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				mockAccountInfo(kcpClient, "org", "origin")
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil).Twice()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if ae, ok := o.(*kcpapisv1alpha2.APIExport); ok {
						ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
						ae.Name = "some-export"
					}
					return nil
				}).Once()
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()
			},
		},
		{
			// An unreconciled binding has no APIExportClusterName, which does not match its
			// own cluster, so it is not self-bound and fails closed.
			name:        "unset APIExportClusterName takes the strict path",
			binding:     bindingWithCluster("foo", "bar", "consumer-cluster"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				mockAccountInfo(kcpClient, "org", "origin")
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil).Twice()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if ae, ok := o.(*kcpapisv1alpha2.APIExport); ok {
						ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
						ae.Name = "foo"
					}
					return nil
				}).Once()
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(nil, assert.AnError).Once()
			},
		},
		{
			name:        "error on ProviderPermissions list",
			binding:     newApiBinding("foo", "bar"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				mockAccountInfo(kcpClient, "org", "origin")
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil).Times(3)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if ae, ok := o.(*kcpapisv1alpha2.APIExport); ok {
						ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
						ae.Name = "test-export"
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().List(mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := mocks.NewMockManager(t)
			lister := mocks.NewMockLister(t)
			cluster := mocks.NewMockCluster(t)
			kcpClient := mocks.NewMockClient(t)

			if test.mockSetup != nil {
				test.mockSetup(manager, lister, cluster, kcpClient)
			}

			sub := subroutine.NewAuthorizationModelGenerationSubroutine(manager, lister)
			_, err := sub.Process(context.Background(), test.binding)
			if test.expectError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestAuthorizationModelGeneration_Finalize(t *testing.T) {
	tests := []struct {
		name        string
		binding     *kcpapisv1alpha2.APIBinding
		mockSetup   func(*mocks.MockManager, *mocks.MockLister, *kcpapisv1alpha2.APIBinding)
		expectError bool
	}{
		{
			name:    "bindings with non-matching export are skipped",
			binding: bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, binding *kcpapisv1alpha2.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				bindingWsCluster := mocks.NewMockCluster(t)
				bindingWsClient := mocks.NewMockClient(t)
				apiExportCluster := mocks.NewMockCluster(t)
				apiExportClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
					list := ol.(*kcpapisv1alpha2.APIBindingList)
					b1 := bindingWithCluster("foo", "bar", "cluster1")
					b2 := bindingWithCluster("other", "other", "cluster2")
					list.Items = []kcpapisv1alpha2.APIBinding{*b1, *b2}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "cluster1")).Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "export-cluster")).Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
				modelCluster := mocks.NewMockCluster(t)
				modelClient := mocks.NewMockClient(t)
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.ProviderFgaProviderName, "export-cluster")).Return(modelCluster, nil)
				modelCluster.EXPECT().GetClient().Return(modelClient)
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "foo"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					ae := o.(*kcpapisv1alpha2.APIExport)
					ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
					return nil
				})
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "schema1"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					rs := o.(*kcpapisv1alpha1.APIResourceSchema)
					rs.Spec.Names.Plural = "foos"
					return nil
				})
				modelClient.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil)
			},
		},
		{
			name:        "error on ClusterFromContext in Finalize",
			binding:     bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, binding *kcpapisv1alpha2.APIBinding) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(nil, assert.AnError)
			},
		},
		{
			name:        "early return when accountInfo missing in Finalize",
			binding:     bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, binding *kcpapisv1alpha2.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
					list := ol.(*kcpapisv1alpha2.APIBindingList)
					b := binding.DeepCopy()
					if b.Annotations == nil {
						b.Annotations = make(map[string]string)
					}
					b.Annotations["kcp.io/cluster"] = "cluster1"
					list.Items = []kcpapisv1alpha2.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).Return(
					apierrors.NewNotFound(schema.GroupResource{Group: "account.platform-mesh.org", Resource: "accountinfos"}, "account"))
			},
		},
		{
			name:        "delete returns error in Finalize",
			binding:     bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, binding *kcpapisv1alpha2.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				apiExportCluster := mocks.NewMockCluster(t)
				apiExportClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
					list := ol.(*kcpapisv1alpha2.APIBindingList)
					b := binding.DeepCopy()
					if b.Annotations == nil {
						b.Annotations = make(map[string]string)
					}
					b.Annotations["kcp.io/cluster"] = "cluster1"
					list.Items = []kcpapisv1alpha2.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "cluster1")).Return(bindingCluster, nil)
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "export-cluster")).Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
				modelCluster := mocks.NewMockCluster(t)
				modelClient := mocks.NewMockClient(t)
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.ProviderFgaProviderName, "export-cluster")).Return(modelCluster, nil)
				modelCluster.EXPECT().GetClient().Return(modelClient)
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "foo"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					ae := o.(*kcpapisv1alpha2.APIExport)
					ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
					return nil
				})
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "schema1"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					rs := o.(*kcpapisv1alpha1.APIResourceSchema)
					rs.Spec.Names.Plural = "foos"
					return nil
				})
				modelClient.EXPECT().Delete(mock.Anything, mock.Anything).Return(assert.AnError)
			},
		},
		{
			name:    "skip Finalize if other bindings exist",
			binding: bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, binding *kcpapisv1alpha2.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				bindingWsCluster1 := mocks.NewMockCluster(t)
				bindingWsClient1 := mocks.NewMockClient(t)
				bindingWsCluster2 := mocks.NewMockCluster(t)
				bindingWsClient2 := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
					list := ol.(*kcpapisv1alpha2.APIBindingList)
					b1 := bindingWithCluster("foo", "bar", "cluster1")
					b2 := bindingWithCluster("foo", "bar", "cluster2")
					list.Items = []kcpapisv1alpha2.APIBinding{*b1, *b2}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "cluster1")).Return(bindingWsCluster1, nil)
				bindingWsCluster1.EXPECT().GetClient().Return(bindingWsClient1)
				bindingWsClient1.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "cluster2")).Return(bindingWsCluster2, nil)
				bindingWsCluster2.EXPECT().GetClient().Return(bindingWsClient2)
				bindingWsClient2.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
			},
		},
		{
			name:    "delete model in Finalize if last binding",
			binding: bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, binding *kcpapisv1alpha2.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				bindingWsCluster := mocks.NewMockCluster(t)
				bindingWsClient := mocks.NewMockClient(t)
				apiExportCluster := mocks.NewMockCluster(t)
				apiExportClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
					list := ol.(*kcpapisv1alpha2.APIBindingList)
					b := binding.DeepCopy()
					if b.Annotations == nil {
						b.Annotations = make(map[string]string)
					}
					b.Annotations["kcp.io/cluster"] = "cluster1"
					list.Items = []kcpapisv1alpha2.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "cluster1")).Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "export-cluster")).Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
				modelCluster := mocks.NewMockCluster(t)
				modelClient := mocks.NewMockClient(t)
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.ProviderFgaProviderName, "export-cluster")).Return(modelCluster, nil)
				modelCluster.EXPECT().GetClient().Return(modelClient)
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "foo"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					ae := o.(*kcpapisv1alpha2.APIExport)
					ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
					return nil
				})
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "schema1"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					rs := o.(*kcpapisv1alpha1.APIResourceSchema)
					rs.Spec.Names.Plural = "foos"
					return nil
				})
				modelClient.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil)
			},
		},
		{
			name:    "delete model in Finalize but model is not found",
			binding: bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, binding *kcpapisv1alpha2.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				bindingWsCluster := mocks.NewMockCluster(t)
				bindingWsClient := mocks.NewMockClient(t)
				apiExportCluster := mocks.NewMockCluster(t)
				apiExportClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
					list := ol.(*kcpapisv1alpha2.APIBindingList)
					b := binding.DeepCopy()
					if b.Annotations == nil {
						b.Annotations = make(map[string]string)
					}
					b.Annotations["kcp.io/cluster"] = "cluster1"
					list.Items = []kcpapisv1alpha2.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "cluster1")).Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "export-cluster")).Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
				modelCluster := mocks.NewMockCluster(t)
				modelClient := mocks.NewMockClient(t)
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.ProviderFgaProviderName, "export-cluster")).Return(modelCluster, nil)
				modelCluster.EXPECT().GetClient().Return(modelClient)
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "foo"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					ae := o.(*kcpapisv1alpha2.APIExport)
					ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
					return nil
				})
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "schema1"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					rs := o.(*kcpapisv1alpha1.APIResourceSchema)
					rs.Spec.Names.Plural = "foos"
					return nil
				})
				modelClient.EXPECT().Delete(mock.Anything, mock.Anything).Return(
					apierrors.NewNotFound(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "authorizationmodels"}, "foos-org"))
				// deregisterAuthorizationModelFromStore: the model was never created, so release
				// any reservation left on the Store. A NotFound Store means nothing to release.
				storeCluster := mocks.NewMockCluster(t)
				storeClient := mocks.NewMockClient(t)
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(storeCluster, nil)
				storeCluster.EXPECT().GetClient().Return(storeClient)
				storeClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(
					apierrors.NewNotFound(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "stores"}, "org"))
			},
		},
		{
			name:        "error on List in Finalize",
			binding:     newApiBinding("foo", "bar"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, binding *kcpapisv1alpha2.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				lister.EXPECT().List(mock.Anything, mock.Anything).Return(assert.AnError)
			},
		},
		{
			name:        "error on getRelatedAuthorizationModels in Finalize",
			binding:     bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, binding *kcpapisv1alpha2.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
					list := ol.(*kcpapisv1alpha2.APIBindingList)
					b := binding.DeepCopy()
					if b.Annotations == nil {
						b.Annotations = make(map[string]string)
					}
					b.Annotations["kcp.io/cluster"] = "cluster1"
					list.Items = []kcpapisv1alpha2.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).Return(assert.AnError)
			},
		},
		{
			name:    "only bindings for same org are counted; delete called if only one, not called if none",
			binding: bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, binding *kcpapisv1alpha2.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				bindingWsCluster1 := mocks.NewMockCluster(t)
				bindingWsClient1 := mocks.NewMockClient(t)
				bindingWsCluster2 := mocks.NewMockCluster(t)
				bindingWsClient2 := mocks.NewMockClient(t)
				apiExportCluster := mocks.NewMockCluster(t)
				apiExportClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
					list := ol.(*kcpapisv1alpha2.APIBindingList)
					b1 := bindingWithCluster("foo", "bar", "cluster1")
					b2 := bindingWithCluster("foo", "bar", "cluster2")
					list.Items = []kcpapisv1alpha2.APIBinding{*b1, *b2}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "cluster1")).Return(bindingWsCluster1, nil)
				bindingWsCluster1.EXPECT().GetClient().Return(bindingWsClient1)
				bindingWsClient1.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "cluster2")).Return(bindingWsCluster2, nil)
				bindingWsCluster2.EXPECT().GetClient().Return(bindingWsClient2)
				bindingWsClient2.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).Return(
					apierrors.NewNotFound(schema.GroupResource{Group: "account.platform-mesh.org", Resource: "accountinfos"}, "account"))
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "export-cluster")).Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
				modelCluster := mocks.NewMockCluster(t)
				modelClient := mocks.NewMockClient(t)
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.ProviderFgaProviderName, "export-cluster")).Return(modelCluster, nil)
				modelCluster.EXPECT().GetClient().Return(modelClient)
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "foo"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					ae := o.(*kcpapisv1alpha2.APIExport)
					ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
					return nil
				})
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "schema1"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					rs := o.(*kcpapisv1alpha1.APIResourceSchema)
					rs.Spec.Names.Plural = "foos"
					return nil
				})
				modelClient.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil)
			},
		},
		{
			name:    "error on GetCluster for binding workspace in Finalize loop",
			binding: bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, binding *kcpapisv1alpha2.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
					list := ol.(*kcpapisv1alpha2.APIBindingList)
					b := bindingWithCluster("foo", "bar", "cluster1")
					list.Items = []kcpapisv1alpha2.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "cluster1")).Return(nil, assert.AnError)
			},
			expectError: true,
		},
		{
			name:    "error on Get accountInfo in Finalize loop (not NotFound)",
			binding: bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, binding *kcpapisv1alpha2.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				bindingWsCluster := mocks.NewMockCluster(t)
				bindingWsClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
					list := ol.(*kcpapisv1alpha2.APIBindingList)
					b := bindingWithCluster("foo", "bar", "cluster1")
					list.Items = []kcpapisv1alpha2.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "cluster1")).Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).Return(assert.AnError)
			},
			expectError: true,
		},
		{
			name:    "bindings with different org are skipped in Finalize",
			binding: bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, binding *kcpapisv1alpha2.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				bindingWsCluster := mocks.NewMockCluster(t)
				bindingWsClient := mocks.NewMockClient(t)
				apiExportCluster := mocks.NewMockCluster(t)
				apiExportClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
					list := ol.(*kcpapisv1alpha2.APIBindingList)
					b := bindingWithCluster("foo", "bar", "cluster1")
					list.Items = []kcpapisv1alpha2.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "cluster1")).Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "different-org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "export-cluster")).Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
				modelCluster := mocks.NewMockCluster(t)
				modelClient := mocks.NewMockClient(t)
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.ProviderFgaProviderName, "export-cluster")).Return(modelCluster, nil)
				modelCluster.EXPECT().GetClient().Return(modelClient)
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "foo"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					ae := o.(*kcpapisv1alpha2.APIExport)
					ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
					return nil
				})
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "schema1"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					rs := o.(*kcpapisv1alpha1.APIResourceSchema)
					rs.Spec.Names.Plural = "foos"
					return nil
				})
				modelClient.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil)
			},
		},
		{
			name:        "error on GetCluster for APIExport cluster in Finalize",
			binding:     bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, binding *kcpapisv1alpha2.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				bindingWsCluster := mocks.NewMockCluster(t)
				bindingWsClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
					list := ol.(*kcpapisv1alpha2.APIBindingList)
					b := bindingWithCluster("foo", "bar", "cluster1")
					list.Items = []kcpapisv1alpha2.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "cluster1")).Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "export-cluster")).Return(nil, assert.AnError)
			},
		},
		{
			name:        "error on Get APIExport in Finalize",
			binding:     bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, binding *kcpapisv1alpha2.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				bindingWsCluster := mocks.NewMockCluster(t)
				bindingWsClient := mocks.NewMockClient(t)
				apiExportCluster := mocks.NewMockCluster(t)
				apiExportClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
					list := ol.(*kcpapisv1alpha2.APIBindingList)
					b := bindingWithCluster("foo", "bar", "cluster1")
					list.Items = []kcpapisv1alpha2.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "cluster1")).Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "export-cluster")).Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
				modelCluster := mocks.NewMockCluster(t)
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.ProviderFgaProviderName, "export-cluster")).Return(modelCluster, nil).Maybe()
				modelCluster.EXPECT().GetClient().Return(mocks.NewMockClient(t)).Maybe()
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "foo"}, mock.Anything).Return(assert.AnError)
			},
		},
		{
			name:        "error on Get resource schema in Finalize",
			binding:     bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, lister *mocks.MockLister, binding *kcpapisv1alpha2.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				bindingWsCluster := mocks.NewMockCluster(t)
				bindingWsClient := mocks.NewMockClient(t)
				apiExportCluster := mocks.NewMockCluster(t)
				apiExportClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				lister.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
					list := ol.(*kcpapisv1alpha2.APIBindingList)
					b := bindingWithCluster("foo", "bar", "cluster1")
					list.Items = []kcpapisv1alpha2.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "cluster1")).Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.CoreProviderName, "export-cluster")).Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
				modelCluster := mocks.NewMockCluster(t)
				manager.EXPECT().GetCluster(mock.Anything, config.MultiProviderName(config.ProviderFgaProviderName, "export-cluster")).Return(modelCluster, nil).Maybe()
				modelCluster.EXPECT().GetClient().Return(mocks.NewMockClient(t)).Maybe()
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "foo"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					ae := o.(*kcpapisv1alpha2.APIExport)
					ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
					return nil
				})
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "schema1"}, mock.Anything).Return(assert.AnError)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := mocks.NewMockManager(t)
			lister := mocks.NewMockLister(t)

			if test.mockSetup != nil {
				test.mockSetup(manager, lister, test.binding)
			}

			sub := subroutine.NewAuthorizationModelGenerationSubroutine(manager, lister)
			_, err := sub.Finalize(context.Background(), test.binding)
			if test.expectError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestAuthorizationModelGeneration_Finalizers(t *testing.T) {
	sub := subroutine.NewAuthorizationModelGenerationSubroutine(nil, mocks.NewMockLister(t))

	tests := []struct {
		name            string
		bindingName     string
		expectFinalizer bool
	}{
		{
			name:            "returns finalizer when name has neither platform-mesh.io nor kcp.io",
			bindingName:     "my-binding",
			expectFinalizer: true,
		},
		{
			name:            "returns no finalizer when name contains platform-mesh.io",
			bindingName:     "core.platform-mesh.io-awuzd",
			expectFinalizer: false,
		},
		{
			name:            "returns no finalizer when name contains kcp.io",
			bindingName:     "tenancy.kcp.io-dr0q1",
			expectFinalizer: false,
		},
		{
			name:            "returns no finalizer when name contains topology.kcp.io",
			bindingName:     "topology.kcp.io-5oxoy",
			expectFinalizer: false,
		},
		{
			name:            "returns no finalizer when name contains platform-mesh.io in the middle",
			bindingName:     "something.platform-mesh.io-suffix",
			expectFinalizer: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binding := newApiBinding("foo", "bar")
			binding.Name = tt.bindingName
			got := sub.Finalizers(binding)
			if tt.expectFinalizer {
				assert.Equal(t, []string{"core.platform-mesh.io/apibinding-finalizer"}, got)
			} else {
				assert.Empty(t, got)
			}
		})
	}
}

func TestAuthorizationModelGenerationSubroutine_GetName(t *testing.T) {
	sub := subroutine.NewAuthorizationModelGenerationSubroutine(nil, mocks.NewMockLister(t))
	assert.Equal(t, "AuthorizationModelGeneration", sub.GetName())
}
