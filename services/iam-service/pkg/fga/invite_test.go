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

package fga

import (
	"context"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	pmcorev1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	appcontext "go.platform-mesh.io/iam-service/pkg/context"
	fgamocks "go.platform-mesh.io/iam-service/pkg/fga/mocks"
	"go.platform-mesh.io/iam-service/pkg/graph"
)

func TestEmailToLabelValue(t *testing.T) {
	tests := []struct {
		name  string
		email string
	}{
		{
			name:  "standard email",
			email: "user@example.com",
		},
		{
			name:  "email with plus",
			email: "user+test@example.com",
		},
		{
			name:  "email with dots",
			email: "first.last@example.com",
		},
		{
			name:  "long email",
			email: "very.long.email.address.with.many.dots@subdomain.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := emailToLabelValue(tt.email)

			// Verify it meets Kubernetes label requirements
			assert.LessOrEqual(t, len(result), 63, "Label value must be 63 characters or less")
			assert.Regexp(t, "^[a-z0-9]+$", result, "Label value must contain only lowercase alphanumeric characters")

			// Verify consistency - same email always produces same hash
			result2 := emailToLabelValue(tt.email)
			assert.Equal(t, result, result2, "Same email should produce same hash")

			// Verify different emails produce different hashes
			differentEmail := "different" + tt.email
			differentResult := emailToLabelValue(differentEmail)
			assert.NotEqual(t, result, differentResult, "Different emails should produce different hashes")
		})
	}
}

func TestService_AssignRolesToUsers_WithInvites_UserExists(t *testing.T) {
	service, client := createTestService(t)

	// Create mocks for workspace client factory and IDM user checker
	mockWsFactory := fgamocks.NewClientFactory(t)
	mockIDMChecker := fgamocks.NewIDMUserChecker(t)

	// Set the mocks on the service
	service.wsClientFactory = mockWsFactory
	service.idmChecker = mockIDMChecker

	ctx := context.Background()
	ctx = appcontext.SetKCPContext(ctx, appcontext.KCPContext{
		IDMTenant:        "test-tenant",
		OrganizationName: "test-org",
	})
	log, _ := logger.New(logger.DefaultConfig())
	ctx = logger.SetLoggerInContext(ctx, log)

	rCtx := graph.ResourceContext{
		Group: "core.platform-mesh.io",
		Kind:  "Account",
		Resource: &graph.Resource{
			Name:      "test-account",
			Namespace: ptr.To("default"),
		},
		AccountPath: "root:org:test-account",
	}

	ai := &pmcorev1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account"},
		Spec: pmcorev1alpha1.AccountInfoSpec{
			Account: pmcorev1alpha1.AccountLocation{
				GeneratedClusterId: "cluster-123",
			},
		},
	}

	invites := []*graph.InviteInput{
		{
			Email: "newuser@example.com",
			Roles: []string{"member"},
		},
	}

	storeID := "store-123"

	// Mock ListStores call for StoreHelper
	listStoresResponse := &openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{
			{
				Id:   storeID,
				Name: "test-org",
			},
		},
	}
	client.EXPECT().ListStores(mock.Anything, mock.Anything).Return(listStoresResponse, nil)

	// Mock IDM check - user exists, so no invite should be created
	mockIDMChecker.EXPECT().UserByMail(mock.Anything, "newuser@example.com").Return(&graph.User{
		UserID: "newuser@example.com",
		Email:  "newuser@example.com",
	}, nil).Once()

	// Mock Write calls for role assignment (2 writes per role: assignee + role tuple)
	client.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
		return req.StoreId == storeID &&
			len(req.Writes.TupleKeys) == 1
	})).Return(&openfgav1.WriteResponse{}, nil).Times(2)

	// Set cluster ID in context
	ctx = appcontext.SetClusterId(ctx, ai.Spec.Account.GeneratedClusterId)

	result, err := service.AssignRolesToUsers(ctx, rCtx, nil, invites)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Success)
	assert.Equal(t, 2, result.AssignedCount)
	assert.Empty(t, result.Errors)
}

