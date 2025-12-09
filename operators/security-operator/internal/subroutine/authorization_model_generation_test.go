package subroutine_test

import (
	"context"
	"testing"

	kcpv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/subroutine"
	"github.com/platform-mesh/security-operator/internal/subroutine/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	accountv1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func newApiBinding(name, path string) *kcpv1alpha1.APIBinding {
	return &kcpv1alpha1.APIBinding{
		Spec: kcpv1alpha1.APIBindingSpec{Reference: kcpv1alpha1.BindingReference{Export: &kcpv1alpha1.ExportBindingReference{Name: name, Path: path}}},
	}
}

func bindingWithCluster(name, path, cluster string) *kcpv1alpha1.APIBinding {
	b := newApiBinding(name, path)
	if b.Annotations == nil {
		b.Annotations = make(map[string]string)
	}
	b.Annotations["kcp.io/cluster"] = cluster
	return b
}

func bindingWithApiExportCluster(name, path, exportCluster string) *kcpv1alpha1.APIBinding {
	b := newApiBinding(name, path)
	b.Status.APIExportClusterName = exportCluster
	return b
}

func mockAccountInfo(cl *mocks.MockClient, orgName, originCluster string) {
	cl.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
		if acc, ok := o.(*accountv1alpha1.AccountInfo); ok {
			acc.Spec.Organization.Name = orgName
			acc.Spec.Organization.OriginClusterId = originCluster
		}
		return nil
	}).Once()
}

