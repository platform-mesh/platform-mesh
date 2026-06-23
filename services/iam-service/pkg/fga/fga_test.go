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
	"path/filepath"
	"sort"
	"testing"
	"time"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	accountsv1alpha1 "go.platform-mesh.io/apis/core/v1alpha1"
	"go.platform-mesh.io/golang-commons/logger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"go.platform-mesh.io/iam-service/pkg/config"
	appcontext "go.platform-mesh.io/iam-service/pkg/context"
	fgamocks "go.platform-mesh.io/iam-service/pkg/fga/mocks"
	"go.platform-mesh.io/iam-service/pkg/graph"
	"go.platform-mesh.io/iam-service/pkg/roles"
)

// createTestConfig creates a test configuration
func createTestConfig() *config.ServiceConfig {
	testRolesFile := filepath.Join("testdata", "roles.yaml")
	return &config.ServiceConfig{
		OpenFGA: config.OpenFGAConfig{
			GRPCAddr:      "localhost:8081",
			StoreCacheTTL: 5 * time.Minute,
		},
		Roles: config.RolesConfig{
			FilePath: testRolesFile,
		},
		Keycloak: config.KeycloakConfig{
			Cache: config.KeycloakCacheConfig{
				TTL:     5 * time.Minute,
				Enabled: true,
			},
		},
	}
}

// createTestService creates a test service with a real roles retriever
func createTestService(t *testing.T) (*Service, *fgamocks.OpenFGAServiceClient) {
	client := fgamocks.NewOpenFGAServiceClient(t)

	// Use real roles retriever with test data
	testRolesFile := filepath.Join("testdata", "roles.yaml")
	rolesRetriever, err := roles.NewFileBasedRolesRetriever(testRolesFile)
	if err != nil {
		t.Fatalf("Failed to create roles retriever: %v", err)
	}

	// Create config with proper OpenFGA settings
	cfg := createTestConfig()
	service := NewWithRolesRetriever(client, cfg, rolesRetriever)
	return service, client
}

func TestNew(t *testing.T) {
	client := fgamocks.NewOpenFGAServiceClient(t)

	// Create config with testdata roles file
	cfg := createTestConfig()
	service, err := New(client, cfg, nil, nil)

	// Should succeed with test config
	assert.NoError(t, err)
	assert.NotNil(t, service)
}

func TestService_ListUsers_Success(t *testing.T) {
	service, client := createTestService(t)

	ctx := context.Background()
	ctx = appcontext.SetKCPContext(ctx, appcontext.KCPContext{
		IDMTenant:        "test-tenant",
		OrganizationName: "test-org",
	})

	rCtx := graph.ResourceContext{
		Group: "core.platform-mesh.io",
		Kind:  "Account",
		Resource: &graph.Resource{
			Name:      "test-account",
			Namespace: ptr.To("default"),
		},
		AccountPath: "test-account",
	}

	ai := &accountsv1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account"},
		Spec: accountsv1alpha1.AccountInfoSpec{
			Account: accountsv1alpha1.AccountLocation{
				GeneratedClusterId: "cluster-123",
			},
		},
	}

	roleFilters := []string{"owner", "member"}
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

	// Mock ListUsers calls for each role
	ownerUsersResponse := &openfgav1.ListUsersResponse{
		Users: []*openfgav1.User{
			{
				User: &openfgav1.User_Object{
					Object: &openfgav1.Object{
						Type: "user",
						Id:   "user1",
					},
				},
			},
			{
				User: &openfgav1.User_Object{
					Object: &openfgav1.Object{
						Type: "user",
						Id:   "user2",
					},
				},
			},
		},
	}

	memberUsersResponse := &openfgav1.ListUsersResponse{
		Users: []*openfgav1.User{
			{
				User: &openfgav1.User_Object{
					Object: &openfgav1.Object{
						Type: "user",
						Id:   "user2",
					},
				},
			},
			{
				User: &openfgav1.User_Object{
					Object: &openfgav1.Object{
						Type: "user",
						Id:   "user3",
					},
				},
			},
		},
	}

	// Expect calls for owner and member roles
	client.EXPECT().ListUsers(mock.Anything, mock.MatchedBy(func(req *openfgav1.ListUsersRequest) bool {
		return req.StoreId == storeID &&
			req.Object.Type == "role" &&
			req.Object.Id == "core_platform-mesh_io_account/cluster-123/test-account/owner" &&
			req.Relation == "assignee"
	})).Return(ownerUsersResponse, nil)

	client.EXPECT().ListUsers(mock.Anything, mock.MatchedBy(func(req *openfgav1.ListUsersRequest) bool {
		return req.StoreId == storeID &&
			req.Object.Type == "role" &&
			req.Object.Id == "core_platform-mesh_io_account/cluster-123/test-account/member" &&
			req.Relation == "assignee"
	})).Return(memberUsersResponse, nil)

	// Set cluster ID in context since it's now retrieved from context instead of accountinfo
	ctx = appcontext.SetClusterId(ctx, ai.Spec.Account.GeneratedClusterId)

	result, err := service.ListUsers(ctx, rCtx, roleFilters)

	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Verify the results - convert to map for easier testing
	resultMap := make(map[string][]string)
	for _, userRoles := range result {
		var roleNames []string
		for _, role := range userRoles.Roles {
			roleNames = append(roleNames, role.ID)
		}
		sort.Strings(roleNames) // Sort for deterministic comparison
		resultMap[userRoles.User.Email] = roleNames
	}

	expected := map[string][]string{
		"user1": {"owner"},
		"user2": {"member", "owner"}, // sorted alphabetically
		"user3": {"member"},
	}

	assert.Equal(t, expected, resultMap)
}

