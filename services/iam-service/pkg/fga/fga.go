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
	"fmt"
	"sync"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"go.opentelemetry.io/otel"
	"go.platform-mesh.io/golang-commons/errors"
	"go.platform-mesh.io/golang-commons/fga/util"
	"go.platform-mesh.io/golang-commons/logger"
	"google.golang.org/grpc/status"

	"go.platform-mesh.io/iam-service/pkg/config"
	appcontext "go.platform-mesh.io/iam-service/pkg/context"
	"go.platform-mesh.io/iam-service/pkg/fga/store"
	"go.platform-mesh.io/iam-service/pkg/graph"
	"go.platform-mesh.io/iam-service/pkg/roles"
	"go.platform-mesh.io/iam-service/pkg/workspace"
)

var (
	userFilter = []*openfgav1.UserTypeFilter{{Type: "user"}}
)

// sanitizeUserID returns a sanitized version of the userID for logging (first 3 chars + ***)
// to avoid logging PII information
func sanitizeUserID(userID string) string {
	if len(userID) <= 3 {
		return userID
	}
	return userID[:3] + "***"
}

type UserIDToRoles map[string][]string

// IDMUserChecker checks if a user exists in the Identity Management system
type IDMUserChecker interface {
	UserByMail(ctx context.Context, userID string) (*graph.User, error)
}

type Service struct {
	client          openfgav1.OpenFGAServiceClient
	helper          store.StoreHelper
	rolesRetriever  roles.RolesRetriever
	wsClientFactory workspace.ClientFactory
	idmChecker      IDMUserChecker
}

func New(client openfgav1.OpenFGAServiceClient, cfg *config.ServiceConfig, wsClientFactory workspace.ClientFactory, idmChecker IDMUserChecker) (*Service, error) {
	// Use configurable roles retriever from YAML file
	rolesRetriever, err := roles.NewFileBasedRolesRetriever(cfg.Roles.FilePath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to initialize roles retriever from YAML file")
	}

	return &Service{
		client:          client,
		helper:          store.NewFGAStoreHelper(cfg.OpenFGA.StoreCacheTTL),
		rolesRetriever:  rolesRetriever,
		wsClientFactory: wsClientFactory,
		idmChecker:      idmChecker,
	}, nil
}

// NewWithRolesRetriever creates a new FGA service with a custom roles retriever
func NewWithRolesRetriever(client openfgav1.OpenFGAServiceClient, cfg *config.ServiceConfig, rolesRetriever roles.RolesRetriever) *Service {
	helper := store.NewFGAStoreHelper(cfg.OpenFGA.StoreCacheTTL)
	return &Service{
		client:         client,
		helper:         helper,
		rolesRetriever: rolesRetriever,
	}
}

func (s *Service) ListUsers(ctx context.Context, rctx graph.ResourceContext, roleFilters []string) ([]*graph.UserRoles, error) {
	log := logger.LoadLoggerFromContext(ctx)
	ctx, span := otel.GetTracerProvider().Tracer("").Start(ctx, "fga.ListUsers")
	defer span.End()

	kctx, err := appcontext.GetKCPContext(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get kcp user context")
	}

	storeID, err := s.helper.GetStoreID(ctx, s.client, kctx.OrganizationName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get store ID for organization %s", kctx.OrganizationName)
	}

	appliedRoles, err := s.applyRoleFilter(rctx, roleFilters, log)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get available roles for group resource %s/%s", rctx.Group, rctx.Kind)
	}

	// If no roles to process, return empty result
	if len(appliedRoles) == 0 {
		return []*graph.UserRoles{}, nil
	}

	// Use parallel processing for multiple roles
	return s.listUsersParallel(ctx, rctx, storeID, appliedRoles)
}

func (s *Service) CountUsersForRole(ctx context.Context, rctx graph.ResourceContext, roleID string) (int, error) {
	users, err := s.ListUsers(ctx, rctx, []string{roleID})
	if err != nil {
		return 0, err
	}

	return len(users), nil
}

