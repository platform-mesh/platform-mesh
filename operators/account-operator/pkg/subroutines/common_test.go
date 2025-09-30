package subroutines

import (
	"context"
	"fmt"
	"testing"

	kcpcorev1alpha1 "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
)

// Test suite for common.go functions
type CommonTestSuite struct {
	suite.Suite
	clientMock *mocks.Client
	log        *logger.Logger
	ctx        context.Context
}

func (suite *CommonTestSuite) SetupTest() {
	suite.clientMock = new(mocks.Client)
	var err error
	suite.log, err = logger.New(logger.DefaultConfig())
	suite.Require().NoError(err)
	suite.ctx = context.Background()
}

func TestCommonTestSuite(t *testing.T) {
	suite.Run(t, new(CommonTestSuite))
}

// Test generateAccountWorkspaceTypeName function
func TestGenerateAccountWorkspaceTypeName(t *testing.T) {
	tests := []struct {
		name         string
		orgName      string
		expectedName string
	}{
		{
			name:         "normal organization name",
			orgName:      "test-org",
			expectedName: "test-org-acc",
		},
		{
			name:         "empty organization name",
			orgName:      "",
			expectedName: "-acc",
		},
		{
			name:         "organization name with special characters",
			orgName:      "org-with-dashes",
			expectedName: "org-with-dashes-acc",
		},
		{
			name:         "single character organization name",
			orgName:      "a",
			expectedName: "a-acc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateAccountWorkspaceTypeName(tt.orgName)
			assert.Equal(t, tt.expectedName, result)
		})
	}
}

// Test generateOrganizationWorkspaceTypeName function
func TestGenerateOrganizationWorkspaceTypeName(t *testing.T) {
	tests := []struct {
		name         string
		orgName      string
		expectedName string
	}{
		{
			name:         "normal organization name",
			orgName:      "test-org",
			expectedName: "test-org-org",
		},
		{
			name:         "empty organization name",
			orgName:      "",
			expectedName: "-org",
		},
		{
			name:         "organization name with special characters",
			orgName:      "org-with-dashes",
			expectedName: "org-with-dashes-org",
		},
		{
			name:         "single character organization name",
			orgName:      "a",
			expectedName: "a-org",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateOrganizationWorkspaceTypeName(tt.orgName)
			assert.Equal(t, tt.expectedName, result)
		})
	}
}

// Test retrieveWorkspace function
func (suite *CommonTestSuite) TestRetrieveWorkspace_Success() {
	// Given
	account := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "test-account"},
	}

	expectedWorkspace := &kcptenancyv1alpha.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-account"},
		Spec: kcptenancyv1alpha.WorkspaceSpec{
			Cluster: "test-cluster",
		},
	}

	suite.clientMock.EXPECT().
		Get(suite.ctx, client.ObjectKey{Name: "test-account"}, mock.AnythingOfType("*v1alpha1.Workspace")).
		Run(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) {
			workspace := obj.(*kcptenancyv1alpha.Workspace)
			workspace.Name = expectedWorkspace.Name
			workspace.Spec = expectedWorkspace.Spec
		}).
		Return(nil)

	// When
	result, err := retrieveWorkspace(suite.ctx, account, suite.clientMock, suite.log)

	// Then
	suite.NoError(err)
	suite.NotNil(result)
	suite.Equal("test-account", result.Name)
	suite.Equal("test-cluster", result.Spec.Cluster)
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *CommonTestSuite) TestRetrieveWorkspace_NotFound() {
	// Given
	account := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "nonexistent-account"},
	}

	suite.clientMock.EXPECT().
		Get(suite.ctx, client.ObjectKey{Name: "nonexistent-account"}, mock.AnythingOfType("*v1alpha1.Workspace")).
		Return(kerrors.NewNotFound(schema.GroupResource{Group: "tenancy.kcp.io", Resource: "workspaces"}, "nonexistent-account"))

	// When
	result, err := retrieveWorkspace(suite.ctx, account, suite.clientMock, suite.log)

	// Then
	suite.Error(err)
	suite.Nil(result)
	suite.Contains(err.Error(), "workspace does not exist")
	suite.clientMock.AssertExpectations(suite.T())
}

func (suite *CommonTestSuite) TestRetrieveWorkspace_GetError() {
	// Given
	account := &corev1alpha1.Account{
		ObjectMeta: metav1.ObjectMeta{Name: "test-account"},
	}

	suite.clientMock.EXPECT().
		Get(suite.ctx, client.ObjectKey{Name: "test-account"}, mock.AnythingOfType("*v1alpha1.Workspace")).
		Return(kerrors.NewInternalError(fmt.Errorf("internal server error")))

	// When
	result, err := retrieveWorkspace(suite.ctx, account, suite.clientMock, suite.log)

	// Then
	suite.Error(err)
	suite.Nil(result)
	suite.Contains(err.Error(), "workspace does not exist")
	suite.clientMock.AssertExpectations(suite.T())
}

// Test createOrganizationRestConfig function
func TestCreateOrganizationRestConfig(t *testing.T) {
	tests := []struct {
		name         string
		inputHost    string
		expectedHost string
	}{
		{
			name:         "https host",
			inputHost:    "https://api.example.com",
			expectedHost: "https://api.example.com/clusters/root:orgs",
		},
		{
			name:         "http host",
			inputHost:    "http://localhost:8080",
			expectedHost: "http://localhost:8080/clusters/root:orgs",
		},
		{
			name:         "host with path",
			inputHost:    "https://api.example.com/path",
			expectedHost: "https://api.example.com/clusters/root:orgs",
		},
		{
			name:         "host with port",
			inputHost:    "https://api.example.com:443",
			expectedHost: "https://api.example.com:443/clusters/root:orgs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			originalConfig := &rest.Config{
				Host:        tt.inputHost,
				BearerToken: "test-token",
				TLSClientConfig: rest.TLSClientConfig{
					Insecure: true,
				},
			}

			// When
			result, err := createOrganizationRestConfig(originalConfig)

			// Then
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedHost, result.Host)
			assert.Equal(t, originalConfig.BearerToken, result.BearerToken)
			assert.Equal(t, originalConfig.TLSClientConfig, result.TLSClientConfig)
			assert.Nil(t, result.WrapTransport, "WrapTransport should be nil to avoid redirect issues")

			// Verify original config is not modified
			assert.Equal(t, tt.inputHost, originalConfig.Host)
		})
	}
}

func TestCreateOrganizationRestConfig_InvalidURL(t *testing.T) {
	// Given
	invalidConfig := &rest.Config{
		Host: "://invalid-url",
	}

	// When
	result, err := createOrganizationRestConfig(invalidConfig)

	// Then
	assert.Error(t, err)
	assert.Nil(t, result)
}

// Mock helper functions (existing)
func mockGetWorkspaceByName(clientMock *mocks.Client, ready kcpcorev1alpha1.LogicalClusterPhaseType, path string) *mocks.Client_Get_Call {
	return clientMock.EXPECT().
		Get(mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.Workspace")).
		Run(func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) {
			wsPath := ""
			if path != "" {
				wsPath = "https://example.com/" + path
			}
			actual, _ := obj.(*kcptenancyv1alpha.Workspace)
			actual.Name = key.Name
			actual.Spec = kcptenancyv1alpha.WorkspaceSpec{
				Cluster: "some-cluster-id-" + key.Name,
				URL:     wsPath,
			}
			actual.Status.Phase = ready
		}).
		Return(nil)
}
