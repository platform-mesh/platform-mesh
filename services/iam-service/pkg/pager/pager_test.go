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
	"testing"

	"github.com/stretchr/testify/assert"

	"go.platform-mesh.io/iam-service/pkg/graph"
)

func TestDefaultPager_PaginateUserRoles_DefaultValues(t *testing.T) {
	pager := NewDefaultPager()

	// Create test data
	userRoles := createTestUserRoles(25) // 25 users

	// Test with nil page input (should use defaults)
	paginatedUsers, pageInfo := pager.PaginateUserRoles(userRoles, nil, len(userRoles))

	// Should return first 10 users (default limit)
	assert.Equal(t, 10, len(paginatedUsers))
	assert.Equal(t, 10, pageInfo.Count)
	assert.Equal(t, 25, pageInfo.TotalCount)
	assert.True(t, pageInfo.HasNextPage)
	assert.False(t, pageInfo.HasPreviousPage)

	// Verify we got the first 10 users
	for i, userRole := range paginatedUsers {
		expectedEmail := userRoles[i].User.Email
		assert.Equal(t, expectedEmail, userRole.User.Email)
	}
}

func TestDefaultPager_PaginateUserRoles_CustomLimitAndPage(t *testing.T) {
	pager := NewDefaultPager()

	// Create test data
	userRoles := createTestUserRoles(25) // 25 users

	limit := 5
	page := 3
	pageInput := &graph.PageInput{
		Limit: &limit,
		Page:  &page,
	}

	// Test with custom page input
	paginatedUsers, pageInfo := pager.PaginateUserRoles(userRoles, pageInput, len(userRoles))

	// Should return 5 users from page 3 (users 10-14, 0-indexed)
	assert.Equal(t, 5, len(paginatedUsers))
	assert.Equal(t, 5, pageInfo.Count)
	assert.Equal(t, 25, pageInfo.TotalCount)
	assert.True(t, pageInfo.HasNextPage)
	assert.True(t, pageInfo.HasPreviousPage)

	// Verify we got the correct users (page 3 with limit 5 = offset 10)
	expectedOffset := (page - 1) * limit // (3-1) * 5 = 10
	for i, userRole := range paginatedUsers {
		expectedEmail := userRoles[expectedOffset+i].User.Email
		assert.Equal(t, expectedEmail, userRole.User.Email)
	}
}

func TestDefaultPager_PaginateUserRoles_LastPage(t *testing.T) {
	pager := NewDefaultPager()

	// Create test data
	userRoles := createTestUserRoles(23) // 23 users

	limit := 10
	page := 3 // Page 3 with limit 10 should have 3 users (20-22)
	pageInput := &graph.PageInput{
		Limit: &limit,
		Page:  &page,
	}

	paginatedUsers, pageInfo := pager.PaginateUserRoles(userRoles, pageInput, len(userRoles))

	// Should return 3 users (the remainder)
	assert.Equal(t, 3, len(paginatedUsers))
	assert.Equal(t, 3, pageInfo.Count)
	assert.Equal(t, 23, pageInfo.TotalCount)
	assert.False(t, pageInfo.HasNextPage)
	assert.True(t, pageInfo.HasPreviousPage)
}

func TestDefaultPager_PaginateUserRoles_EmptyResults(t *testing.T) {
	pager := NewDefaultPager()

	// Empty user roles
	userRoles := []*graph.UserRoles{}

	paginatedUsers, pageInfo := pager.PaginateUserRoles(userRoles, nil, 0)

	assert.Equal(t, 0, len(paginatedUsers))
	assert.Equal(t, 0, pageInfo.Count)
	assert.Equal(t, 0, pageInfo.TotalCount)
	assert.False(t, pageInfo.HasNextPage)
	assert.False(t, pageInfo.HasPreviousPage)
}

func TestDefaultPager_PaginateUserRoles_PageBeyondTotal(t *testing.T) {
	pager := NewDefaultPager()

	// Create test data
	userRoles := createTestUserRoles(5) // 5 users

	limit := 10
	page := 2 // Page 2 with limit 10 and only 5 users total
	pageInput := &graph.PageInput{
		Limit: &limit,
		Page:  &page,
	}

	paginatedUsers, pageInfo := pager.PaginateUserRoles(userRoles, pageInput, len(userRoles))

	// Should return empty results but maintain correct pagination info
	assert.Equal(t, 0, len(paginatedUsers))
	assert.Equal(t, 0, pageInfo.Count)
	assert.Equal(t, 5, pageInfo.TotalCount)
	assert.False(t, pageInfo.HasNextPage)
	assert.True(t, pageInfo.HasPreviousPage)
}