// listUsersParallel performs parallel ListUsers calls for multiple roles
func (s *Service) listUsersParallel(ctx context.Context, rctx graph.ResourceContext, storeID string, roles []string) ([]*graph.UserRoles, error) {

	type roleResult struct {
		role  string
		users *openfgav1.ListUsersResponse
		err   error
	}
	fgaTypeName := util.ConvertToTypeName(rctx.Group, rctx.Kind)

	// Create channels for goroutine communication
	resultChan := make(chan roleResult, len(roles))

	clusterId, err := appcontext.GetClusterId(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get cluster ID from context")
	}

	// Launch goroutines for each role
	for _, role := range roles {
		go func(role string) {
			req := &openfgav1.ListUsersRequest{
				StoreId: storeID,
				Object: &openfgav1.Object{
					Type: "role",
					Id: fmt.Sprintf("%s/%s/%s/%s",
						fgaTypeName,
						clusterId,
						rctx.Resource.Name,
						role),
				},
				Relation:    "assignee",
				UserFilters: userFilter,
			}

			users, err := s.client.ListUsers(ctx, req)
			resultChan <- roleResult{
				role:  role,
				users: users,
				err:   err,
			}
		}(role)
	}

	// Collect results from all goroutines
	allUserIDToRoles := UserIDToRoles{}
	var mu sync.Mutex

	for i := 0; i < len(roles); i++ {
		result := <-resultChan

		// Handle any errors
		if result.err != nil {
			return nil, errors.Wrap(result.err, "failed to list users for resource %s with role %s", rctx.Resource.Name, result.role)
		}

		// Process users for this role with thread safety
		mu.Lock()
		for _, tuple := range result.users.Users {
			user := tuple.User.(*openfgav1.User_Object)
			allUserIDToRoles[user.Object.Id] = append(allUserIDToRoles[user.Object.Id], result.role)
		}
		mu.Unlock()
	}

	// Convert UserIDToRoles to []*graph.UserRoles
	return s.convertToGraphUserRoles(rctx, allUserIDToRoles), nil
}

// convertToGraphUserRoles converts UserIDToRoles map to []*graph.UserRoles
func (s *Service) convertToGraphUserRoles(rctx graph.ResourceContext, userIDToRoles UserIDToRoles) []*graph.UserRoles {
	var result []*graph.UserRoles

	// Get role definitions for this group resource
	roleDefinitions, err := s.rolesRetriever.GetRoleDefinitions(rctx)
	if err != nil {
		// Fallback to basic roles if we can't get definitions
		roleDefinitions = []roles.RoleDefinition{}
	}

	// Create a map for quick role definition lookup
	roleDefMap := make(map[string]roles.RoleDefinition)
	for _, roleDef := range roleDefinitions {
		roleDefMap[roleDef.ID] = roleDef
	}

	for userID, roleNames := range userIDToRoles {
		// Create User with available information (only userID from OpenFGA)
		user := &graph.User{
			UserID: "",
			Email:  userID, // Not available from OpenFGA ListUsers response
		}

		// Convert role names to Role objects
		var rArr []*graph.Role
		for _, roleName := range roleNames {
			if roleDef, exists := roleDefMap[roleName]; exists {
				role := &graph.Role{
					ID:          roleDef.ID,
					DisplayName: roleDef.DisplayName,
					Description: roleDef.Description,
				}
				rArr = append(rArr, role)
			}
		}

		userRoles := &graph.UserRoles{
			User:  user,
			Roles: rArr,
		}

		result = append(result, userRoles)
	}

	return result
}

func (s *Service) GetRoles(ctx context.Context, rctx graph.ResourceContext) ([]*graph.Role, error) {
	log := logger.LoadLoggerFromContext(ctx)
	log = log.MustChildLoggerWithAttributes("group", rctx.Group, "kind", rctx.Kind)
	_, span := otel.GetTracerProvider().Tracer("").Start(ctx, "fga.GetRoles")
	defer span.End()

	// Get role definitions from the rArr retriever
	roleDefinitions, err := s.rolesRetriever.GetRoleDefinitions(rctx)
	if err != nil { // coverage-ignore
		return nil, errors.Wrap(err, "failed to get role definitions for group resource %s/%s", rctx.Group, rctx.Kind)
	}

	// Convert to graph.Role objects
	var rArr []*graph.Role
	for _, roleDef := range roleDefinitions {
		role := &graph.Role{
			ID:          roleDef.ID,
			DisplayName: roleDef.DisplayName,
			Description: roleDef.Description,
		}
		rArr = append(rArr, role)
	}

	log.Debug().Int("roleCount", len(rArr)).Msg("Successfully retrieved rArr")
	return rArr, nil
}

func (s *Service) applyRoleFilter(rctx graph.ResourceContext, roleFilters []string, log *logger.Logger) ([]string, error) {
	roleDefinitions, err := s.rolesRetriever.GetRoleDefinitions(rctx)
	if err != nil { // coverage-ignore
		return nil, errors.Wrap(err, "failed to get role definitions for group resource %s/%s", rctx.Group, rctx.Kind)
	}
	availableRoles := roles.GetAvailableRoleIDs(roleDefinitions)

	var appliedRoles []string
	if len(roleFilters) > 0 {
		log.Debug().Interface("roleFilters", roleFilters).Interface("availableRoles", availableRoles).Msg("Applying role filters")
		for _, role := range availableRoles {
			if contains := containsString(roleFilters, role); contains {
				appliedRoles = append(appliedRoles, role)
			}
		}
	} else {
		appliedRoles = availableRoles
	}
	return appliedRoles, nil
}

