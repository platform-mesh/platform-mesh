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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/security-operator/internal/subroutine"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
	kcpapisv1alpha2 "github.com/kcp-dev/sdk/apis/apis/v1alpha2"
)

func newApiBinding(name, path string) *kcpapisv1alpha2.APIBinding {
	return &kcpapisv1alpha2.APIBinding{
		Spec: kcpapisv1alpha2.APIBindingSpec{Reference: kcpapisv1alpha2.BindingReference{Export: &kcpapisv1alpha2.ExportBindingReference{Name: name, Path: path}}},
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
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if _, ok := o.(*pmcorev1alpha1.AccountInfo); ok {
						return nil
					}
					return nil
				}).Once()
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
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if _, ok := o.(*pmcorev1alpha1.AccountInfo); ok {
						return nil
					}
					return nil
				}).Once()
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if ae, ok := o.(*kcpapisv1alpha2.APIExport); ok {
						ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
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
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if ae, ok := o.(*kcpapisv1alpha2.APIExport); ok {
						ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
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
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if ae, ok := o.(*kcpapisv1alpha2.APIExport); ok {
						ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
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
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if ae, ok := o.(*kcpapisv1alpha2.APIExport); ok {
						ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
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
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					if ae, ok := o.(*kcpapisv1alpha2.APIExport); ok {
						ae.Spec.Resources = []kcpapisv1alpha2.ResourceSchema{{Schema: "schema1"}}
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
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("export-cluster")).Return(nil, assert.AnError)
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
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("cluster1")).Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("export-cluster")).Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
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
				apiExportClient.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil)
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
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("cluster1")).Return(bindingCluster, nil)
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("export-cluster")).Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
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
				apiExportClient.EXPECT().Delete(mock.Anything, mock.Anything).Return(assert.AnError)
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
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("cluster1")).Return(bindingWsCluster1, nil)
				bindingWsCluster1.EXPECT().GetClient().Return(bindingWsClient1)
				bindingWsClient1.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("cluster2")).Return(bindingWsCluster2, nil)
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
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("cluster1")).Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("export-cluster")).Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
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
				apiExportClient.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil)
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
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("cluster1")).Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("export-cluster")).Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
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
				apiExportClient.EXPECT().Delete(mock.Anything, mock.Anything).Return(
					apierrors.NewNotFound(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "authorizationmodels"}, "foos-org"))
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
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("cluster1")).Return(bindingWsCluster1, nil)
				bindingWsCluster1.EXPECT().GetClient().Return(bindingWsClient1)
				bindingWsClient1.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("cluster2")).Return(bindingWsCluster2, nil)
				bindingWsCluster2.EXPECT().GetClient().Return(bindingWsClient2)
				bindingWsClient2.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).Return(
					apierrors.NewNotFound(schema.GroupResource{Group: "account.platform-mesh.org", Resource: "accountinfos"}, "account"))
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("export-cluster")).Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
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
				apiExportClient.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil)
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
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("cluster1")).Return(nil, assert.AnError)
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
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("cluster1")).Return(bindingWsCluster, nil)
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
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("cluster1")).Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "different-org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("export-cluster")).Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
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
				apiExportClient.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil)
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
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("cluster1")).Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("export-cluster")).Return(nil, assert.AnError)
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
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("cluster1")).Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("export-cluster")).Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
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
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("cluster1")).Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o ctrlruntimeclient.Object, opts ...ctrlruntimeclient.GetOption) error {
					acc := o.(*pmcorev1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, multicluster.ClusterName("export-cluster")).Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
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