func TestService_AssignRolesToUsers_WithInvites_UserDoesNotExist(t *testing.T) {
	service, client := createTestService(t)

	// Create mocks for workspace client factory and IDM user checker
	mockWsFactory := fgamocks.NewClientFactory(t)
	mockIDMChecker := fgamocks.NewIDMUserChecker(t)

	// Create scheme with security operator API
	scheme := runtime.NewScheme()
	err := pmcorev1alpha1.AddToScheme(scheme)
	require.NoError(t, err)

	mockWsClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	// Set the mocks on the service
	service.wsClientFactory = mockWsFactory
	service.idmChecker = mockIDMChecker

	ctx := context.Background()
	ctx = appcontext.SetKCPContext(ctx, appcontext.KCPContext{
		IDMTenant:        "test-tenant",
		OrganizationName: "test-org",
	})
	log, _ := logger.New(logger.DefaultConfig())
	ctx = logger.SetLoggerInContext(ctx, log)

	rCtx := graph.ResourceContext{
		Group: "core.platform-mesh.io",
		Kind:  "Account",
		Resource: &graph.Resource{
			Name:      "test-account",
			Namespace: ptr.To("default"),
		},
		AccountPath: "root:org:test-account",
	}

	ai := &pmcorev1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account"},
		Spec: pmcorev1alpha1.AccountInfoSpec{
			Account: pmcorev1alpha1.AccountLocation{
				GeneratedClusterId: "cluster-123",
			},
		},
	}

	invites := []*graph.InviteInput{
		{
			Email: "newuser@example.com",
			Roles: []string{"member"},
		},
	}

	storeID := "store-123"

	// Mock ListStores call for StoreHelper
	listStoresResponse := &openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{
			{
				Id:   storeID,
				Name: "test-org",
			},
		},
	}
	client.EXPECT().ListStores(mock.Anything, mock.Anything).Return(listStoresResponse, nil)

	// Mock IDM check - user doesn't exist (returns nil user, nil error)
	mockIDMChecker.EXPECT().UserByMail(mock.Anything, "newuser@example.com").Return(
		nil, nil).Once()

	// Mock workspace client creation (path is appended with resource name for Account)
	mockWsFactory.EXPECT().New(mock.Anything, multicluster.ClusterName("root:org:test-account:test-account")).Return(mockWsClient, nil).Once()

	// Mock Write calls for role assignment (2 writes per role: assignee + role tuple)
	client.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
		return req.StoreId == storeID &&
			len(req.Writes.TupleKeys) == 1
	})).Return(&openfgav1.WriteResponse{}, nil).Times(2)

	// Set cluster ID in context
	ctx = appcontext.SetClusterId(ctx, ai.Spec.Account.GeneratedClusterId)

	result, err := service.AssignRolesToUsers(ctx, rCtx, nil, invites)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Success)
	assert.Equal(t, 2, result.AssignedCount)
	assert.Empty(t, result.Errors)
}

