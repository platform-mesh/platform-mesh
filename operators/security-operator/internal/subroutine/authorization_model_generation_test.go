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

// Additional helpers for mocking
func mockAccountInfo(cl *mocks.MockClient, orgName, originCluster string) {
	cl.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
		acc := o.(*accountv1alpha1.AccountInfo)
		acc.Spec.Organization.Name = orgName
		acc.Spec.Organization.OriginClusterId = originCluster
		return nil
	}).Once()
}

func TestAuthorizationModelGeneration_Process(t *testing.T) {
	tests := []struct {
		name        string
		binding     *kcpv1alpha1.APIBinding
		mockSetup   func(*mocks.MockClient)
		expectError bool
	}{
		{
			name: "error on lcClientFunc for binding workspace client",
			binding: &kcpv1alpha1.APIBinding{
				Spec: kcpv1alpha1.APIBindingSpec{Reference: kcpv1alpha1.BindingReference{Export: &kcpv1alpha1.ExportBindingReference{Name: "foo", Path: "bar"}}},
			},
			mockSetup:   func(kcpClient *mocks.MockClient) {},
			expectError: true,
		},
		{
			name: "early return when accountInfo not found",
			binding: &kcpv1alpha1.APIBinding{
				Spec: kcpv1alpha1.APIBindingSpec{Reference: kcpv1alpha1.BindingReference{Export: &kcpv1alpha1.ExportBindingReference{Name: "foo", Path: "bar"}}},
			},
			mockSetup: func(kcpClient *mocks.MockClient) {
				// First lcClientFunc returns kcpClient; Get account returns NotFound
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(
					kerrors.NewNotFound(schema.GroupResource{Group: "account.platform-mesh.org", Resource: "accountinfos"}, "account"),
				)
			},
		},
		{
			name: "error on getting apiExport",
			binding: &kcpv1alpha1.APIBinding{
				Spec: kcpv1alpha1.APIBindingSpec{Reference: kcpv1alpha1.BindingReference{Export: &kcpv1alpha1.ExportBindingReference{Name: "foo", Path: "bar"}}},
			},
			mockSetup: func(kcpClient *mocks.MockClient) {
				// account info exists
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
				// apiExport Get fails
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError)
			},
			expectError: true,
		},
		{
			name: "error from CreateOrUpdate when creating model",
			binding: &kcpv1alpha1.APIBinding{
				Spec: kcpv1alpha1.APIBindingSpec{Reference: kcpv1alpha1.BindingReference{Export: &kcpv1alpha1.ExportBindingReference{Name: "foo", Path: "bar"}}},
			},
			mockSetup: func(kcpClient *mocks.MockClient) {
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					if _, ok := o.(*accountv1alpha1.AccountInfo); ok {
						return nil
					}
					return nil
				}).Once()
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
					kerrors.NewNotFound(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "authorizationmodels"}, "things-org"),
				).Once()
				kcpClient.EXPECT().Create(mock.Anything, mock.Anything).Return(assert.AnError).Once()
			},
			expectError: true,
		},
		{
			name: "skip core exports in Process",
			binding: &kcpv1alpha1.APIBinding{
				Spec: kcpv1alpha1.APIBindingSpec{
					Reference: kcpv1alpha1.BindingReference{
						Export: &kcpv1alpha1.ExportBindingReference{
							Name: "core.platform-mesh.io",
							Path: "root",
						},
					},
				},
			},
			mockSetup: func(kcpClient *mocks.MockClient) {
				mockAccountInfo(kcpClient, "org", "origin")
			},
		},
		{
			name: "generate model in Process",
			binding: &kcpv1alpha1.APIBinding{
				Spec: kcpv1alpha1.APIBindingSpec{
					Reference: kcpv1alpha1.BindingReference{
						Export: &kcpv1alpha1.ExportBindingReference{
							Name: "foo",
							Path: "bar",
						},
					},
				},
			},
			mockSetup: func(kcpClient *mocks.MockClient) {
				mockAccountInfo(kcpClient, "org", "origin")
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					apiExport := o.(*kcpv1alpha1.APIExport)
					apiExport.Spec.LatestResourceSchemas = []string{"schema1"}
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					rs := o.(*kcpv1alpha1.APIResourceSchema)
					rs.Spec.Group = "group"
					rs.Spec.Names.Plural = "foos"
					rs.Spec.Names.Singular = "foo"
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(nil)
				kcpClient.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Maybe()
				kcpClient.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Maybe()
			},
		},
		{
			name: "generate model in Process with namespaced scope",
			binding: &kcpv1alpha1.APIBinding{
				Spec: kcpv1alpha1.APIBindingSpec{
					Reference: kcpv1alpha1.BindingReference{
						Export: &kcpv1alpha1.ExportBindingReference{
							Name: "foo",
							Path: "bar",
						},
					},
				},
			},
			mockSetup: func(kcpClient *mocks.MockClient) {
				mockAccountInfo(kcpClient, "org", "origin")
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					apiExport := o.(*kcpv1alpha1.APIExport)
					apiExport.Spec.LatestResourceSchemas = []string{"schema1"}
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					rs := o.(*kcpv1alpha1.APIResourceSchema)
					rs.Spec.Group = "group"
					rs.Spec.Names.Plural = "foos"
					rs.Spec.Names.Singular = "foo"
					rs.Spec.Scope = apiextensionsv1.NamespaceScoped
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(nil)
				kcpClient.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Maybe()
				kcpClient.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Maybe()
			},
		},
		{
			name: "error on apiExportClient.Get in Process",
			binding: &kcpv1alpha1.APIBinding{
				Spec: kcpv1alpha1.APIBindingSpec{
					Reference: kcpv1alpha1.BindingReference{
						Export: &kcpv1alpha1.ExportBindingReference{
							Name: "foo",
							Path: "bar",
						},
					},
				},
			},
			mockSetup: func(kcpClient *mocks.MockClient) {
				mockAccountInfo(kcpClient, "org", "origin")
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError)
			},
			expectError: true,
		},
		{
			name: "error on apiExportClient.Get resource schema in Process",
			binding: &kcpv1alpha1.APIBinding{
				Spec: kcpv1alpha1.APIBindingSpec{
					Reference: kcpv1alpha1.BindingReference{
						Export: &kcpv1alpha1.ExportBindingReference{
							Name: "foo",
							Path: "bar",
						},
					},
				},
			},
			mockSetup: func(kcpClient *mocks.MockClient) {
				mockAccountInfo(kcpClient, "org", "origin")
				// First Get returns APIExport with one resource schema
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					apiExport := o.(*kcpv1alpha1.APIExport)
					apiExport.Spec.LatestResourceSchemas = []string{"schema1"}
					return nil
				}).Once()
				// Second Get returns error for resource schema
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError)
			},
			expectError: true,
		},
		{
			name: "generate model in Process with longestRelationName > 50",
			binding: &kcpv1alpha1.APIBinding{
				Spec: kcpv1alpha1.APIBindingSpec{
					Reference: kcpv1alpha1.BindingReference{
						Export: &kcpv1alpha1.ExportBindingReference{
							Name: "foo",
							Path: "bar",
						},
					},
				},
			},
			mockSetup: func(kcpClient *mocks.MockClient) {
				mockAccountInfo(kcpClient, "org", "origin")
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					apiExport := o.(*kcpv1alpha1.APIExport)
					apiExport.Spec.LatestResourceSchemas = []string{"schema1"}
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					rs := o.(*kcpv1alpha1.APIResourceSchema)
					rs.Spec.Group = "averyveryveryveryveryveryveryveryverylonggroup.platform-mesh.org"
					rs.Spec.Names.Plural = "plural"
					rs.Spec.Names.Singular = "singular"
					return nil
				}).Once()
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(nil)
				kcpClient.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Maybe()
				kcpClient.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Maybe()
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := mocks.NewMockManager(t)
			cluster := mocks.NewMockCluster(t)
			kcpClient := mocks.NewMockClient(t)

			if test.name == "error on lcClientFunc for binding workspace client" {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(nil, assert.AnError)
			} else {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil).Maybe()
				cluster.EXPECT().GetClient().Return(kcpClient).Maybe()
				if test.mockSetup != nil {
					test.mockSetup(kcpClient)
				}
			}
			sub := subroutine.NewAuthorizationModelGenerationSubroutine(manager)
			ctx := context.Background()
			_, err := sub.Process(ctx, test.binding)
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
		mockSetup   func(*mocks.MockClient, *kcpv1alpha1.APIBinding)
		expectError bool
	}{
		{
			name:    "bindings with non-matching export are skipped",
			binding: &kcpv1alpha1.APIBinding{Spec: kcpv1alpha1.APIBindingSpec{Reference: kcpv1alpha1.BindingReference{Export: &kcpv1alpha1.ExportBindingReference{Name: "foo", Path: "bar"}}}},
			mockSetup: func(kcpClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				kcpClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					other := binding.DeepCopy()
					other.Spec.Reference.Export.Name = "other"
					other.Spec.Reference.Export.Path = "other"
					list.Items = []kcpv1alpha1.APIBinding{*binding, *other}
					return nil
				})
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					if _, ok := o.(*accountv1alpha1.AccountInfo); ok {
						return nil
					}
					return nil
				})
				// With one matching binding remaining, delete should be called
				kcpClient.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil)
			},
		},
		{
			name:    "error on lcClientFunc for binding in Finalize",
			binding: &kcpv1alpha1.APIBinding{Spec: kcpv1alpha1.APIBindingSpec{Reference: kcpv1alpha1.BindingReference{Export: &kcpv1alpha1.ExportBindingReference{Name: "foo", Path: "bar"}}}},
			mockSetup: func(kcpClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				kcpClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					list.Items = []kcpv1alpha1.APIBinding{*binding}
					return nil
				})
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(nil) // toDeleteAccountInfo
			},
			expectError: true,
		},
		{
			name:    "early return when accountInfo missing in Finalize",
			binding: &kcpv1alpha1.APIBinding{Spec: kcpv1alpha1.APIBindingSpec{Reference: kcpv1alpha1.BindingReference{Export: &kcpv1alpha1.ExportBindingReference{Name: "foo", Path: "bar"}}}},
			mockSetup: func(kcpClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				kcpClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					list.Items = []kcpv1alpha1.APIBinding{*binding}
					return nil
				})
				// First Get: toDeleteAccountInfo OK; Second Get: binding workspace account info NotFound -> early return
				call := 0
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
						if _, ok := o.(*accountv1alpha1.AccountInfo); ok {
							if call == 0 {
								call++
								return nil
							}
							return kerrors.NewNotFound(schema.GroupResource{Group: "account.platform-mesh.org", Resource: "accountinfos"}, nn.Name)
						}
						return nil
					},
				).Times(2)
			},
		},
		{
			name:    "delete returns error in Finalize",
			binding: &kcpv1alpha1.APIBinding{Spec: kcpv1alpha1.APIBindingSpec{Reference: kcpv1alpha1.BindingReference{Export: &kcpv1alpha1.ExportBindingReference{Name: "foo", Path: "bar"}}}},
			mockSetup: func(kcpClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				kcpClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					list.Items = []kcpv1alpha1.APIBinding{}
					return nil
				})
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(nil)
				kcpClient.EXPECT().Delete(mock.Anything, mock.Anything).Return(assert.AnError)
			},
			expectError: true,
		},
		{
			name: "skip Finalize if other bindings exist",
			binding: &kcpv1alpha1.APIBinding{
				Spec: kcpv1alpha1.APIBindingSpec{
					Reference: kcpv1alpha1.BindingReference{
						Export: &kcpv1alpha1.ExportBindingReference{
							Name: "foo",
							Path: "bar",
						},
					},
				},
			},
			mockSetup: func(kcpClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				kcpClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					list.Items = []kcpv1alpha1.APIBinding{*binding, *binding}
					return nil
				})
				// Add this mock to avoid missing lcClientFunc call
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			},
		},
		{
			name: "delete model in Finalize if last binding",
			binding: &kcpv1alpha1.APIBinding{
				Spec: kcpv1alpha1.APIBindingSpec{
					Reference: kcpv1alpha1.BindingReference{
						Export: &kcpv1alpha1.ExportBindingReference{
							Name: "foo",
							Path: "bar",
						},
					},
				},
			},
			mockSetup: func(kcpClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				kcpClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					list.Items = []kcpv1alpha1.APIBinding{*binding}
					return nil
				})
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(nil)
				kcpClient.EXPECT().Delete(mock.Anything, mock.Anything).Return(nil)
			},
		},
		{
			name: "delete model in Finalize but model is not found",
			binding: &kcpv1alpha1.APIBinding{
				Spec: kcpv1alpha1.APIBindingSpec{
					Reference: kcpv1alpha1.BindingReference{
						Export: &kcpv1alpha1.ExportBindingReference{
							Name: "foo",
							Path: "bar",
						},
					},
				},
			},
			mockSetup: func(kcpClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				// List returns a single binding
				kcpClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					list.Items = []kcpv1alpha1.APIBinding{*binding}
					return nil
				})
				// Get returns account info
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.GeneratedClusterId = "org1"
					return nil
				})
				// Delete returns NotFound
				kcpClient.EXPECT().Delete(mock.Anything, mock.Anything).Return(
					kerrors.NewNotFound(schema.GroupResource{Group: "core.platform-mesh.io", Resource: "authorizationmodels"}, "foo-bar"),
				)
			},
		},
		{
			name: "error on List in Finalize",
			binding: &kcpv1alpha1.APIBinding{
				Spec: kcpv1alpha1.APIBindingSpec{
					Reference: kcpv1alpha1.BindingReference{
						Export: &kcpv1alpha1.ExportBindingReference{
							Name: "foo",
							Path: "bar",
						},
					},
				},
			},
			mockSetup: func(kcpClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				kcpClient.EXPECT().List(mock.Anything, mock.Anything).Return(assert.AnError)
			},
			expectError: true,
		},
		{
			name: "error on getRelatedAuthorizationModels in Finalize",
			binding: &kcpv1alpha1.APIBinding{
				Spec: kcpv1alpha1.APIBindingSpec{
					Reference: kcpv1alpha1.BindingReference{
						Export: &kcpv1alpha1.ExportBindingReference{
							Name: "foo",
							Path: "bar",
						},
					},
				},
			},
			mockSetup: func(kcpClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				// List returns a single binding, so Finalize will call Get next
				kcpClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					list.Items = []kcpv1alpha1.APIBinding{*binding}
					return nil
				})
				// Simulate error in getRelatedAuthorizationModels
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError)
			},
			expectError: true,
		},
		{
			name: "only bindings for same org are counted; delete called if only one, not called if none",
			binding: &kcpv1alpha1.APIBinding{
				Spec: kcpv1alpha1.APIBindingSpec{
					Reference: kcpv1alpha1.BindingReference{
						Export: &kcpv1alpha1.ExportBindingReference{
							Name: "foo",
							Path: "bar",
						},
					},
				},
			},
			mockSetup: func(kcpClient *mocks.MockClient, binding *kcpv1alpha1.APIBinding) {
				otherBinding := *binding.DeepCopy()
				// List returns two bindings: one for same org, one for different org
				kcpClient.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
					list := ol.(*kcpv1alpha1.APIBindingList)
					list.Items = []kcpv1alpha1.APIBinding{*binding, otherBinding}
					return nil
				})
				// toDeleteAccountInfo (for bindingToDelete) - org1
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.GeneratedClusterId = "org1"
					return nil
				})
				// accountInfo for first binding (same org)
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
					acc := o.(*accountv1alpha1.AccountInfo)
					acc.Spec.Organization.GeneratedClusterId = "org1"
					return nil
				})
				// accountInfo for second binding (different org) - simulate NotFound
				kcpClient.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).Return(
					kerrors.NewNotFound(schema.GroupResource{Group: "account.platform-mesh.org", Resource: "accountinfos"}, "account"),
				)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := mocks.NewMockManager(t)
			cluster := mocks.NewMockCluster(t)
			kcpClient := mocks.NewMockClient(t)

			if test.name == "error on lcClientFunc for binding in Finalize" {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(nil, assert.AnError)
			} else {
				manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
				manager.EXPECT().GetCluster(mock.Anything, mock.Anything).Return(cluster, nil).Maybe()
				cluster.EXPECT().GetClient().Return(kcpClient).Maybe()
				if test.mockSetup != nil {
					test.mockSetup(kcpClient, test.binding)
				}
			}
			sub := subroutine.NewAuthorizationModelGenerationSubroutine(manager)
			ctx := context.Background()
			_, err := sub.Finalize(ctx, test.binding)
			if test.expectError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestFinalizeAuthorizationModelGeneration(t *testing.T) {
	finalizers := subroutine.NewAuthorizationModelGenerationSubroutine(nil).Finalizers(nil)
	assert.Equal(t, []string{}, finalizers)
}