func TestAuthorizationModelGeneration_Process(t *testing.T) {
	tests := []struct {
		name        string
		binding     *kcpv1alpha1.APIBinding
		mockSetup   func(*mocks.MockManager, *mocks.MockClient, *mocks.MockCluster, *mocks.MockClient)
		expectError bool
	}{
		{
			name:    "error on ClusterFromContext in Process",
			binding: newApiBinding("foo", "bar"),
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(nil, assert.AnError)
			},
			expectError: true,
		},
		{
			name:    "early return when accountInfo not found",
			binding: newApiBinding("foo", "bar"),
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(
					kerrors.NewNotFound(schema.GroupResource{Group: "account.platform-mesh.org", Resource: "accountinfos"}, "account"))
			},
		},
		{
			name:        "error on getting apiExport",
			binding:     newApiBinding("foo", "bar"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					if _, ok := o.(*accountv1alpha1.AccountInfo); ok {
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
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					if _, ok := o.(*accountv1alpha1.AccountInfo); ok {
						return nil
					}
					return nil
				}).Once()
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					if ae, ok := o.(*kcpv1alpha1.APIExport); ok {
						ae.Spec.LatestResourceSchemas = []string{"schema1"}
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					if rs, ok := o.(*kcpv1alpha1.APIResourceSchema); ok {
						rs.Spec.Group = "group"
						rs.Spec.Names.Plural = "things"
						rs.Spec.Names.Singular = "thing"
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(
					kerrors.NewNotFound(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "authorizationmodels"}, "things-org")).Once()
				kcpClient.EXPECT().Create(mock.Anything, mock.Anything).Return(assert.AnError).Once()
			},
		},
		{
			name:    "skip core exports in Process",
			binding: newApiBinding("core.platform-mesh.io", "root"),
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				mockAccountInfo(kcpClient, "org", "origin")
			},
		},
		{
			name:    "generate model in Process",
			binding: newApiBinding("foo", "bar"),
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				mockAccountInfo(kcpClient, "org", "origin")
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					if ae, ok := o.(*kcpv1alpha1.APIExport); ok {
						ae.Spec.LatestResourceSchemas = []string{"schema1"}
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					if rs, ok := o.(*kcpv1alpha1.APIResourceSchema); ok {
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
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				mockAccountInfo(kcpClient, "org", "origin")
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					if ae, ok := o.(*kcpv1alpha1.APIExport); ok {
						ae.Spec.LatestResourceSchemas = []string{"schema1"}
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					if rs, ok := o.(*kcpv1alpha1.APIResourceSchema); ok {
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
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
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
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				mockAccountInfo(kcpClient, "org", "origin")
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					if ae, ok := o.(*kcpv1alpha1.APIExport); ok {
						ae.Spec.LatestResourceSchemas = []string{"schema1"}
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
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				mockAccountInfo(kcpClient, "org", "origin")
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					if ae, ok := o.(*kcpv1alpha1.APIExport); ok {
						ae.Spec.LatestResourceSchemas = []string{"schema1"}
						return nil
					}
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					if rs, ok := o.(*kcpv1alpha1.APIResourceSchema); ok {
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
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError)
			},
		},
		{
			name:        "error on GetCluster for APIExport cluster in Process",
			binding:     bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, cluster *mocks.MockCluster, kcpClient *mocks.MockClient) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				cluster.EXPECT().GetClient().Return(kcpClient)
				mockAccountInfo(kcpClient, "org", "origin")
				manager.EXPECT().GetCluster(mock.Anything, "export-cluster").Return(nil, assert.AnError)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := mocks.NewMockManager(t)
			allClient := mocks.NewMockClient(t)
			cluster := mocks.NewMockCluster(t)
			kcpClient := mocks.NewMockClient(t)

			if test.mockSetup != nil {
				test.mockSetup(manager, allClient, cluster, kcpClient)
			}

			sub := subroutine.NewAuthorizationModelGenerationSubroutine(manager, allClient)
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
		binding     *kcpv1alpha1.APIBinding
		mockSetup   func(*mocks.MockManager, *mocks.MockClient, *kcpv1alpha1.APIBinding)
		expectError bool
	}{
		{
			name:    "bindings with non-matching export are skipped",
			binding: bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				bindingWsCluster := mocks.NewMockCluster(t)
				bindingWsClient := mocks.NewMockClient(t)
				apiExportCluster := mocks.NewMockCluster(t)
				apiExportClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				allClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					b1 := bindingWithCluster("foo", "bar", "cluster1")
					b2 := bindingWithCluster("other", "other", "cluster2")
					list.Items = []kcpv1alpha1.APIBinding{*b1, *b2}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "cluster1").Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "export-cluster").Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "foo"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					ae := o.(*kcpv1alpha1.APIExport)
					ae.Spec.LatestResourceSchemas = []string{"schema1"}
					return nil
				})
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "schema1"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					rs := o.(*kcpv1alpha1.APIResourceSchema)
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
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(nil, assert.AnError)
			},
		},
		{
			name:        "early return when accountInfo missing in Finalize",
			binding:     bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				allClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					b := binding.DeepCopy()
					if b.Annotations == nil {
						b.Annotations = make(map[string]string)
					}
					b.Annotations["kcp.io/cluster"] = "cluster1"
					list.Items = []kcpv1alpha1.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).Return(
					kerrors.NewNotFound(schema.GroupResource{Group: "account.platform-mesh.org", Resource: "accountinfos"}, "account"))
			},
		},
		{
			name:        "delete returns error in Finalize",
			binding:     bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				apiExportCluster := mocks.NewMockCluster(t)
				apiExportClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				allClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					b := binding.DeepCopy()
					if b.Annotations == nil {
						b.Annotations = make(map[string]string)
					}
					b.Annotations["kcp.io/cluster"] = "cluster1"
					list.Items = []kcpv1alpha1.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "cluster1").Return(bindingCluster, nil)
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "export-cluster").Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "foo"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					ae := o.(*kcpv1alpha1.APIExport)
					ae.Spec.LatestResourceSchemas = []string{"schema1"}
					return nil
				})
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "schema1"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					rs := o.(*kcpv1alpha1.APIResourceSchema)
					rs.Spec.Names.Plural = "foos"
					return nil
				})
				apiExportClient.EXPECT().Delete(mock.Anything, mock.Anything).Return(assert.AnError)
			},
		},
		{
			name:    "skip Finalize if other bindings exist",
			binding: bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				bindingWsCluster1 := mocks.NewMockCluster(t)
				bindingWsClient1 := mocks.NewMockClient(t)
				bindingWsCluster2 := mocks.NewMockCluster(t)
				bindingWsClient2 := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				allClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					b1 := bindingWithCluster("foo", "bar", "cluster1")
					b2 := bindingWithCluster("foo", "bar", "cluster2")
					list.Items = []kcpv1alpha1.APIBinding{*b1, *b2}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "cluster1").Return(bindingWsCluster1, nil)
				bindingWsCluster1.EXPECT().GetClient().Return(bindingWsClient1)
				bindingWsClient1.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "cluster2").Return(bindingWsCluster2, nil)
				bindingWsCluster2.EXPECT().GetClient().Return(bindingWsClient2)
				bindingWsClient2.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
			},
		},
		{
			name:    "delete model in Finalize if last binding",
			binding: bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				bindingWsCluster := mocks.NewMockCluster(t)
				bindingWsClient := mocks.NewMockClient(t)
				apiExportCluster := mocks.NewMockCluster(t)
				apiExportClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				allClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					b := binding.DeepCopy()
					if b.Annotations == nil {
						b.Annotations = make(map[string]string)
					}
					b.Annotations["kcp.io/cluster"] = "cluster1"
					list.Items = []kcpv1alpha1.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "cluster1").Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "export-cluster").Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "foo"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					ae := o.(*kcpv1alpha1.APIExport)
					ae.Spec.LatestResourceSchemas = []string{"schema1"}
					return nil
				})
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "schema1"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					rs := o.(*kcpv1alpha1.APIResourceSchema)
					rs.Spec.Names.Plural = "foos"
					return nil
				})
				apiExportClient.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil)
			},
		},
		{
			name:    "delete model in Finalize but model is not found",
			binding: bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				bindingWsCluster := mocks.NewMockCluster(t)
				bindingWsClient := mocks.NewMockClient(t)
				apiExportCluster := mocks.NewMockCluster(t)
				apiExportClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				allClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					b := binding.DeepCopy()
					if b.Annotations == nil {
						b.Annotations = make(map[string]string)
					}
					b.Annotations["kcp.io/cluster"] = "cluster1"
					list.Items = []kcpv1alpha1.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "cluster1").Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "export-cluster").Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "foo"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					ae := o.(*kcpv1alpha1.APIExport)
					ae.Spec.LatestResourceSchemas = []string{"schema1"}
					return nil
				})
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "schema1"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					rs := o.(*kcpv1alpha1.APIResourceSchema)
					rs.Spec.Names.Plural = "foos"
					return nil
				})
				apiExportClient.EXPECT().Delete(mock.Anything, mock.Anything).Return(
					kerrors.NewNotFound(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "authorizationmodels"}, "foos-org"))
			},
		},
		{
			name:        "error on List in Finalize",
			binding:     newApiBinding("foo", "bar"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient).Maybe()
				allClient.EXPECT().List(mock.Anything, mock.Anything).Return(assert.AnError)
			},
		},
		{
			name:        "error on getRelatedAuthorizationModels in Finalize",
			binding:     bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				allClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					b := binding.DeepCopy()
					if b.Annotations == nil {
						b.Annotations = make(map[string]string)
					}
					b.Annotations["kcp.io/cluster"] = "cluster1"
					list.Items = []kcpv1alpha1.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).Return(assert.AnError)
			},
		},
		{
			name:    "only bindings for same org are counted; delete called if only one, not called if none",
			binding: bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
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
				allClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					b1 := bindingWithCluster("foo", "bar", "cluster1")
					b2 := bindingWithCluster("foo", "bar", "cluster2")
					list.Items = []kcpv1alpha1.APIBinding{*b1, *b2}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "cluster1").Return(bindingWsCluster1, nil)
				bindingWsCluster1.EXPECT().GetClient().Return(bindingWsClient1)
				bindingWsClient1.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "cluster2").Return(bindingWsCluster2, nil)
				bindingWsCluster2.EXPECT().GetClient().Return(bindingWsClient2)
				bindingWsClient2.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).Return(
					kerrors.NewNotFound(schema.GroupResource{Group: "account.platform-mesh.org", Resource: "accountinfos"}, "account"))
				manager.EXPECT().GetCluster(mock.Anything, "export-cluster").Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "foo"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					ae := o.(*kcpv1alpha1.APIExport)
					ae.Spec.LatestResourceSchemas = []string{"schema1"}
					return nil
				})
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "schema1"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					rs := o.(*kcpv1alpha1.APIResourceSchema)
					rs.Spec.Names.Plural = "foos"
					return nil
				})
				apiExportClient.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil)
			},
		},
		{
			name:    "error on GetCluster for binding workspace in Finalize loop",
			binding: bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				allClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					b := bindingWithCluster("foo", "bar", "cluster1")
					list.Items = []kcpv1alpha1.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "cluster1").Return(nil, assert.AnError)
			},
			expectError: true,
		},
		{
			name:    "error on Get accountInfo in Finalize loop (not NotFound)",
			binding: bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				bindingWsCluster := mocks.NewMockCluster(t)
				bindingWsClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				allClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					b := bindingWithCluster("foo", "bar", "cluster1")
					list.Items = []kcpv1alpha1.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "cluster1").Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).Return(assert.AnError)
			},
			expectError: true,
		},
		{
			name:    "bindings with different org are skipped in Finalize",
			binding: bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				bindingWsCluster := mocks.NewMockCluster(t)
				bindingWsClient := mocks.NewMockClient(t)
				apiExportCluster := mocks.NewMockCluster(t)
				apiExportClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				allClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					b := bindingWithCluster("foo", "bar", "cluster1")
					list.Items = []kcpv1alpha1.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "cluster1").Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "different-org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "export-cluster").Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "foo"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					ae := o.(*kcpv1alpha1.APIExport)
					ae.Spec.LatestResourceSchemas = []string{"schema1"}
					return nil
				})
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "schema1"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					rs := o.(*kcpv1alpha1.APIResourceSchema)
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
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				bindingWsCluster := mocks.NewMockCluster(t)
				bindingWsClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				allClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					b := bindingWithCluster("foo", "bar", "cluster1")
					list.Items = []kcpv1alpha1.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "cluster1").Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "export-cluster").Return(nil, assert.AnError)
			},
		},
		{
			name:        "error on Get APIExport in Finalize",
			binding:     bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				bindingWsCluster := mocks.NewMockCluster(t)
				bindingWsClient := mocks.NewMockClient(t)
				apiExportCluster := mocks.NewMockCluster(t)
				apiExportClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				allClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					b := bindingWithCluster("foo", "bar", "cluster1")
					list.Items = []kcpv1alpha1.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "cluster1").Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "export-cluster").Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "foo"}, mock.Anything).Return(assert.AnError)
			},
		},
		{
			name:        "error on Get resource schema in Finalize",
			binding:     bindingWithApiExportCluster("foo", "bar", "export-cluster"),
			expectError: true,
			mockSetup: func(manager *mocks.MockManager, allClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				bindingCluster := mocks.NewMockCluster(t)
				bindingClient := mocks.NewMockClient(t)
				bindingWsCluster := mocks.NewMockCluster(t)
				bindingWsClient := mocks.NewMockClient(t)
				apiExportCluster := mocks.NewMockCluster(t)
				apiExportClient := mocks.NewMockClient(t)

				manager.EXPECT().ClusterFromContext(mock.Anything).Return(bindingCluster, nil)
				bindingCluster.EXPECT().GetClient().Return(bindingClient)
				allClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					b := bindingWithCluster("foo", "bar", "cluster1")
					list.Items = []kcpv1alpha1.APIBinding{*b}
					return nil
				})
				bindingClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "cluster1").Return(bindingWsCluster, nil)
				bindingWsCluster.EXPECT().GetClient().Return(bindingWsClient)
				bindingWsClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "account"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.Name = "org"
					acc.Spec.Organization.GeneratedClusterId = "org-id"
					return nil
				})
				manager.EXPECT().GetCluster(mock.Anything, "export-cluster").Return(apiExportCluster, nil)
				apiExportCluster.EXPECT().GetClient().Return(apiExportClient)
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "foo"}, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					ae := o.(*kcpv1alpha1.APIExport)
					ae.Spec.LatestResourceSchemas = []string{"schema1"}
					return nil
				})
				apiExportClient.EXPECT().Get(mock.Anything, types.NamespacedName{Name: "schema1"}, mock.Anything).Return(assert.AnError)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := mocks.NewMockManager(t)
			allClient := mocks.NewMockClient(t)

			if test.mockSetup != nil {
				test.mockSetup(manager, allClient, test.binding)
			}

			sub := subroutine.NewAuthorizationModelGenerationSubroutine(manager, allClient)
			_, err := sub.Finalize(context.Background(), test.binding)
			if test.expectError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestFinalizeAuthorizationModelGeneration(t *testing.T) {
	allClient := mocks.NewMockClient(t)
	finalizers := subroutine.NewAuthorizationModelGenerationSubroutine(nil, allClient).Finalizers(nil)
	assert.Equal(t, []string{"core.platform-mesh.io/apibinding-finalizer"}, finalizers)
}

func TestAuthorizationModelGenerationSubroutine_GetName(t *testing.T) {
	allClient := mocks.NewMockClient(t)
	sub := subroutine.NewAuthorizationModelGenerationSubroutine(nil, allClient)
	assert.Equal(t, "AuthorizationModelGeneration", sub.GetName())
}
