package subroutine_test

import (
	"context"
	"testing"

	"github.com/kcp-dev/logicalcluster/v3"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	language "github.com/openfga/language/pkg/go/transformer"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/golang-commons/logger/testlogger"
	"github.com/platform-mesh/security-operator/api/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/subroutine"
	"github.com/platform-mesh/security-operator/internal/subroutine/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
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

func TestAuthorizationModelFinalizers(t *testing.T) {
	subroutine := subroutine.NewAuthorizationModelSubroutine(nil, nil, nil, nil, nil)
	assert.Equal(t, []string(nil), subroutine.Finalizers(nil))
}

func TestAuthorizationModelFinalize(t *testing.T) {
	subroutine := subroutine.NewAuthorizationModelSubroutine(nil, nil, nil, nil, nil)
	_, err := subroutine.Finalize(t.Context(), nil)
	assert.Nil(t, err)
}

func TestAuthorizationModelProcess(t *testing.T) {
	tests := []struct {
		name        string
		store       *v1alpha1.Store
		fgaMocks    func(*mocks.MockOpenFGAServiceClient)
		k8sMocks    func(*mocks.MockClient)
		mgrMocks    func(*mocks.MockManager)
		expectError bool
	}{
		{
			name: "should reconcile and apply the merged model",
			store: &v1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Spec: v1alpha1.StoreSpec{
					CoreModule: coreModule,
				},
				Status: v1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			k8sMocks: func(k8s *mocks.MockClient) {
				k8s.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
						am := ol.(*v1alpha1.AuthorizationModelList)
						am.Items = []v1alpha1.AuthorizationModel{
							{
								Spec: v1alpha1.AuthorizationModelSpec{
									Model: extensionModel,
									StoreRef: v1alpha1.WorkspaceStoreRef{
										Name: "store",
										Path: "path",
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
			store: &v1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Spec: v1alpha1.StoreSpec{
					CoreModule: coreModule,
				},
				Status: v1alpha1.StoreStatus{
					StoreID:              "id",
					AuthorizationModelID: "id",
				},
			},
			k8sMocks: func(k8s *mocks.MockClient) {
				k8s.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
						am := ol.(*v1alpha1.AuthorizationModelList)
						am.Items = []v1alpha1.AuthorizationModel{
							{
								Spec: v1alpha1.AuthorizationModelSpec{
									Model: extensionModel,
									StoreRef: v1alpha1.WorkspaceStoreRef{
										Name: "store",
										Path: "path",
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
			store: &v1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Spec: v1alpha1.StoreSpec{
					CoreModule: coreModule,
				},
				Status: v1alpha1.StoreStatus{
					StoreID:              "id",
					AuthorizationModelID: "id",
				},
			},
			k8sMocks: func(k8s *mocks.MockClient) {
				k8s.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
						am := ol.(*v1alpha1.AuthorizationModelList)
						am.Items = []v1alpha1.AuthorizationModel{
							{
								Spec: v1alpha1.AuthorizationModelSpec{
									Model: extensionModel,
									StoreRef: v1alpha1.WorkspaceStoreRef{
										Name: "store",
										Path: "path",
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
			store: &v1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Status: v1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			k8sMocks: func(k8s *mocks.MockClient) {
				k8s.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
						am := ol.(*v1alpha1.AuthorizationModelList)
						am.Items = []v1alpha1.AuthorizationModel{
							{
								Spec: v1alpha1.AuthorizationModelSpec{
									Model: extensionModel,
									StoreRef: v1alpha1.WorkspaceStoreRef{
										Name: "store",
										Path: "path",
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
			store: &v1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
				Spec: v1alpha1.StoreSpec{
					CoreModule: coreModule,
				},
				Status: v1alpha1.StoreStatus{
					StoreID: "id",
				},
			},
			k8sMocks: func(k8s *mocks.MockClient) {
				k8s.EXPECT().List(mock.Anything, mock.Anything).RunAndReturn(
					func(ctx context.Context, ol client.ObjectList, lo ...client.ListOption) error {
						am := ol.(*v1alpha1.AuthorizationModelList)
						am.Items = []v1alpha1.AuthorizationModel{
							{
								Spec: v1alpha1.AuthorizationModelSpec{
									Model: extensionModel,
									StoreRef: v1alpha1.WorkspaceStoreRef{
										Name: "store",
										Path: "path",
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

			if test.k8sMocks != nil {
				test.k8sMocks(client)
			}

			manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil).Maybe()
			cluster.EXPECT().GetClient().Return(client).Maybe()

			logger := testlogger.New()

			ctrlManager := mocks.NewCTRLManager(t)
			manager.EXPECT().GetLocalManager().Return(ctrlManager).Maybe()
			ctrlManager.EXPECT().GetConfig().Return(&rest.Config{}).Maybe()

			discoveryMock := mocks.NewMockDiscoveryInterface(t)
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

			subroutine := subroutine.NewAuthorizationModelSubroutine(fga, manager, client, func(cfg *rest.Config) discovery.DiscoveryInterface { return discoveryMock }, logger.Logger)
			ctx := mccontext.WithCluster(context.Background(), string(logicalcluster.Name("path")))

			_, err := subroutine.Process(ctx, test.store)
			if test.expectError {
				assert.Error(t, err.Err())
			} else {
				assert.Nil(t, err)
			}
		})
	}
}
