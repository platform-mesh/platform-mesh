package subroutines_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	kcpcorev1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	platformmeshcontext "github.com/platform-mesh/golang-commons/context"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/kontext"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/pkg/subroutines"
	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
)

const defaultExpectedTestNamespace = "account-test"

type WorkspaceSubroutineTestSuite struct {
	suite.Suite

	// Tested Object(s)
	testObj *subroutines.WorkspaceSubroutine

	// Mocks
	clientMock *mocks.Client

	context context.Context
	log     *logger.Logger
}

func (suite *WorkspaceSubroutineTestSuite) SetupTest() {
	// Setup Mocks
	suite.clientMock = new(mocks.Client)

	// Initialize Tested Object(s)
	suite.testObj = subroutines.NewWorkspaceSubroutine(suite.clientMock)

	utilruntime.Must(corev1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(corev1.AddToScheme(scheme.Scheme))

	cfg := config.OperatorConfig{}
	var err error
	suite.log, err = logger.New(logger.DefaultConfig())
	suite.Require().NoError(err)
	suite.context, _, _ = platformmeshcontext.StartContext(suite.log, cfg, 1*time.Minute)
}

func (suite *WorkspaceSubroutineTestSuite) TestGetName_OK() {
	// When
	result := suite.testObj.GetName()

	// Then
	suite.Equal(subroutines.WorkspaceSubroutineName, result)
}

func (suite *WorkspaceSubroutineTestSuite) TestGetFinalizerName() {
	// When
	finalizers := suite.testObj.Finalizers()

	// Then
	suite.Contains(finalizers, subroutines.WorkspaceSubroutineFinalizer)
}

func (suite *WorkspaceSubroutineTestSuite) TestFinalize_OK_Workspace_NotExisting() {
	// Given
	testAccount := &corev1alpha1.Account{}
	mockGetWorkspaceCallNotFound(suite)
	ctx := kontext.WithCluster(suite.context, "some-cluster-id")
	// When
	res, err := suite.testObj.Finalize(ctx, testAccount)

	// Then
	suite.False(res.Requeue)
	suite.Assert().Zero(res.RequeueAfter)
	suite.Nil(err)
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestFinalize_Error_No_Cluster() {
	// Given
	testAccount := &corev1alpha1.Account{}

	ctx := suite.context
	// When
	assert.Panics(suite.T(), func() {
		suite.testObj.Finalize(ctx, testAccount)
	})

	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestFinalize_OK_Workspace_ExistingButInDeletion() {
	// Given
	testAccount := &corev1alpha1.Account{}
	mockGetWorkspaceByNameInDeletion(suite)
	ctx := kontext.WithCluster(suite.context, "some-cluster-id")

	// When
	res, err := suite.testObj.Finalize(ctx, testAccount)

	// Then
	suite.Assert().NotZero(res.RequeueAfter)
	suite.Nil(err)
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestFinalize_OK_Workspace_Existing() {
	// Given
	testAccount := &corev1alpha1.Account{}
	mockGetWorkspaceByName(suite.clientMock, kcpcorev1alpha1.LogicalClusterPhaseReady, "https://example.com/")
	mockDeleteWorkspaceCall(suite)
	ctx := context.Background()
	ctx = kontext.WithCluster(ctx, "some-cluster-id")

	// When
	res, err := suite.testObj.Finalize(ctx, testAccount)

	// Then
	suite.Assert().NotZero(res.RequeueAfter)
	suite.Nil(err)
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestFinalize_Error_On_Deletion() {
	// Given
	testAccount := &corev1alpha1.Account{}
	mockGetWorkspaceByName(suite.clientMock, kcpcorev1alpha1.LogicalClusterPhaseReady, "https://example.com/")
	mockDeleteWorkspaceCallFailed(suite)
	ctx := kontext.WithCluster(suite.context, "some-cluster-id")
	// When
	_, err := suite.testObj.Finalize(ctx, testAccount)

	// Then
	suite.Require().NotNil(err)
	suite.Error(err.Err())

	suite.True(err.Sentry())
	suite.True(err.Retry())
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestFinalize_Error_On_Get() {
	// Given
	testAccount := &corev1alpha1.Account{}
	mockGetWorkspaceFailed(suite)
	ctx := kontext.WithCluster(suite.context, "some-cluster-id")
	// When
	_, err := suite.testObj.Finalize(ctx, testAccount)

	// Then
	suite.Require().NotNil(err)
	suite.Error(err.Err())

	suite.True(err.Sentry())
	suite.True(err.Retry())
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestProcessing_OK() {
	// Given
	testAccount := &corev1alpha1.Account{}
	suite.clientMock.On("Scheme").Return(scheme.Scheme)
	mockGetWorkspaceCallNotFound(suite)
	mockNewWorkspaceCreateCall(suite, defaultExpectedTestNamespace)

	// When
	_, err := suite.testObj.Process(suite.context, testAccount)

	// Then
	suite.Nil(err)
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestProcessing_Error_On_Get() {
	// Given
	testAccount := &corev1alpha1.Account{}
	mockGetWorkspaceFailed(suite)

	// When
	_, err := suite.testObj.Process(suite.context, testAccount)

	// Then
	suite.Require().NotNil(err)
	suite.Error(err.Err())
	suite.True(err.Sentry())
	suite.True(err.Retry())
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceSubroutineTestSuite) TestProcessing_CreateError() {
	// Given
	testAccount := &corev1alpha1.Account{}
	suite.clientMock.On("Scheme").Return(scheme.Scheme)
	mockGetWorkspaceCallNotFound(suite)
	suite.clientMock.EXPECT().
		Create(mock.Anything, mock.Anything).
		Return(kerrors.NewBadRequest(""))

	// When
	_, err := suite.testObj.Process(suite.context, testAccount)

	// Then
	suite.NotNil(err)
	suite.True(err.Retry())
	suite.True(err.Sentry())
	suite.clientMock.AssertExpectations(suite.T())
}

func TestWorkspaceSubroutineTestSuite(t *testing.T) {
	suite.Run(t, new(WorkspaceSubroutineTestSuite))
}

//nolint:golint,unparam
func mockNewWorkspaceCreateCall(suite *WorkspaceSubroutineTestSuite, name string) *mocks.Client_Create_Call {
	return suite.clientMock.EXPECT().
		Create(mock.Anything, mock.Anything).
		Run(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) {
			actual, _ := obj.(*kcptenancyv1alpha.Workspace)
			actual.Name = name
		}).
		Return(nil)
}

//nolint:golint,unparam
func mockGetWorkspaceCallNotFound(suite *WorkspaceSubroutineTestSuite) *mocks.Client_Get_Call {
	return suite.clientMock.EXPECT().
		Get(mock.Anything, mock.Anything, mock.Anything).
		Return(kerrors.NewNotFound(schema.GroupResource{}, ""))
}

func mockGetWorkspaceFailed(suite *WorkspaceSubroutineTestSuite) *mocks.Client_Get_Call {
	return suite.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{}, mock.Anything).
		Return(kerrors.NewInternalError(fmt.Errorf("failed")))
}

func mockGetWorkspaceByNameInDeletion(suite *WorkspaceSubroutineTestSuite) *mocks.Client_Get_Call {
	return suite.clientMock.EXPECT().
		Get(mock.Anything, types.NamespacedName{}, mock.Anything).
		Run(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) {
			actual, _ := obj.(*kcptenancyv1alpha.Workspace)
			actual.Name = key.Name
			actual.DeletionTimestamp = &metav1.Time{}
		}).
		Return(nil)
}

//nolint:golint,unparam
func mockDeleteWorkspaceCall(suite *WorkspaceSubroutineTestSuite) *mocks.Client_Delete_Call {
	return suite.clientMock.EXPECT().
		Delete(mock.Anything, mock.Anything).
		Return(nil)
}

func mockDeleteWorkspaceCallFailed(suite *WorkspaceSubroutineTestSuite) *mocks.Client_Delete_Call {
	return suite.clientMock.EXPECT().
		Delete(mock.Anything, mock.Anything).
		Return(kerrors.NewInternalError(fmt.Errorf("failed")))
}