func TestDefaultPager_PaginateUserRoles_InvalidValues(t *testing.T) {
	pager := NewDefaultPager()

	// Create test data
	userRoles := createTestUserRoles(15) // 15 users

	// Test with invalid limit and page values
	limit := -5
	page := 0
	pageInput := &graph.PageInput{
		Limit: &limit,
		Page:  &page,
	}

	paginatedUsers, pageInfo := pager.PaginateUserRoles(userRoles, pageInput, len(userRoles))

	// Should use default values (limit=10, page=1)
	assert.Equal(t, 10, len(paginatedUsers))
	assert.Equal(t, 10, pageInfo.Count)
	assert.Equal(t, 15, pageInfo.TotalCount)
	assert.True(t, pageInfo.HasNextPage)
	assert.False(t, pageInfo.HasPreviousPage)
}

func TestDefaultPager_PaginateUserRoles_SinglePage(t *testing.T) {
	pager := NewDefaultPager()

	// Create test data that fits in one page
	userRoles := createTestUserRoles(8) // 8 users, less than default limit of 10

	paginatedUsers, pageInfo := pager.PaginateUserRoles(userRoles, nil, len(userRoles))

	assert.Equal(t, 8, len(paginatedUsers))
	assert.Equal(t, 8, pageInfo.Count)
	assert.Equal(t, 8, pageInfo.TotalCount)
	assert.False(t, pageInfo.HasNextPage)
	assert.False(t, pageInfo.HasPreviousPage)
}

func TestDefaultPager_PaginateUserRoles_ExactPageBoundary(t *testing.T) {
	pager := NewDefaultPager()

	// Create test data that exactly matches page boundaries
	userRoles := createTestUserRoles(20) // 20 users, exactly 2 pages with default limit 10

	limit := 10
	page := 2
	pageInput := &graph.PageInput{
		Limit: &limit,
		Page:  &page,
	}

	paginatedUsers, pageInfo := pager.PaginateUserRoles(userRoles, pageInput, len(userRoles))

	assert.Equal(t, 10, len(paginatedUsers))
	assert.Equal(t, 10, pageInfo.Count)
	assert.Equal(t, 20, pageInfo.TotalCount)
	assert.False(t, pageInfo.HasNextPage) // No next page since we're on the last page
	assert.True(t, pageInfo.HasPreviousPage)
}

func TestDefaultPager_PaginateUserRoles_LargeLimit(t *testing.T) {
	pager := NewDefaultPager()

	// Create test data
	userRoles := createTestUserRoles(15) // 15 users

	// Request more than available
	limit := 100
	page := 1
	pageInput := &graph.PageInput{
		Limit: &limit,
		Page:  &page,
	}

	paginatedUsers, pageInfo := pager.PaginateUserRoles(userRoles, pageInput, len(userRoles))

	// Should return all users
	assert.Equal(t, 15, len(paginatedUsers))
	assert.Equal(t, 15, pageInfo.Count)
	assert.Equal(t, 15, pageInfo.TotalCount)
	assert.False(t, pageInfo.HasNextPage)
	assert.False(t, pageInfo.HasPreviousPage)
}

func TestDefaultPager_PaginateUserRoles_NilUserRoles(t *testing.T) {
	pager := NewDefaultPager()

	// Test with nil user roles slice
	paginatedUsers, pageInfo := pager.PaginateUserRoles(nil, nil, 0)

	assert.Equal(t, 0, len(paginatedUsers))
	assert.Equal(t, 0, pageInfo.Count)
	assert.Equal(t, 0, pageInfo.TotalCount)
	assert.False(t, pageInfo.HasNextPage)
	assert.False(t, pageInfo.HasPreviousPage)
}

func TestDefaultPager_PaginateUserRoles_CustomDefaults(t *testing.T) {
	// Create pager with custom defaults
	pager := &DefaultPager{
		defaultLimit: 5,
		defaultPage:  1,
	}

	userRoles := createTestUserRoles(12) // 12 users

	// Test with nil page input (should use custom defaults)
	paginatedUsers, pageInfo := pager.PaginateUserRoles(userRoles, nil, len(userRoles))

	// Should return first 5 users (custom default limit)
	assert.Equal(t, 5, len(paginatedUsers))
	assert.Equal(t, 5, pageInfo.Count)
	assert.Equal(t, 12, pageInfo.TotalCount)
	assert.True(t, pageInfo.HasNextPage)
	assert.False(t, pageInfo.HasPreviousPage)
}