// AssignRolesToUsers creates tuples in FGA for the given users and roles, and processes invites
func (s *Service) AssignRolesToUsers(ctx context.Context, rctx graph.ResourceContext, changes []*graph.UserRoleChange, invites []*graph.InviteInput) (*graph.RoleAssignmentResult, error) {
	log := logger.LoadLoggerFromContext(ctx)
	log = log.MustChildLoggerWithAttributes("group", rctx.Group, "kind", rctx.Kind)
	ctx, span := otel.GetTracerProvider().Tracer("").Start(ctx, "fga.AssignRolesToUsers")
	defer span.End()

	clusterId, err := appcontext.GetClusterId(ctx)
	if err != nil { // coverage-ignore
		return nil, errors.Wrap(err, "failed to get cluster ID from account path")
	}

	kctx, err := appcontext.GetKCPContext(ctx)
	if err != nil { // coverage-ignore
		return nil, errors.Wrap(err, "failed to get kcp user context")
	}
	fgaTypeName := util.ConvertToTypeName(rctx.Group, rctx.Kind)

	storeID, err := s.helper.GetStoreID(ctx, s.client, kctx.OrganizationName)
	if err != nil { // coverage-ignore
		return nil, errors.Wrap(err, "failed to get store ID for organization %s", kctx.OrganizationName)
	}

	var allErrors []string
	var totalAssigned int

	// Process invites first - create Invite resources for users that don't exist
	// and then assign their roles
	if len(invites) > 0 {
		invitedCount, inviteErrors := s.processInvites(ctx, rctx, invites, storeID, fgaTypeName, clusterId, log)
		totalAssigned += invitedCount
		allErrors = append(allErrors, inviteErrors...)
	}

	// Process regular user role changes (for existing users)
	for _, change := range changes {
		changeLog := log.MustChildLoggerWithAttributes("userId", sanitizeUserID(change.UserID))
		changeLog.Debug().Interface("roles", change.Roles).Msg("Processing role assignment")

		// Validate that only available roles are being assigned
		roleDefinitions, err := s.rolesRetriever.GetRoleDefinitions(rctx)
		if err != nil { // coverage-ignore
			errMsg := fmt.Sprintf("failed to get role definitions for group resource '%s/%s': %v", rctx.Group, rctx.Kind, err)
			allErrors = append(allErrors, errMsg)
			log.Error().Err(err).Msg("Failed to retrieve role definitions")
			continue
		}
		availableRoles := roles.GetAvailableRoleIDs(roleDefinitions)

		for _, role := range change.Roles {
			roleLog := changeLog.MustChildLoggerWithAttributes("role", role)
			if !containsString(availableRoles, role) {
				errMsg := fmt.Sprintf("role '%s' is not allowed for user '%s'. Only roles %v are permitted", role, sanitizeUserID(change.UserID), availableRoles)
				allErrors = append(allErrors, errMsg)
				roleLog.Warn().Interface("availableRoles", availableRoles).Msg("Invalid role assignment attempted")
				continue
			}

			// Assign the role to the user
			count, errs := s.assignRoleToUser(ctx, change.UserID, role, rctx, storeID, fgaTypeName, clusterId, roleLog)
			totalAssigned += count
			allErrors = append(allErrors, errs...)
		}
	}

	// Determine overall success
	success := len(allErrors) == 0

	return &graph.RoleAssignmentResult{
		Success:       success,
		Errors:        allErrors,
		AssignedCount: totalAssigned,
	}, nil
}

