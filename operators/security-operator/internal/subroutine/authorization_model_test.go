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

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	language "github.com/openfga/language/pkg/go/transformer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"

	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/logger/testlogger"
	"go.platform-mesh.io/security-operator/internal/subroutine"
	"go.platform-mesh.io/security-operator/internal/subroutine/mocks"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	"github.com/kcp-dev/logicalcluster/v3"
)

var coreModule = `
module core

type user

type role
  relations
	define assignee: [user,user:*]

type core_platform-mesh_io_account
	relations
		define owner: [role#assignee]
		define member: [role#assignee]
`

var extensionModel = `
module extension

extend type role
  relations
	define extensions: assignee
`

var mergedModule = `model
  schema 1.2

type core_platform-mesh_io_account
  relations
    define member: [role#assignee]
    define owner: [role#assignee]
    define create_core_namespaces: owner
    define list_core_namespaces: member
    define watch_core_namespaces: member

type role
  relations
    define assignee: [user, user:*]
    define extensions: assignee

type user

type core_namespace
  relations
    define delete: member
    define get: member
    define get_iam_roles: member
    define get_iam_users: member
    define manage_iam_roles: owner
    define member: [role#assignee] or owner or member from parent
    define owner: [role#assignee] or owner from parent
    define parent: [core_platform-mesh_io_account]
    define patch: member
    define update: member
    define watch: member
`

func TestAuthorizationModelGetName(t *testing.T) {
	subroutine := subroutine.NewAuthorizationModelSubroutine(nil, nil, nil, nil, nil)
	assert.Equal(t, "AuthorizationModel", subroutine.GetName())
}

