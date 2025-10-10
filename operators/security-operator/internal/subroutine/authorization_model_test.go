package subroutine_test

import (
	"context"
	"testing"

	kcpcorev1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	"github.com/kcp-dev/logicalcluster/v3"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	language "github.com/openfga/language/pkg/go/transformer"
	"github.com/platform-mesh/golang-commons/errors"
	"github.com/platform-mesh/security-operator/api/v1alpha1"
	"github.com/platform-mesh/security-operator/internal/subroutine"
	"github.com/platform-mesh/security-operator/internal/subroutine/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mccontext "sigs.k8s.io/multicluster-runtime/pkg/context"
)

var coreModule = `
module core

type user

type role
  relations
	define assignee: [user,user:*]
`

var extensionModel = `
module extension

extend type role
  relations
	define extensions: assignee
`

var mergedModule = `model
  schema 1.2

type role
  relations
    define assignee: [user, user:*]
    define extensions: assignee

type user
`

func TestAuthorizationModelGetName(t *testing.T) {
	subroutine := subroutine.NewAuthorizationModelSubroutine(nil, nil)
	assert.Equal(t, "AuthorizationModel", subroutine.GetName())
}

func TestAuthorizationModelFinalizers(t *testing.T) {
	subroutine := subroutine.NewAuthorizationModelSubroutine(nil, nil)
	assert.Equal(t, []string(nil), subroutine.Finalizers(nil))
}

func TestAuthorizationModelFinalize(t *testing.T) {
	subroutine := subroutine.NewAuthorizationModelSubroutine(nil, nil)
	_, err := subroutine.Finalize(context.Background(), nil)
	assert.Nil(t, err)
}

func mockLogicalClusterGet(k8s *mocks.MockClient) {
	k8s.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, nn types.NamespacedName, o client.Object, opts ...client.GetOption) error {
			lc := o.(*kcpcorev1alpha1.LogicalCluster)
			lc.Annotations = map[string]string{
				"kcp.io/cluster": "path",
			}

			return nil
		},
	).Once()
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
				model, err := language.TransformDSLToProto(mergedModule)
				assert.NoError(t, err)

				fga.EXPECT().ReadAuthorizationModel(mock.Anything, mock.Anything).Return(&openfgav1.ReadAuthorizationModelResponse{
					AuthorizationModel: model,
				}, nil)
			},
		},
		{
			name: "should stop reconciliation if no resources are found",
			store: &v1alpha1.Store{
				ObjectMeta: metav1.ObjectMeta{
					Name: "store",
				},
			},
			k8sMocks: func(k8s *mocks.MockClient) {
				k8s.EXPECT().List(mock.Anything, mock.Anything).Return(errors.New("error"))
			},
			expectError: true,
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

			manager.EXPECT().ClusterFromContext(mock.Anything).Return(cluster, nil)
			cluster.EXPECT().GetClient().Return(client)

			subroutine := subroutine.NewAuthorizationModelSubroutine(fga, manager)
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