// RemoveRole removes a role from a user by deleting the tuple in FGA
func (s *Service) RemoveRole(ctx context.Context, rctx graph.ResourceContext, input graph.RemoveRoleInput) (*graph.RoleRemovalResult, error) {
	log := logger.LoadLoggerFromContext(ctx)
	log = log.MustChildLoggerWithAttributes("group", rctx.Group, "kind", rctx.Kind)
	ctx, span := otel.GetTracerProvider().Tracer("").Start(ctx, "fga.RemoveRole")
	defer span.End()

	clusterId, err := appcontext.GetClusterId(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get cluster ID from account path")
	}

	fgaTypeName := util.ConvertToTypeName(rctx.Group, rctx.Kind)
	kctx, err := appcontext.GetKCPContext(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get kcp user context")
	}

	storeID, err := s.helper.GetStoreID(ctx, s.client, kctx.OrganizationName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get store ID for organization %s", kctx.OrganizationName)
	}

	log.Debug().Str("userId", sanitizeUserID(input.UserID)).Str("role", input.Role).Msg("Processing role removal")

	// Validate that only available roles can be removed
	roleDefinitions, err := s.rolesRetriever.GetRoleDefinitions(rctx)
	if err != nil {
		errMsg := fmt.Sprintf("failed to get role definitions for group resource '%s/%s': %v", rctx.Group, rctx.Kind, err)
		log.Error().Err(err).Msg("Failed to retrieve role definitions")
		return &graph.RoleRemovalResult{
			Success:     false,
			Error:       &errMsg,
			WasAssigned: false,
		}, nil
	}
	availableRoles := roles.GetAvailableRoleIDs(roleDefinitions)

	if !containsString(availableRoles, input.Role) {
		errMsg := fmt.Sprintf("role '%s' is not allowed. Only roles %v are permitted", input.Role, availableRoles)
		log.Warn().Str("role", input.Role).Str("userId", sanitizeUserID(input.UserID)).Interface("availableRoles", availableRoles).Msg("Invalid role removal attempted")
		return &graph.RoleRemovalResult{
			Success:     false,
			Error:       &errMsg,
			WasAssigned: false,
		}, nil
	}

	// First, check if the tuple exists by trying to read it
	readTuple := &openfgav1.ReadRequestTupleKey{
		User:     fmt.Sprintf("user:%s", input.UserID),
		Relation: "assignee",
		Object: fmt.Sprintf("role:%s/%s/%s/%s",
			fgaTypeName,
			clusterId,
			rctx.Resource.Name,
			input.Role),
	}

	readReq := &openfgav1.ReadRequest{
		StoreId:  storeID,
		TupleKey: readTuple,
	}

	readResp, err := s.client.Read(ctx, readReq)
	if err != nil {
		log.Error().Err(err).Str("role", input.Role).Str("userId", sanitizeUserID(input.UserID)).Msg("Failed to check if tuple exists")
		errMsg := fmt.Sprintf("failed to check role assignment: %v", err)
		return &graph.RoleRemovalResult{
			Success:     false,
			Error:       &errMsg,
			WasAssigned: false,
		}, nil
	}

	// Check if the tuple was found
	wasAssigned := len(readResp.Tuples) > 0
	if !wasAssigned {
		log.Info().Str("role", input.Role).Str("userId", sanitizeUserID(input.UserID)).Msg("Role was not assigned to user - nothing to remove")
		return &graph.RoleRemovalResult{
			Success:     true,
			Error:       nil,
			WasAssigned: false,
		}, nil
	}

	// Delete the tuple from FGA
	deleteTuple := &openfgav1.TupleKeyWithoutCondition{
		User:     fmt.Sprintf("user:%s", input.UserID),
		Relation: "assignee",
		Object: fmt.Sprintf("role:%s/%s/%s/%s",
			fgaTypeName,
			clusterId,
			rctx.Resource.Name,
			input.Role),
	}

	deleteReq := &openfgav1.WriteRequest{
		StoreId: storeID,
		Deletes: &openfgav1.WriteRequestDeletes{
			TupleKeys: []*openfgav1.TupleKeyWithoutCondition{deleteTuple},
		},
	}

	_, err = s.client.Write(ctx, deleteReq)
	if err != nil {
		log.Error().Err(err).Str("role", input.Role).Str("userId", sanitizeUserID(input.UserID)).Msg("Failed to delete tuple from FGA")
		errMsg := fmt.Sprintf("failed to remove role '%s' from user '%s': %v", input.Role, sanitizeUserID(input.UserID), err)
		return &graph.RoleRemovalResult{
			Success:     false,
			Error:       &errMsg,
			WasAssigned: true,
		}, nil
	}

	log.Info().Str("role", input.Role).Str("userId", sanitizeUserID(input.UserID)).Msg("Successfully removed role from user")
	return &graph.RoleRemovalResult{
		Success:     true,
		Error:       nil,
		WasAssigned: true,
	}, nil
}

var containsString = func(arr []string, s string) bool {
	for _, a := range arr {
		if a == s {
			return true
		}
	}
	return false
}

func isDuplicateWriteError(err error) bool {
	if err == nil {
		return false
	}

	s, ok := status.FromError(err)
	return ok && int32(s.Code()) == int32(openfgav1.ErrorCode_write_failed_due_to_invalid_input)
}