func TestAuthorizationModelProcess(t *testing.T) {
	tests := []struct {
		name           string
		store          *pmcorev1alpha1.Store
		fgaMocks       func(*mocks.MockOpenFGAServiceClient)
		kcpHelperMocks func(*mocks.MockLister)
		mgrMocks       func(*mocks.MockManager)
		discoveryMocks func(*mocks.MockDiscoveryInterface)
		expectError    bool
	}{
		{
			name: "should reconcile and apply the merged model",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Spec: pmcorev1alpha1.StoreSpec{
					CoreModule: coreModule,
				},
				Status: pmcorev1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			kcpHelperMocks: func(kcpHelper *mocks.MockLister) {
				kcpHelper.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
						am := ol.(*pmcorev1alpha1.AuthorizationModelList)
						am.Items = []pmcorev1alpha1.AuthorizationModel{
							{
								Spec: pmcorev1alpha1.AuthorizationModelSpec{
									Model: extensionModel,
									StoreRef: pmcorev1alpha1.WorkspaceStoreRef{
										Name:    "store",
										Cluster: "path",
									},
								},
							},
						}
						return nil
					},
				).Once()
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().WriteAuthorizationModel(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, wamr *openfgav1.WriteAuthorizationModelRequest, co ...grpc.CallOption) (*openfgav1.WriteAuthorizationModelResponse, error) {

						m := openfgav1.AuthorizationModel{
							SchemaVersion:   wamr.SchemaVersion,
							TypeDefinitions: wamr.TypeDefinitions,
							Conditions:      wamr.Conditions,
							Id:              "id",
						}

						raw, err := protojson.Marshal(&m)
						assert.NoError(t, err)

						dsl, err := language.TransformJSONStringToDSL(string(raw))
						assert.NoError(t, err)

						assert.Equal(t, mergedModule, *dsl)

						return &openfgav1.WriteAuthorizationModelResponse{AuthorizationModelId: "id"}, nil
					},
				)
			},
		},
		{
			name: "should reconcile and not patch the model in case they are equal",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Spec: pmcorev1alpha1.StoreSpec{
					CoreModule: coreModule,
				},
				Status: pmcorev1alpha1.StoreStatus{
					StoreID:              "id",
					AuthorizationModelID: "id",
				},
			},
			kcpHelperMocks: func(kcpHelper *mocks.MockLister) {
				kcpHelper.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
						am := ol.(*pmcorev1alpha1.AuthorizationModelList)
						am.Items = []pmcorev1alpha1.AuthorizationModel{
							{
								Spec: pmcorev1alpha1.AuthorizationModelSpec{
									Model: extensionModel,
									StoreRef: pmcorev1alpha1.WorkspaceStoreRef{
										Name:    "store",
										Cluster: "path",
									},
								},
							},
						}
						return nil
					},
				).Once()
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				// Simulate the same module transformation process as the actual code
				moduleFiles := []language.ModuleFile{
					{
						Name:     "/store.fga",
						Contents: coreModule,
					},
					{
						Name:     "/.fga",
						Contents: extensionModel,
					},
					{
						Name: "internal_core_types_namespaces.fga",
						Contents: `module namespaces

extend type core_platform-mesh_io_account
	relations
		define create_core_namespaces: owner
		define list_core_namespaces: member
		define watch_core_namespaces: member

type core_namespace
	relations
		define parent: [core_platform-mesh_io_account]
		define member: [role#assignee] or owner or member from parent
		define owner: [role#assignee] or owner from parent

		define get: member
		define update: member
		define delete: member
		define patch: member
		define watch: member

		define manage_iam_roles: owner
		define get_iam_roles: member
		define get_iam_users: member
`,
					},
				}

				model, err := language.TransformModuleFilesToModel(moduleFiles, "1.2")
				assert.NoError(t, err)

				fga.EXPECT().ReadAuthorizationModel(mock.Anything, mock.Anything).Return(&openfgav1.ReadAuthorizationModelResponse{
					AuthorizationModel: model,
				}, nil)
			},
		},
		{
			name: "should stop reconciliation if the authorization model is not found",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Spec: pmcorev1alpha1.StoreSpec{
					CoreModule: coreModule,
				},
				Status: pmcorev1alpha1.StoreStatus{
					StoreID:              "id",
					AuthorizationModelID: "id",
				},
			},
			kcpHelperMocks: func(kcpHelper *mocks.MockLister) {
				kcpHelper.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
						am := ol.(*pmcorev1alpha1.AuthorizationModelList)
						am.Items = []pmcorev1alpha1.AuthorizationModel{
							{
								Spec: pmcorev1alpha1.AuthorizationModelSpec{
									Model: extensionModel,
									StoreRef: pmcorev1alpha1.WorkspaceStoreRef{
										Name:    "store",
										Cluster: "path",
									},
								},
							},
						}
						return nil
					},
				).Once()
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().ReadAuthorizationModel(mock.Anything, mock.Anything).Return(nil, errors.New("error"))
			},
			expectError: true,
		},
		{
			name: "should stop reconciliation for invalid model",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Status: pmcorev1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			kcpHelperMocks: func(kcpHelper *mocks.MockLister) {
				kcpHelper.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
						am := ol.(*pmcorev1alpha1.AuthorizationModelList)
						am.Items = []pmcorev1alpha1.AuthorizationModel{
							{
								Spec: pmcorev1alpha1.AuthorizationModelSpec{
									Model: extensionModel,
									StoreRef: pmcorev1alpha1.WorkspaceStoreRef{
										Name:    "store",
										Cluster: "path",
									},
								},
							},
						}
						return nil
					},
				).Once()
			},
			expectError: true,
		},
		{
			name: "should stop reconciliation for failing write",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Spec: pmcorev1alpha1.StoreSpec{
					CoreModule: coreModule,
				},
				Status: pmcorev1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			kcpHelperMocks: func(kcpHelper *mocks.MockLister) {
				kcpHelper.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
						am := ol.(*pmcorev1alpha1.AuthorizationModelList)
						am.Items = []pmcorev1alpha1.AuthorizationModel{
							{
								Spec: pmcorev1alpha1.AuthorizationModelSpec{
									Model: extensionModel,
									StoreRef: pmcorev1alpha1.WorkspaceStoreRef{
										Name:    "store",
										Cluster: "path",
									},
								},
							},
						}
						return nil
					},
				).Once()
			},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().WriteAuthorizationModel(mock.Anything, mock.Anything).Return(nil, errors.New("error"))
			},
			expectError: true,
		},
		{
			name: "non-matching authorization model is filtered",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{Name: "orgs"},
				Spec:       pmcorev1alpha1.StoreSpec{CoreModule: coreModule},
				Status:     pmcorev1alpha1.StoreStatus{StoreID: "id"},
			},
			kcpHelperMocks: func(kcpHelper *mocks.MockLister) {
				kcpHelper.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
						am := ol.(*pmcorev1alpha1.AuthorizationModelList)
						am.Items = []pmcorev1alpha1.AuthorizationModel{
							{
								Spec: pmcorev1alpha1.AuthorizationModelSpec{
									Model: extensionModel,
									StoreRef: pmcorev1alpha1.WorkspaceStoreRef{
										Name:    "different-store",
										Cluster: "path",
									},
								},
							},
						}
						return nil
					},
				).Once()
			},
			discoveryMocks: func(d *mocks.MockDiscoveryInterface) {},
			fgaMocks: func(fga *mocks.MockOpenFGAServiceClient) {
				fga.EXPECT().WriteAuthorizationModel(mock.Anything, mock.Anything).Return(
					&openfgav1.WriteAuthorizationModelResponse{AuthorizationModelId: "new-id"}, nil,
				)
			},
			expectError: false,
		},
		{
			name: "discovery returns namespaced and grouped resources",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{Name: "store"},
				Spec:       pmcorev1alpha1.StoreSpec{CoreModule: coreModule},
				Status:     pmcorev1alpha1.StoreStatus{StoreID: "id"},
			},
			kcpHelperMocks: func(kcpHelper *mocks.MockLister) {
				kcpHelper.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
						return nil
					},
				).Once()
			},
			discoveryMocks: func(d *mocks.MockDiscoveryInterface) {
				d.EXPECT().ServerResourcesForGroupVersion(mock.Anything).Return(&metav1.APIResourceList{
					GroupVersion: "apps/v1",
					APIResources: []metav1.APIResource{
						{Name: "deployments", SingularName: "deployment", Namespaced: true, Group: ""},
						{Name: "daemonsets", SingularName: "daemonset", Namespaced: false, Group: "apps"},
					},
				}, nil).Once()
				d.EXPECT().ServerResourcesForGroupVersion(mock.Anything).Return(&metav1.APIResourceList{}, nil).Maybe()
			},
			expectError: true,
		},
		{
			name: "core discoverAndRender fails",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{Name: "store"},
				Spec:       pmcorev1alpha1.StoreSpec{CoreModule: coreModule},
				Status:     pmcorev1alpha1.StoreStatus{StoreID: "id"},
			},
			kcpHelperMocks: func(kcpHelper *mocks.MockLister) {
				kcpHelper.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
						return nil
					},
				).Once()
			},
			discoveryMocks: func(d *mocks.MockDiscoveryInterface) {
				d.EXPECT().ServerResourcesForGroupVersion(mock.Anything).Return(nil, errors.New("discovery unavailable")).Once()
			},
			expectError: true,
		},
		{
			name: "privileged discoverAndRender fails",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{Name: "store"},
				Spec:       pmcorev1alpha1.StoreSpec{CoreModule: coreModule},
				Status:     pmcorev1alpha1.StoreStatus{StoreID: "id"},
			},
			kcpHelperMocks: func(kcpHelper *mocks.MockLister) {
				kcpHelper.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
						return nil
					},
				).Once()
			},
			discoveryMocks: func(d *mocks.MockDiscoveryInterface) {
				// 5 core groupVersions succeed, then 1 privileged groupVersion fails
				d.EXPECT().ServerResourcesForGroupVersion(mock.Anything).Return(&metav1.APIResourceList{}, nil).Times(5)
				d.EXPECT().ServerResourcesForGroupVersion(mock.Anything).Return(nil, errors.New("privileged discovery unavailable")).Once()
			},
			expectError: true,
		},
		{
			name: "ParseGroupVersion fails for returned resource list",
			store: &pmcorev1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{Name: "store"},
				Spec:       pmcorev1alpha1.StoreSpec{CoreModule: coreModule},
				Status:     pmcorev1alpha1.StoreStatus{StoreID: "id"},
			},
			kcpHelperMocks: func(kcpHelper *mocks.MockLister) {
				kcpHelper.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, ol ctrlruntimeclient.ObjectList, lo ...ctrlruntimeclient.ListOption) error {
						return nil
					},
				).Once()
			},
			discoveryMocks: func(d *mocks.MockDiscoveryInterface) {
				// GroupVersion with more than one slash is invalid for schema.ParseGroupVersion
				d.EXPECT().ServerResourcesForGroupVersion(mock.Anything).Return(&metav1.APIResourceList{
					GroupVersion: "a/b/c/d",
					APIResources: []metav1.APIResource{{Name: "pods", SingularName: "pod"}},
				}, nil).Once()
			},
			expectError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			fga := mocks.NewMockOpenFGAServiceClient(t)
			if test.fgaMocks != nil {
				test.fgaMocks(fga)
			}

			manager := mocks.NewMockManager(t)
			if test.mgrMocks != nil {
				test.mgrMocks(manager)
			}

			cluster := mocks.NewMockCluster(t)
			client := mocks.NewMockClient(t)

			kcpHelper := mocks.NewMockLister(t)
			if test.kcpHelperMocks != nil {
				test.kcpHelperMocks(kcpHelper)
			}

			manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Maybe()
			cluster.EXPECT().GetClient().Return(client).Maybe()

			logger := testlogger.New()

			ctrlManager := mocks.NewMockCTRLManager(t)
			manager.EXPECT().GetLocalManager().Return(ctrlManager).Maybe()
			ctrlManager.EXPECT().GetConfig().Return(&rest.Config{}).Maybe()

			discoveryMock := mocks.NewMockDiscoveryInterface(t)
			if test.discoveryMocks != nil {
				test.discoveryMocks(discoveryMock)
			} else {
				discoveryMock.EXPECT().ServerResourcesForGroupVersion(mock.Anything).Return(&metav1.APIResourceList{
					APIResources: []metav1.APIResource{
						{
							Name:         "namespaces",
							SingularName: "namespace",
							Namespaced:   false,
							Group:        "",
						},
					},
				}, nil).Once().Maybe()
				discoveryMock.EXPECT().ServerResourcesForGroupVersion(mock.Anything).Return(&metav1.APIResourceList{}, nil).Maybe()
			}

			subroutine := subroutine.NewAuthorizationModelSubroutine(fga, manager, kcpHelper, func(cfg *rest.Config) discovery.DiscoveryInterface { return discoveryMock }, logger.Logger)
			ctx := mccontext.WithCluster(context.Background(), multicluster.ClusterName(logicalcluster.Name("path").String()))

			_, err := subroutine.Process(ctx, test.store)
			if test.expectError {
				assert.Error(t, err)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}
