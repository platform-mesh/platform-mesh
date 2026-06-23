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

package pager

import (
	"go.platform-mesh.io/iam-service/pkg/config"
	"go.platform-mesh.io/iam-service/pkg/graph"
)

// Pager provides pagination functionality for slices
type Pager interface {
	// PaginateUserRoles applies pagination to a slice of user roles
	PaginateUserRoles(allUserRoles []*graph.UserRoles, page *graph.PageInput, totalCount int) ([]*graph.UserRoles, *graph.PageInfo)

	// PaginateUsers applies pagination to a slice of users
	PaginateUsers(allUsers []*graph.User, page *graph.PageInput, totalCount int) ([]*graph.User, *graph.PageInfo)
}

// DefaultPager implements the Pager interface with standard pagination logic
type DefaultPager struct {
	defaultLimit int
	defaultPage  int
}

// NewDefaultPager creates a new DefaultPager with default values
func NewDefaultPager() *DefaultPager {
	return &DefaultPager{
		defaultLimit: 10,
		defaultPage:  1,
	}
}

// NewPager creates a new DefaultPager with configurable values
func NewPager(cfg *config.ServiceConfig) *DefaultPager {
	return &DefaultPager{
		defaultLimit: cfg.Pagination.DefaultLimit,
		defaultPage:  cfg.Pagination.DefaultPage,
	}
}

// PaginateUserRoles applies pagination logic to the user roles list and returns the paginated slice and PageInfo
func (p *DefaultPager) PaginateUserRoles(allUserRoles []*graph.UserRoles, page *graph.PageInput, totalCount int) ([]*graph.UserRoles, *graph.PageInfo) {
	// Extract pagination parameters
	limit := p.defaultLimit
	if page != nil && page.Limit != nil {
		limit = *page.Limit
	}

	pageNum := p.defaultPage
	if page != nil && page.Page != nil {
		pageNum = *page.Page
	}

	// Ensure minimum values
	if limit < 1 {
		limit = p.defaultLimit
	}
	if pageNum < 1 {
		pageNum = p.defaultPage
	}

	// Calculate pagination bounds
	offset := (pageNum - 1) * limit
	end := offset + limit

	// Handle empty result set
	if totalCount == 0 {
		return []*graph.UserRoles{}, &graph.PageInfo{
			Count:           0,
			TotalCount:      0,
			HasNextPage:     false,
			HasPreviousPage: false,
		}
	}

	// Handle offset beyond total count
	if offset >= totalCount {
		return []*graph.UserRoles{}, &graph.PageInfo{
			Count:           0,
			TotalCount:      totalCount,
			HasNextPage:     false,
			HasPreviousPage: pageNum > 1,
		}
	}

	// Adjust end boundary
	if end > totalCount {
		end = totalCount
	}

	// Extract the paginated slice
	paginatedUserRoles := allUserRoles[offset:end]

	// Calculate pagination info
	count := len(paginatedUserRoles)
	hasNextPage := end < totalCount
	hasPreviousPage := pageNum > 1

	pageInfo := &graph.PageInfo{
		Count:           count,
		TotalCount:      totalCount,
		HasNextPage:     hasNextPage,
		HasPreviousPage: hasPreviousPage,
	}

	return paginatedUserRoles, pageInfo
}

// PaginateUsers applies pagination logic to the users list and returns the paginated slice and PageInfo
func (p *DefaultPager) PaginateUsers(allUsers []*graph.User, page *graph.PageInput, totalCount int) ([]*graph.User, *graph.PageInfo) {
	// Extract pagination parameters
	limit := p.defaultLimit
	if page != nil && page.Limit != nil {
		limit = *page.Limit
	}

	pageNum := p.defaultPage
	if page != nil && page.Page != nil {
		pageNum = *page.Page
	}

	// Ensure minimum values
	if limit < 1 {
		limit = p.defaultLimit
	}
	if pageNum < 1 {
		pageNum = p.defaultPage
	}

	// Calculate pagination bounds
	offset := (pageNum - 1) * limit
	end := offset + limit

	// Handle empty result set
	if totalCount == 0 {
		return []*graph.User{}, &graph.PageInfo{
			Count:           0,
			TotalCount:      0,
			HasNextPage:     false,
			HasPreviousPage: false,
		}
	}

	// Handle offset beyond total count
	if offset >= totalCount {
		return []*graph.User{}, &graph.PageInfo{
			Count:           0,
			TotalCount:      totalCount,
			HasNextPage:     false,
			HasPreviousPage: pageNum > 1,
		}
	}

	// Adjust end boundary
	if end > totalCount {
		end = totalCount
	}

	// Extract the paginated slice
	paginatedUsers := allUsers[offset:end]

	// Calculate pagination info
	count := len(paginatedUsers)
	hasNextPage := end < totalCount
	hasPreviousPage := pageNum > 1

	pageInfo := &graph.PageInfo{
		Count:           count,
		TotalCount:      totalCount,
		HasNextPage:     hasNextPage,
		HasPreviousPage: hasPreviousPage,
	}

	return paginatedUsers, pageInfo
}
