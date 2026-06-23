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

package pm

import (
	"context"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	pmcontext "go.platform-mesh.io/golang-commons/context"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	"go.platform-mesh.io/iam-service/pkg/config"
	"go.platform-mesh.io/iam-service/pkg/fga"
	"go.platform-mesh.io/iam-service/pkg/graph"
	"go.platform-mesh.io/iam-service/pkg/keycloak"
	"go.platform-mesh.io/iam-service/pkg/pager"
	"go.platform-mesh.io/iam-service/pkg/resolver/api"
	serrors "go.platform-mesh.io/iam-service/pkg/resolver/errors"
	"go.platform-mesh.io/iam-service/pkg/resolver/transformer"
	"go.platform-mesh.io/iam-service/pkg/sorter"
	"go.platform-mesh.io/iam-service/pkg/workspace"
)

var _ api.ResolverService = (*Service)(nil)

const ownerRoleID = "owner"

type Service struct {
	fgaService      *fga.Service
	keycloakService *keycloak.Service
	userSorter      sorter.UserSorter
	pager           pager.Pager
	mgr             mcmanager.Manager
	transformer     *transformer.UserTransformer
}

func (s *Service) Me(ctx context.Context) (*graph.User, error) {
	webToken, err := pmcontext.GetWebTokenFromContext(ctx)
	if err != nil {
		return nil, serrors.ErrInternal
	}

	u := &graph.User{
		UserID:    webToken.Subject,
		Email:     webToken.Mail,
		FirstName: &webToken.FirstName,
		LastName:  &webToken.LastName,
	}

	return s.transformer.Transform(u), nil
}

func (s *Service) User(ctx context.Context, userID string) (*graph.User, error) {
	user, err := s.keycloakService.UserByMail(ctx, userID)
	if err != nil {
		return nil, err
	}

	return s.transformer.Transform(user), nil
}

func (s *Service) Users(ctx context.Context, rctx graph.ResourceContext, roleFilters []string, sortBy *graph.SortByInput, page *graph.PageInput) (*graph.UserConnection, error) {
	ownersCount, err := s.fgaService.CountUsersForRole(ctx, rctx, ownerRoleID)
	if err != nil {
		return nil, err
	}

	allUserRoles, err := s.fgaService.ListUsers(ctx, rctx, roleFilters)
	if err != nil {
		return nil, err
	}

	err = s.keycloakService.EnrichUserRoles(ctx, allUserRoles)
	if err != nil {
		return nil, err
	}

	s.userSorter.SortUserRoles(allUserRoles, sortBy)

	for _, ur := range allUserRoles {
		ur.User = s.transformer.Transform(ur.User)
	}

	totalCount := len(allUserRoles)
	paginatedUserRoles, pageInfo := s.pager.PaginateUserRoles(allUserRoles, page, totalCount)

	return &graph.UserConnection{
		Users:       paginatedUserRoles,
		PageInfo:    pageInfo,
		OwnersCount: ownersCount,
	}, nil
}

func (s *Service) KnownUsers(ctx context.Context, sortBy *graph.SortByInput, page *graph.PageInput) (*graph.UserConnection, error) {
	// Fetch all known users from Keycloak
	users, err := s.keycloakService.GetUsers(ctx)
	if err != nil {
		return nil, err
	}

	// Sort users using the new SortUsers method
	s.userSorter.SortUsers(users, sortBy)

	// Paginate users using the new PaginateUsers method
	totalCount := len(users)
	paginatedUsers, pageInfo := s.pager.PaginateUsers(users, page, totalCount)

	// Convert []*graph.User to []*graph.UserRoles for GraphQL compatibility
	// For known users without role context, we create UserRoles with empty roles
	userRoles := make([]*graph.UserRoles, len(paginatedUsers))
	for i, user := range paginatedUsers {
		userRoles[i] = &graph.UserRoles{
			User:  s.transformer.Transform(user),
			Roles: []*graph.Role{}, // Empty roles for known users query
		}
	}

	return &graph.UserConnection{
		Users:       userRoles,
		PageInfo:    pageInfo,
		OwnersCount: 0,
	}, nil
}

func (s *Service) AssignRolesToUsers(ctx context.Context, rCtx graph.ResourceContext, changes []*graph.UserRoleChange, invites []*graph.InviteInput) (*graph.RoleAssignmentResult, error) {
	return s.fgaService.AssignRolesToUsers(ctx, rCtx, changes, invites)
}

func (s *Service) RemoveRole(ctx context.Context, rCtx graph.ResourceContext, input graph.RemoveRoleInput) (*graph.RoleRemovalResult, error) {
	return s.fgaService.RemoveRole(ctx, rCtx, input)
}

func (s *Service) Roles(ctx context.Context, context graph.ResourceContext) ([]*graph.Role, error) {
	return s.fgaService.GetRoles(ctx, context)
}

func NewResolverService(fgaClient openfgav1.OpenFGAServiceClient, service *keycloak.Service, cfg *config.ServiceConfig, mgr mcmanager.Manager) (*Service, error) {
	// Create workspace client factory
	wsClientFactory := workspace.NewClientFactory(mgr)

	// Create FGA service with workspace client factory and keycloak checker
	fgaService, err := fga.New(fgaClient, cfg, wsClientFactory, service)
	if err != nil {
		return nil, err
	}

	return &Service{
		fgaService:      fgaService,
		keycloakService: service,
		userSorter:      sorter.NewUserSorterWithConfig(cfg),
		pager:           pager.NewPager(cfg),
		mgr:             mgr,
		transformer:     transformer.NewUserTransformer(&cfg.JWT),
	}, nil
}