func TestService_ListUsers_NoKCPContext(t *testing.T) {
	service, _ := createTestService(t)

	ctx := context.Background()

	rCtx := graph.ResourceContext{
		Group: "core.platform-mesh.io",
		Kind:  "Account",
		Resource: &graph.Resource{
			Name:      "test-account",
			Namespace: ptr.To("default"),
		},
		AccountPath: "test-account",
	}

	ai := &accountsv1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account"},
		Spec: accountsv1alpha1.AccountInfoSpec{
			Account: accountsv1alpha1.AccountLocation{
				GeneratedClusterId: "cluster-123",
			},
		},
	}

	// Set cluster ID in context since it's now retrieved from context instead of accountinfo
	ctx = appcontext.SetClusterId(ctx, ai.Spec.Account.GeneratedClusterId)

	result, err := service.ListUsers(ctx, rCtx, []string{"owner"})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "kcp user context")
}

func TestService_CountUsersForRole(t *testing.T) {
	service, client := createTestService(t)

	ctx := context.Background()
	ctx = appcontext.SetKCPContext(ctx, appcontext.KCPContext{
		IDMTenant:        "test-tenant",
		OrganizationName: "test-org",
	})
	ctx = appcontext.SetClusterId(ctx, "cluster-123")

	rCtx := graph.ResourceContext{
		Group: "core.platform-mesh.io",
		Kind:  "Account",
		Resource: &graph.Resource{
			Name:      "test-account",
			Namespace: ptr.To("default"),
		},
		AccountPath: "test-account",
	}

	client.EXPECT().ListStores(mock.Anything, mock.Anything).Return(&openfgav1.ListStoresResponse{
		Stores: []*openfgav1.Store{
			{
				Id:   "store-123",
				Name: "test-org",
			},
		},
	}, nil)

	client.EXPECT().ListUsers(mock.Anything, mock.MatchedBy(func(req *openfgav1.ListUsersRequest) bool {
		return req.StoreId == "store-123" &&
			req.Object.Type == "role" &&
			req.Object.Id == "core_platform-mesh_io_account/cluster-123/test-account/owner" &&
			req.Relation == "assignee"
	})).Return(&openfgav1.ListUsersResponse{
		Users: []*openfgav1.User{
			{
				User: &openfgav1.User_Object{
					Object: &openfgav1.Object{Type: "user", Id: "user1"},
				},
			},
			{
				User: &openfgav1.User_Object{
					Object: &openfgav1.Object{Type: "user", Id: "user2"},
				},
			},
		},
	}, nil)

	count, err := service.CountUsersForRole(ctx, rCtx, "owner")

	assert.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestApplyRoleFilter_WithFilters(t *testing.T) {
	// Create a logger for testing
	log, _ := logger.New(logger.DefaultConfig())

	service, _ := createTestService(t)
	roleFilters := []string{"owner", "member", "invalid-role"}
	rCtx := graph.ResourceContext{
		Group: "core.platform-mesh.io",
		Kind:  "Account",
	}
	result, err := service.applyRoleFilter(rCtx, roleFilters, log)

	assert.NoError(t, err)
	expected := []string{"owner", "member"}
	assert.Equal(t, expected, result)
}

func TestService_GetRoles_Success(t *testing.T) {
	service, _ := createTestService(t)

	ctx := context.Background()
	rCtx := graph.ResourceContext{
		Group: "core.platform-mesh.io",
		Kind:  "Account",
		Resource: &graph.Resource{
			Name:      "test-account",
			Namespace: ptr.To("default"),
		},
		AccountPath: "test-account",
	}

	result, err := service.GetRoles(ctx, rCtx)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result, 2)

	// Check the roles are properly mapped
	roleMap := make(map[string]*graph.Role)
	for _, role := range result {
		roleMap[role.ID] = role
	}

	ownerRole, exists := roleMap["owner"]
	assert.True(t, exists)
	assert.Equal(t, "owner", ownerRole.ID)
	assert.Equal(t, "Owner", ownerRole.DisplayName)

	memberRole, exists := roleMap["member"]
	assert.True(t, exists)
	assert.Equal(t, "member", memberRole.ID)
	assert.Equal(t, "Member", memberRole.DisplayName)
}