func TestService_AssignRolesToUsers_WithInvites_InvalidRole(t *testing.T) {
	service, client := createTestService(t)

	// Create mocks for workspace client factory and IDM user checker
	mockWsFactory := fgamocks.NewClientFactory(t)
	mockIDMChecker := fgamocks.NewIDMUserChecker(t)

	// Set the mocks on the service
	service.wsClientFactory = mockWsFactory
	service.idmChecker = mockIDMChecker

	ctx := context.Background()
	ctx = appcontext.SetKCPContext(ctx, appcontext.KCPContext{
		IDMTenant:        "test-tenant",
		OrganizationName: "test-org",
	})
	log, _ := logger.New(logger.DefaultConfig())
	ctx = logger.SetLoggerInContext(ctx, log)

	rCtx := graph.ResourceContext{
		Group: "core.platform-mesh.io",
		Kind:  "Account",
		Resource: &graph.Resource{
			Name:      "test-account",
			Namespace: ptr.To("default"),
		},
		AccountPath: "root:org:test-account",
	}

	ai := &pmcorev1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account"},
		Spec: pmcorev1alpha1.AccountInfoSpec{
			Account: pmcorev1alpha1.AccountLocation{
				GeneratedClusterId: "cluster-123",
			},
		},
	}

	invites := []*graph.InviteInput{
		{
			Email: "newuser@example.com",
			Roles: []string{"admin"}, // Invalid role for Account
		},
	}

	storeID := "store-123"

	// Mock ListStores call for StoreHelper
	listStoresResponse := &openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{
			{
				Id:   storeID,
				Name: "test-org",
			},
		},
	}
	client.EXPECT().ListStores(mock.Anything, mock.Anything).Return(listStoresResponse, nil)

	// Mock IDM check - user exists
	mockIDMChecker.EXPECT().UserByMail(mock.Anything, "newuser@example.com").Return(&graph.User{
		UserID: "newuser@example.com",
		Email:  "newuser@example.com",
	}, nil).Once()

	// Set cluster ID in context
	ctx = appcontext.SetClusterId(ctx, ai.Spec.Account.GeneratedClusterId)

	result, err := service.AssignRolesToUsers(ctx, rCtx, nil, invites)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Success)
	assert.Equal(t, 0, result.AssignedCount)
	assert.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Errors[0], "admin")
	assert.Contains(t, result.Errors[0], "not allowed")
}

func TestService_AssignRolesToUsers_WithBothChangesAndInvites(t *testing.T) {
	service, client := createTestService(t)

	// Create mocks for workspace client factory and IDM user checker
	mockWsFactory := fgamocks.NewClientFactory(t)
	mockIDMChecker := fgamocks.NewIDMUserChecker(t)

	// Set the mocks on the service
	service.wsClientFactory = mockWsFactory
	service.idmChecker = mockIDMChecker

	ctx := context.Background()
	ctx = appcontext.SetKCPContext(ctx, appcontext.KCPContext{
		IDMTenant:        "test-tenant",
		OrganizationName: "test-org",
	})
	log, _ := logger.New(logger.DefaultConfig())
	ctx = logger.SetLoggerInContext(ctx, log)

	rCtx := graph.ResourceContext{
		Group: "core.platform-mesh.io",
		Kind:  "Account",
		Resource: &graph.Resource{
			Name:      "test-account",
			Namespace: ptr.To("default"),
		},
		AccountPath: "root:org:test-account",
	}

	ai := &pmcorev1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account"},
		Spec: pmcorev1alpha1.AccountInfoSpec{
			Account: pmcorev1alpha1.AccountLocation{
				GeneratedClusterId: "cluster-123",
			},
		},
	}

	changes := []*graph.UserRoleChange{
		{
			UserID: "existinguser@example.com",
			Roles:  []string{"owner"},
		},
	}

	invites := []*graph.InviteInput{
		{
			Email: "newuser@example.com",
			Roles: []string{"member"},
		},
	}

	storeID := "store-123"

	// Mock ListStores call for StoreHelper
	listStoresResponse := &openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{
			{
				Id:   storeID,
				Name: "test-org",
			},
		},
	}
	client.EXPECT().ListStores(mock.Anything, mock.Anything).Return(listStoresResponse, nil)

	// Mock IDM check for invite
	mockIDMChecker.EXPECT().UserByMail(mock.Anything, "newuser@example.com").Return(&graph.User{
		UserID: "newuser@example.com",
		Email:  "newuser@example.com",
	}, nil).Once()

	// Mock Write calls for role assignments
	// 2 writes for owner role (existing user) + 2 writes for member role (invited user) = 4 total
	client.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
		return req.StoreId == storeID &&
			len(req.Writes.TupleKeys) == 1
	})).Return(&openfgav1.WriteResponse{}, nil).Times(4)

	// Set cluster ID in context
	ctx = appcontext.SetClusterId(ctx, ai.Spec.Account.GeneratedClusterId)

	result, err := service.AssignRolesToUsers(ctx, rCtx, changes, invites)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Success)
	assert.Equal(t, 4, result.AssignedCount) // 2 for existing user + 2 for invited user
	assert.Empty(t, result.Errors)
}

