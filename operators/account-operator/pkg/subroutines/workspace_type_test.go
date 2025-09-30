package subroutines

import (
	"context"
	"fmt"
	"testing"
	"time"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	platformmeshcontext "github.com/platform-mesh/golang-commons/context"
	"github.com/platform-mesh/golang-commons/logger"
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

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
)

type WorkspaceTypeSubroutineTestSuite struct {
	suite.Suite

	// Tested Object(s)
	testObj *WorkspaceTypeSubroutine

	// Mocks
	orgsClientMock *mocks.Client

	context context.Context
	log     *logger.Logger
}

func (suite *WorkspaceTypeSubroutineTestSuite) SetupTest() {
	// Setup Mocks
	suite.orgsClientMock = new(mocks.Client)

	// Initialize Tested Object(s)
	suite.testObj = &WorkspaceTypeSubroutine{orgsClient: suite.orgsClientMock}

	utilruntime.Must(corev1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(corev1.AddToScheme(scheme.Scheme))
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(scheme.Scheme))

	cfg := config.OperatorConfig{}
	var err error
	suite.log, err = logger.New(logger.DefaultConfig())
	suite.Require().NoError(err)
	suite.context, _, _ = platformmeshcontext.StartContext(suite.log, cfg, 1*time.Minute)
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestGetName_OK() {
	// When
	result := suite.testObj.GetName()

	// Then
	suite.Equal("WorkspaceTypeSubroutine", result)
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestGetFinalizerName() {
	// When
	finalizers := suite.testObj.Finalizers()

	// Then
	suite.Contains(finalizers, "workspacetype.core.platform-mesh.io/finalizer")
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestProcess_OK_OrgAccount() {
	// Given
	testAccount := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-org",
		},
		Spec: corev1alpha1.AccountSpec{
			Type:        corev1alpha1.AccountTypeOrg,
			DisplayName: "Test Organization",
		},
	}

	// Mock CreateOrUpdate calls for both WorkspaceTypes
	suite.mockCreateOrUpdateWorkspaceType("test-org-org", nil)
	suite.mockCreateOrUpdateWorkspaceType("test-org-acc", nil)

	// When
	result, err := suite.testObj.Process(suite.context, testAccount)

	// Then
	suite.False(result.Requeue)
	suite.Zero(result.RequeueAfter)
	suite.Nil(err)
	suite.orgsClientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestProcess_Skip_NonOrgAccount() {
	// Given
	testAccount := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-account",
		},
		Spec: corev1alpha1.AccountSpec{
			Type:        corev1alpha1.AccountTypeAccount,
			DisplayName: "Test Account",
		},
	}

	// When
	result, err := suite.testObj.Process(suite.context, testAccount)

	// Then
	suite.False(result.Requeue)
	suite.Zero(result.RequeueAfter)
	suite.Nil(err)
	// No expectations on the mock since it should be skipped
	suite.orgsClientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestProcess_Error_OrgWorkspaceTypeCreation() {
	// Given
	testAccount := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-org",
		},
		Spec: corev1alpha1.AccountSpec{
			Type:        corev1alpha1.AccountTypeOrg,
			DisplayName: "Test Organization",
		},
	}

	// Mock CreateOrUpdate call that fails for org workspace type
	suite.mockCreateOrUpdateWorkspaceType("test-org-org", kerrors.NewInternalError(fmt.Errorf("creation failed")))

	// When
	result, err := suite.testObj.Process(suite.context, testAccount)

	// Then
	suite.False(result.Requeue)
	suite.Zero(result.RequeueAfter)
	suite.NotNil(err)
	suite.True(err.Retry())
	suite.True(err.Sentry())
	suite.orgsClientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestProcess_Error_AccountWorkspaceTypeCreation() {
	// Given
	testAccount := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-org",
		},
		Spec: corev1alpha1.AccountSpec{
			Type:        corev1alpha1.AccountTypeOrg,
			DisplayName: "Test Organization",
		},
	}

	// Mock successful CreateOrUpdate call for org workspace type
	suite.mockCreateOrUpdateWorkspaceType("test-org-org", nil)
	// Mock failed CreateOrUpdate call for account workspace type
	suite.mockCreateOrUpdateWorkspaceType("test-org-acc", kerrors.NewInternalError(fmt.Errorf("creation failed")))

	// When
	result, err := suite.testObj.Process(suite.context, testAccount)

	// Then
	suite.False(result.Requeue)
	suite.Zero(result.RequeueAfter)
	suite.NotNil(err)
	suite.True(err.Retry())
	suite.True(err.Sentry())
	suite.orgsClientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestFinalize_OK_OrgAccount() {
	// Given
	testAccount := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-org",
		},
		Spec: corev1alpha1.AccountSpec{
			Type:        corev1alpha1.AccountTypeOrg,
			DisplayName: "Test Organization",
		},
	}

	// Mock Delete calls for both WorkspaceTypes
	suite.mockDeleteWorkspaceType("test-org-org", nil)
	suite.mockDeleteWorkspaceType("test-org-acc", nil)

	// When
	result, err := suite.testObj.Finalize(suite.context, testAccount)

	// Then
	suite.False(result.Requeue)
	suite.Zero(result.RequeueAfter)
	suite.Nil(err)
	suite.orgsClientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestFinalize_OK_NotFound() {
	// Given
	testAccount := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-org",
		},
		Spec: corev1alpha1.AccountSpec{
			Type:        corev1alpha1.AccountTypeOrg,
			DisplayName: "Test Organization",
		},
	}

	// Mock Delete calls that return NotFound errors (should be ignored)
	suite.mockDeleteWorkspaceType("test-org-org", kerrors.NewNotFound(schema.GroupResource{}, "test-org-org"))
	suite.mockDeleteWorkspaceType("test-org-acc", kerrors.NewNotFound(schema.GroupResource{}, "test-org-acc"))

	// When
	result, err := suite.testObj.Finalize(suite.context, testAccount)

	// Then
	suite.False(result.Requeue)
	suite.Zero(result.RequeueAfter)
	suite.Nil(err)
	suite.orgsClientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestFinalize_Skip_NonOrgAccount() {
	// Given
	testAccount := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-account",
		},
		Spec: corev1alpha1.AccountSpec{
			Type:        corev1alpha1.AccountTypeAccount,
			DisplayName: "Test Account",
		},
	}

	// When
	result, err := suite.testObj.Finalize(suite.context, testAccount)

	// Then
	suite.False(result.Requeue)
	suite.Zero(result.RequeueAfter)
	suite.Nil(err)
	// No expectations on the mock since it should be skipped
	suite.orgsClientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestFinalize_Error_OrgWorkspaceTypeDeletion() {
	// Given
	testAccount := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-org",
		},
		Spec: corev1alpha1.AccountSpec{
			Type:        corev1alpha1.AccountTypeOrg,
			DisplayName: "Test Organization",
		},
	}

	// Mock Delete call that fails for org workspace type
	suite.mockDeleteWorkspaceType("test-org-org", kerrors.NewInternalError(fmt.Errorf("deletion failed")))

	// When
	result, err := suite.testObj.Finalize(suite.context, testAccount)

	// Then
	suite.False(result.Requeue)
	suite.Zero(result.RequeueAfter)
	suite.NotNil(err)
	suite.True(err.Retry())
	suite.True(err.Sentry())
	suite.orgsClientMock.AssertExpectations(suite.T())
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestFinalize_Error_AccountWorkspaceTypeDeletion() {
	// Given
	testAccount := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-org",
		},
		Spec: corev1alpha1.AccountSpec{
			Type:        corev1alpha1.AccountTypeOrg,
			DisplayName: "Test Organization",
		},
	}

	// Mock successful Delete call for org workspace type
	suite.mockDeleteWorkspaceType("test-org-org", nil)
	// Mock failed Delete call for account workspace type
	suite.mockDeleteWorkspaceType("test-org-acc", kerrors.NewInternalError(fmt.Errorf("deletion failed")))

	// When
	result, err := suite.testObj.Finalize(suite.context, testAccount)

	// Then
	suite.False(result.Requeue)
	suite.Zero(result.RequeueAfter)
	suite.NotNil(err)
	suite.True(err.Retry())
	suite.True(err.Sentry())
	suite.orgsClientMock.AssertExpectations(suite.T())
}