// Tests for PaginateUsers method
func TestDefaultPager_PaginateUsers_DefaultValues(t *testing.T) {
	pager := NewDefaultPager()

	// Create test data
	users := createTestUsers(25) // 25 users

	// Test with nil page input (should use defaults)
	paginatedUsers, pageInfo := pager.PaginateUsers(users, nil, len(users))

	// Should return first 10 users (default limit)
	assert.Equal(t, 10, len(paginatedUsers))
	assert.Equal(t, 10, pageInfo.Count)
	assert.Equal(t, 25, pageInfo.TotalCount)
	assert.True(t, pageInfo.HasNextPage)
	assert.False(t, pageInfo.HasPreviousPage)

	// Verify we got the first 10 users
	for i, user := range paginatedUsers {
		expectedEmail := users[i].Email
		assert.Equal(t, expectedEmail, user.Email)
	}
}

func TestDefaultPager_PaginateUsers_CustomPageSize(t *testing.T) {
	pager := NewDefaultPager()

	users := createTestUsers(25)

	// Test with custom page size
	limit := 5
	page := 2
	pageInput := &graph.PageInput{
		Limit: &limit,
		Page:  &page,
	}

	paginatedUsers, pageInfo := pager.PaginateUsers(users, pageInput, len(users))

	// Should return 5 users starting from index 5 (page 2 with limit 5)
	assert.Equal(t, 5, len(paginatedUsers))
	assert.Equal(t, 5, pageInfo.Count)
	assert.Equal(t, 25, pageInfo.TotalCount)
	assert.True(t, pageInfo.HasNextPage)
	assert.True(t, pageInfo.HasPreviousPage)

	// Verify we got users from index 5-9
	for i, user := range paginatedUsers {
		expectedEmail := users[i+5].Email // offset by 5 for page 2
		assert.Equal(t, expectedEmail, user.Email)
	}
}

func TestDefaultPager_PaginateUsers_EmptyResult(t *testing.T) {
	pager := NewDefaultPager()

	var users []*graph.User

	paginatedUsers, pageInfo := pager.PaginateUsers(users, nil, 0)

	assert.Empty(t, paginatedUsers)
	assert.Equal(t, 0, pageInfo.Count)
	assert.Equal(t, 0, pageInfo.TotalCount)
	assert.False(t, pageInfo.HasNextPage)
	assert.False(t, pageInfo.HasPreviousPage)
}

func TestDefaultPager_PaginateUsers_OffsetBeyondTotal(t *testing.T) {
	pager := NewDefaultPager()

	users := createTestUsers(5)

	// Request page 3 with limit 10 (offset would be 20, beyond total of 5)
	limit := 10
	page := 3
	pageInput := &graph.PageInput{
		Limit: &limit,
		Page:  &page,
	}

	paginatedUsers, pageInfo := pager.PaginateUsers(users, pageInput, len(users))

	assert.Empty(t, paginatedUsers)
	assert.Equal(t, 0, pageInfo.Count)
	assert.Equal(t, 5, pageInfo.TotalCount)
	assert.False(t, pageInfo.HasNextPage)
	assert.True(t, pageInfo.HasPreviousPage)
}

func TestDefaultPager_PaginateUsers_LastPage(t *testing.T) {
	pager := NewDefaultPager()

	users := createTestUsers(23) // 23 users

	// Request page 3 with limit 10 (should get last 3 users)
	limit := 10
	page := 3
	pageInput := &graph.PageInput{
		Limit: &limit,
		Page:  &page,
	}

	paginatedUsers, pageInfo := pager.PaginateUsers(users, pageInput, len(users))

	assert.Equal(t, 3, len(paginatedUsers)) // Only 3 users on last page
	assert.Equal(t, 3, pageInfo.Count)
	assert.Equal(t, 23, pageInfo.TotalCount)
	assert.False(t, pageInfo.HasNextPage)
	assert.True(t, pageInfo.HasPreviousPage)

	// Verify we got the last 3 users
	for i, user := range paginatedUsers {
		expectedEmail := users[i+20].Email // offset by 20 for page 3
		assert.Equal(t, expectedEmail, user.Email)
	}
}

// Helper function to create test user roles data
func createTestUserRoles(count int) []*graph.UserRoles {
	userRoles := make([]*graph.UserRoles, count)
	for i := 0; i < count; i++ {
		userRoles[i] = &graph.UserRoles{
			User: &graph.User{
				UserID: "",
				Email:  "user" + string(rune('0'+i%10)) + "@example.com", // Simple pattern for testing
			},
			Roles: []*graph.Role{
				{
					ID:          "member",
					DisplayName: "Member",
					Description: "Limited access to resources",
				},
			},
		}
	}
	return userRoles
}

// Helper function to create test users data
func createTestUsers(count int) []*graph.User {
	users := make([]*graph.User, count)
	for i := 0; i < count; i++ {
		users[i] = &graph.User{
			UserID: "",
			Email:  "user" + string(rune('0'+i%10)) + "@example.com", // Simple pattern for testing
		}
	}
	return users
}