func TestService_AssignRolesToUsers_WithInvites_IDMCheckError(t *testing.T) {
	service, client := createTestService(t)

	// Create mocks for workspace client factory and IDM user checker
	mockWsFactory := fgamocks.NewClientFactory(t)
	mockIDMChecker := fgamocks.NewIDMUserChecker(t)

	// Set the mocks on the service
	service.wsClientFactory = mockWsFactory
	service.idmChecker = mockIDMChecker

	ctx := context.Background()
	ctx = appcontext.SetKCPContext(ctx, appcontext.KCPContext{
		IDMTenant:        "test-tenant",
		OrganizationName: "test-org",
	})
	log, _ := logger.New(logger.DefaultConfig())
	ctx = logger.SetLoggerInContext(ctx, log)

	rCtx := graph.ResourceContext{
		Group: "core.platform-mesh.io",
		Kind:  "Account",
		Resource: &graph.Resource{
			Name:      "test-account",
			Namespace: ptr.To("default"),
		},
		AccountPath: "root:org:test-account",
	}

	ai := &pmcorev1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account"},
		Spec: pmcorev1alpha1.AccountInfoSpec{
			Account: pmcorev1alpha1.AccountLocation{
				GeneratedClusterId: "cluster-123",
			},
		},
	}

	invites := []*graph.InviteInput{
		{
			Email: "newuser@example.com",
			Roles: []string{"member"},
		},
	}

	storeID := "store-123"

	// Mock ListStores call for StoreHelper
	listStoresResponse := &openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{
			{
				Id:   storeID,
				Name: "test-org",
			},
		},
	}
	client.EXPECT().ListStores(mock.Anything, mock.Anything).Return(listStoresResponse, nil)

	// Mock IDM check - returns error
	mockIDMChecker.EXPECT().UserByMail(mock.Anything, "newuser@example.com").Return(
		nil, assert.AnError).Once()

	// Mock Write calls for role assignment (code continues despite invite error)
	client.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
		return req.StoreId == storeID &&
			len(req.Writes.TupleKeys) == 1
	})).Return(&openfgav1.WriteResponse{}, nil).Times(2)

	// Set cluster ID in context
	ctx = appcontext.SetClusterId(ctx, ai.Spec.Account.GeneratedClusterId)

	result, err := service.AssignRolesToUsers(ctx, rCtx, nil, invites)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Success)          // False because there's an invite error
	assert.Equal(t, 2, result.AssignedCount) // Roles still get assigned
	assert.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Errors[0], "failed to create invite")
}

func TestService_AssignRolesToUsers_WithInvites_WorkspaceClientError(t *testing.T) {
	service, client := createTestService(t)

	// Create mocks for workspace client factory and IDM user checker
	mockWsFactory := fgamocks.NewClientFactory(t)
	mockIDMChecker := fgamocks.NewIDMUserChecker(t)

	// Set the mocks on the service
	service.wsClientFactory = mockWsFactory
	service.idmChecker = mockIDMChecker

	ctx := context.Background()
	ctx = appcontext.SetKCPContext(ctx, appcontext.KCPContext{
		IDMTenant:        "test-tenant",
		OrganizationName: "test-org",
	})
	log, _ := logger.New(logger.DefaultConfig())
	ctx = logger.SetLoggerInContext(ctx, log)

	rCtx := graph.ResourceContext{
		Group: "core.platform-mesh.io",
		Kind:  "Account",
		Resource: &graph.Resource{
			Name:      "test-account",
			Namespace: ptr.To("default"),
		},
		AccountPath: "root:org:test-account",
	}

	ai := &pmcorev1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account"},
		Spec: pmcorev1alpha1.AccountInfoSpec{
			Account: pmcorev1alpha1.AccountLocation{
				GeneratedClusterId: "cluster-123",
			},
		},
	}

	invites := []*graph.InviteInput{
		{
			Email: "newuser@example.com",
			Roles: []string{"member"},
		},
	}

	storeID := "store-123"

	// Mock ListStores call for StoreHelper
	listStoresResponse := &openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{
			{
				Id:   storeID,
				Name: "test-org",
			},
		},
	}
	client.EXPECT().ListStores(mock.Anything, mock.Anything).Return(listStoresResponse, nil)

	// Mock IDM check - user doesn't exist
	mockIDMChecker.EXPECT().UserByMail(mock.Anything, "newuser@example.com").Return(
		nil, nil).Once()

	// Mock workspace client creation - returns error
	mockWsFactory.EXPECT().New(mock.Anything, multicluster.ClusterName("root:org:test-account:test-account")).Return(nil, assert.AnError).Once()

	// Mock Write calls for role assignment (code continues despite invite error)
	client.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
		return req.StoreId == storeID &&
			len(req.Writes.TupleKeys) == 1
	})).Return(&openfgav1.WriteResponse{}, nil).Times(2)

	// Set cluster ID in context
	ctx = appcontext.SetClusterId(ctx, ai.Spec.Account.GeneratedClusterId)

	result, err := service.AssignRolesToUsers(ctx, rCtx, nil, invites)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Success)          // False because there's an invite error
	assert.Equal(t, 2, result.AssignedCount) // Roles still get assigned
	assert.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Errors[0], "failed to create invite")
}