// Test helper functions

func (suite *WorkspaceTypeSubroutineTestSuite) mockCreateOrUpdateWorkspaceType(name string, returnError error) {
	if returnError != nil {
		// Mock Get call to simulate existing object for CreateOrUpdate
		suite.orgsClientMock.EXPECT().
			Get(mock.Anything, types.NamespacedName{Name: name}, mock.AnythingOfType("*v1alpha1.WorkspaceType")).
			Return(returnError)
	} else {
		// Mock successful Get call (object not found) followed by Create
		suite.orgsClientMock.EXPECT().
			Get(mock.Anything, types.NamespacedName{Name: name}, mock.AnythingOfType("*v1alpha1.WorkspaceType")).
			Return(kerrors.NewNotFound(schema.GroupResource{}, name))

		suite.orgsClientMock.EXPECT().
			Create(mock.Anything, mock.AnythingOfType("*v1alpha1.WorkspaceType")).
			Run(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) {
				wst := obj.(*kcptenancyv1alpha.WorkspaceType)
				wst.Name = name
			}).
			Return(nil)
	}
}

func (suite *WorkspaceTypeSubroutineTestSuite) mockDeleteWorkspaceType(name string, returnError error) {
	suite.orgsClientMock.EXPECT().
		Delete(mock.Anything, mock.MatchedBy(func(obj client.Object) bool {
			wst, ok := obj.(*kcptenancyv1alpha.WorkspaceType)
			return ok && wst.Name == name
		})).
		Return(returnError)
}