func TestService_AssignRolesToUsers_Success(t *testing.T) {
	service, client := createTestService(t)

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
		AccountPath: "test-account",
	}

	ai := &accountsv1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account"},
		Spec: accountsv1alpha1.AccountInfoSpec{
			Account: accountsv1alpha1.AccountLocation{
				GeneratedClusterId: "cluster-123",
			},
		},
	}

	changes := []*graph.UserRoleChange{
		{
			UserID: "user1@example.com",
			Roles:  []string{"owner", "member"},
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

	// Mock Write calls for each role assignment (now writes 2 separate calls per role)
	// For 2 roles (owner, member), we expect 4 Write calls total (2 per role)
	client.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
		return req.StoreId == storeID &&
			len(req.Writes.TupleKeys) == 1
	})).Return(&openfgav1.WriteResponse{}, nil).Times(4)

	// Set cluster ID in context since it's now retrieved from context instead of accountinfo
	ctx = appcontext.SetClusterId(ctx, ai.Spec.Account.GeneratedClusterId)

	result, err := service.AssignRolesToUsers(ctx, rCtx, changes, nil)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Success)
	assert.Equal(t, 4, result.AssignedCount)
	assert.Empty(t, result.Errors)
}

func TestService_AssignRolesToUsers_InvalidRole(t *testing.T) {
	service, client := createTestService(t)

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
		AccountPath: "test-account",
	}

	ai := &accountsv1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account"},
		Spec: accountsv1alpha1.AccountInfoSpec{
			Account: accountsv1alpha1.AccountLocation{
				GeneratedClusterId: "cluster-123",
			},
		},
	}

	changes := []*graph.UserRoleChange{
		{
			UserID: "user1@example.com",
			Roles:  []string{"owner", "admin"}, // admin is not in defaultRoles
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

	// Mock Write calls for owner role only (admin should be rejected) - now writes 2 separate calls per role
	// First call: assignee tuple
	client.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
		return req.StoreId == storeID &&
			len(req.Writes.TupleKeys) == 1 &&
			req.Writes.TupleKeys[0].User == "user:user1@example.com" &&
			req.Writes.TupleKeys[0].Object == "role:core_platform-mesh_io_account/cluster-123/test-account/owner" &&
			req.Writes.TupleKeys[0].Relation == "assignee"
	})).Return(&openfgav1.WriteResponse{}, nil).Once()
	// Second call: role tuple
	client.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
		return req.StoreId == storeID &&
			len(req.Writes.TupleKeys) == 1 &&
			req.Writes.TupleKeys[0].Relation == "owner"
	})).Return(&openfgav1.WriteResponse{}, nil).Once()

	// Set cluster ID in context since it's now retrieved from context instead of accountinfo
	ctx = appcontext.SetClusterId(ctx, ai.Spec.Account.GeneratedClusterId)

	result, err := service.AssignRolesToUsers(ctx, rCtx, changes, nil)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Success)
	assert.Equal(t, 2, result.AssignedCount)
	assert.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0], "role 'admin' is not allowed")
}