func TestService_AssignRolesToUsers_WithInvites_InvalidEmail(t *testing.T) {
	service, client := createTestService(t)

	// Create mocks for workspace client factory and IDM user checker
	mockWsFactory := fgamocks.NewClientFactory(t)
	mockIDMChecker := fgamocks.NewIDMUserChecker(t)

	// Create scheme with security operator API
	scheme := runtime.NewScheme()
	err := pmcorev1alpha1.AddToScheme(scheme)
	require.NoError(t, err)

	mockWsClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	// Set the mocks on the service
	service.wsClientFactory = mockWsFactory
	service.idmChecker = mockIDMChecker

	ctx := context.Background()
	ctx = appcontext.SetKCPContext(ctx, appcontext.KCPContext{
		IDMTenant:        "test-tenant",
		OrganizationName: "test-org",
	})
	log, _ := logger.New(logger.DefaultConfig())
	ctx = logger.SetLoggerInContext(ctx, log)

	rCtx := graph.ResourceContext{
		Group: "core.platform-mesh.io",
		Kind:  "Account",
		Resource: &graph.Resource{
			Name:      "test-account",
			Namespace: ptr.To("default"),
		},
		AccountPath: "root:org:test-account",
	}

	ai := &pmcorev1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account"},
		Spec: pmcorev1alpha1.AccountInfoSpec{
			Account: pmcorev1alpha1.AccountLocation{
				GeneratedClusterId: "cluster-123",
			},
		},
	}

	invites := []*graph.InviteInput{
		{
			Email: "invalid-email",
			Roles: []string{"member"},
		},
	}

	storeID := "store-123"

	// Mock ListStores call for StoreHelper
	listStoresResponse := &openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{
			{
				Id:   storeID,
				Name: "test-org",
			},
		},
	}
	client.EXPECT().ListStores(mock.Anything, mock.Anything).Return(listStoresResponse, nil)

	// Mock IDM check - user doesn't exist
	mockIDMChecker.EXPECT().UserByMail(mock.Anything, "invalid-email").Return(
		nil, nil).Once()

	// Mock workspace client creation
	mockWsFactory.EXPECT().New(mock.Anything, multicluster.ClusterName("root:org:test-account:test-account")).Return(mockWsClient, nil).Once()

	// Mock Write calls for role assignment (code continues despite invite error)
	client.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
		return req.StoreId == storeID &&
			len(req.Writes.TupleKeys) == 1
	})).Return(&openfgav1.WriteResponse{}, nil).Times(2)

	// Set cluster ID in context
	ctx = appcontext.SetClusterId(ctx, ai.Spec.Account.GeneratedClusterId)

	result, err := service.AssignRolesToUsers(ctx, rCtx, nil, invites)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Success)          // False because there's an invite error
	assert.Equal(t, 2, result.AssignedCount) // Roles still get assigned
	assert.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Errors[0], "failed to create invite")
}
