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

package api

import (
	"context"

	"go.platform-mesh.io/iam-service/pkg/graph"
)

type ResolverService interface {
	Me(ctx context.Context) (*graph.User, error)
	User(ctx context.Context, userID string) (*graph.User, error)
	Users(ctx context.Context, context graph.ResourceContext, roleFilters []string, sortBy *graph.SortByInput, page *graph.PageInput) (*graph.UserConnection, error)
	Roles(ctx context.Context, context graph.ResourceContext) ([]*graph.Role, error)
	AssignRolesToUsers(ctx context.Context, context graph.ResourceContext, changes []*graph.UserRoleChange, invites []*graph.InviteInput) (*graph.RoleAssignmentResult, error)
	RemoveRole(ctx context.Context, context graph.ResourceContext, input graph.RemoveRoleInput) (*graph.RoleRemovalResult, error)
	KnownUsers(ctx context.Context, sortBy *graph.SortByInput, page *graph.PageInput) (*graph.UserConnection, error)
}