func TestService_RemoveRole_Success(t *testing.T) {
	service, client := createTestService(t)

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
		AccountPath: "test-account",
	}

	ai := &accountsv1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account"},
		Spec: accountsv1alpha1.AccountInfoSpec{
			Account: accountsv1alpha1.AccountLocation{
				GeneratedClusterId: "cluster-123",
			},
		},
	}

	input := graph.RemoveRoleInput{
		UserID: "user1@example.com",
		Role:   "owner",
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

	// Mock Read call to check if tuple exists - returns tuple (role is assigned)
	readResponse := &openfgav1.ReadResponse{
		Tuples: []*openfgav1.Tuple{
			{
				Key: &openfgav1.TupleKey{
					User:     "user:user1@example.com",
					Relation: "assignee",
					Object:   "role:core_platform-mesh_io_account/cluster-123/test-account/owner",
				},
			},
		},
	}
	client.EXPECT().Read(mock.Anything, mock.MatchedBy(func(req *openfgav1.ReadRequest) bool {
		return req.StoreId == storeID &&
			req.TupleKey.User == "user:user1@example.com" &&
			req.TupleKey.Object == "role:core_platform-mesh_io_account/cluster-123/test-account/owner" &&
			req.TupleKey.Relation == "assignee"
	})).Return(readResponse, nil).Once()

	// Mock Write call for deletion
	client.EXPECT().Write(mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
		return req.StoreId == storeID &&
			req.Deletes != nil &&
			len(req.Deletes.TupleKeys) == 1 &&
			req.Deletes.TupleKeys[0].User == "user:user1@example.com" &&
			req.Deletes.TupleKeys[0].Object == "role:core_platform-mesh_io_account/cluster-123/test-account/owner" &&
			req.Deletes.TupleKeys[0].Relation == "assignee"
	})).Return(&openfgav1.WriteResponse{}, nil).Once()

	// Set cluster ID in context since it's now retrieved from context instead of accountinfo
	ctx = appcontext.SetClusterId(ctx, ai.Spec.Account.GeneratedClusterId)

	result, err := service.RemoveRole(ctx, rCtx, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Success)
	assert.True(t, result.WasAssigned)
	assert.Nil(t, result.Error)
}

func TestService_RemoveRole_RoleNotAssigned(t *testing.T) {
	service, client := createTestService(t)

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
		AccountPath: "test-account",
	}

	ai := &accountsv1alpha1.AccountInfo{
		ObjectMeta: metav1.ObjectMeta{Name: "account"},
		Spec: accountsv1alpha1.AccountInfoSpec{
			Account: accountsv1alpha1.AccountLocation{
				GeneratedClusterId: "cluster-123",
			},
		},
	}

	input := graph.RemoveRoleInput{
		UserID: "user1@example.com",
		Role:   "member",
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

	// Mock Read call to check if tuple exists - returns empty (role is not assigned)
	readResponse := &openfgav1.ReadResponse{
		Tuples: []*openfgav1.Tuple{}, // Empty - no tuples found
	}
	client.EXPECT().Read(mock.Anything, mock.MatchedBy(func(req *openfgav1.ReadRequest) bool {
		return req.StoreId == storeID &&
			req.TupleKey.User == "user:user1@example.com" &&
			req.TupleKey.Object == "role:core_platform-mesh_io_account/cluster-123/test-account/member" &&
			req.TupleKey.Relation == "assignee"
	})).Return(readResponse, nil).Once()

	// No Write call should be made since the role wasn't assigned

	// Set cluster ID in context since it's now retrieved from context instead of accountinfo
	ctx = appcontext.SetClusterId(ctx, ai.Spec.Account.GeneratedClusterId)

	result, err := service.RemoveRole(ctx, rCtx, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Success)      // Still successful since idempotent
	assert.False(t, result.WasAssigned) // But role wasn't assigned
	assert.Nil(t, result.Error)
}