// Test workspace type generation functions

func (suite *WorkspaceTypeSubroutineTestSuite) TestGenerateOrganizationWorkspaceTypeName() {
	// Given
	orgName := "test-org"

	// When
	result := generateOrganizationWorkspaceTypeName(orgName)

	// Then
	suite.Equal("test-org-org", result)
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestGenerateAccountWorkspaceTypeName() {
	// Given
	orgName := "test-org"

	// When
	result := generateAccountWorkspaceTypeName(orgName)

	// Then
	suite.Equal("test-org-acc", result)
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestGenerateOrgWorkspaceType() {
	// Given
	testAccount := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "test-org"},
		Spec: corev1alpha1.AccountSpec{
			Type:        corev1alpha1.AccountTypeOrg,
			DisplayName: "Test Organization",
		},
	}
	orgWorkspaceTypeName := "test-org-org"
	accountWorkspaceTypeName := "test-org-acc"

	// When
	result := generateOrgWorkspaceType(testAccount, orgWorkspaceTypeName, accountWorkspaceTypeName)

	// Then
	suite.Equal(orgWorkspaceTypeName, result.Name)
	suite.Equal(kcptenancyv1alpha.WorkspaceTypeName("org"), result.Spec.Extend.With[0].Name)
	suite.Equal("root", result.Spec.Extend.With[0].Path)
	suite.Equal(kcptenancyv1alpha.WorkspaceTypeName(accountWorkspaceTypeName), result.Spec.DefaultChildWorkspaceType.Name)
	suite.Equal("root:orgs", result.Spec.DefaultChildWorkspaceType.Path)

	// Verify LimitAllowedParents
	suite.Equal(kcptenancyv1alpha.WorkspaceTypeName("orgs"), result.Spec.LimitAllowedParents.Types[0].Name)
	suite.Equal("root", result.Spec.LimitAllowedParents.Types[0].Path)

	// Verify LimitAllowedChildren
	suite.Equal(kcptenancyv1alpha.WorkspaceTypeName(accountWorkspaceTypeName), result.Spec.LimitAllowedChildren.Types[0].Name)
	suite.Equal("root:orgs", result.Spec.LimitAllowedChildren.Types[0].Path)
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestGenerateAccountWorkspaceType() {
	// Given
	testAccount := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "test-org"},
		Spec: corev1alpha1.AccountSpec{
			Type:        corev1alpha1.AccountTypeOrg,
			DisplayName: "Test Organization",
		},
	}
	orgWorkspaceTypeName := "test-org-org"
	accountWorkspaceTypeName := "test-org-acc"

	// When
	result := generateAccountWorkspaceType(testAccount, orgWorkspaceTypeName, accountWorkspaceTypeName)

	// Then
	suite.Equal(accountWorkspaceTypeName, result.Name)
	suite.Equal(kcptenancyv1alpha.WorkspaceTypeName("account"), result.Spec.Extend.With[0].Name)
	suite.Equal("root", result.Spec.Extend.With[0].Path)
	suite.Equal(kcptenancyv1alpha.WorkspaceTypeName(accountWorkspaceTypeName), result.Spec.DefaultChildWorkspaceType.Name)
	suite.Equal("root:orgs", result.Spec.DefaultChildWorkspaceType.Path)

	// Verify LimitAllowedParents
	suite.Len(result.Spec.LimitAllowedParents.Types, 2)
	suite.Equal(kcptenancyv1alpha.WorkspaceTypeName(orgWorkspaceTypeName), result.Spec.LimitAllowedParents.Types[0].Name)
	suite.Equal("root:orgs", result.Spec.LimitAllowedParents.Types[0].Path)
	suite.Equal(kcptenancyv1alpha.WorkspaceTypeName(accountWorkspaceTypeName), result.Spec.LimitAllowedParents.Types[1].Name)
	suite.Equal("root:orgs", result.Spec.LimitAllowedParents.Types[1].Path)

	// Verify LimitAllowedChildren
	suite.Equal(kcptenancyv1alpha.WorkspaceTypeName(accountWorkspaceTypeName), result.Spec.LimitAllowedChildren.Types[0].Name)
	suite.Equal("root:orgs", result.Spec.LimitAllowedChildren.Types[0].Path)
}

func TestWorkspaceTypeSubroutineTestSuite(t *testing.T) {
	suite.Run(t, new(WorkspaceTypeSubroutineTestSuite))
}
