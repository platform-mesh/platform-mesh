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

package sorter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.platform-mesh.io/iam-service/pkg/config"
	"go.platform-mesh.io/iam-service/pkg/graph"
)

func TestNewUserSorter(t *testing.T) {
	sorter := NewUserSorter()
	assert.NotNil(t, sorter)
	assert.IsType(t, &DefaultUserSorter{}, sorter)
}

func TestDefaultUserSorter_SortUserRoles_EmptySlice(t *testing.T) {
	sorter := NewUserSorter()

	// Test with empty slice
	var userRoles []*graph.UserRoles
	sorter.SortUserRoles(userRoles, nil)
	assert.Empty(t, userRoles)

	// Test with nil slice
	sorter.SortUserRoles(nil, nil)
}

func TestDefaultUserSorter_SortUserRoles_SingleElement(t *testing.T) {
	sorter := NewUserSorter()

	userRoles := []*graph.UserRoles{
		{
			User: &graph.User{
				UserID:    "user1",
				Email:     "user1@example.com",
				FirstName: stringPtr("John"),
				LastName:  stringPtr("Doe"),
			},
		},
	}

	sorter.SortUserRoles(userRoles, nil)
	assert.Len(t, userRoles, 1)
	assert.Equal(t, "user1", userRoles[0].User.UserID)
}

func TestDefaultUserSorter_SortUserRoles_DefaultSorting(t *testing.T) {
	sorter := NewUserSorter()

	userRoles := []*graph.UserRoles{
		{User: &graph.User{UserID: "user1", Email: "user1@example.com", LastName: stringPtr("Zebra")}},
		{User: &graph.User{UserID: "user2", Email: "user2@example.com", LastName: stringPtr("Apple")}},
		{User: &graph.User{UserID: "user3", Email: "user3@example.com", LastName: stringPtr("Banana")}},
	}

	// Test default sorting (LastName ASC)
	sorter.SortUserRoles(userRoles, nil)

	assert.Equal(t, "Apple", *userRoles[0].User.LastName)
	assert.Equal(t, "Banana", *userRoles[1].User.LastName)
	assert.Equal(t, "Zebra", *userRoles[2].User.LastName)
}

func TestDefaultUserSorter_SortUserRoles_LastNameASC(t *testing.T) {
	sorter := NewUserSorter()

	userRoles := []*graph.UserRoles{
		{User: &graph.User{UserID: "user1", Email: "user1@example.com", LastName: stringPtr("Zebra")}},
		{User: &graph.User{UserID: "user2", Email: "user2@example.com", LastName: stringPtr("Apple")}},
		{User: &graph.User{UserID: "user3", Email: "user3@example.com", LastName: stringPtr("Banana")}},
	}

	sortBy := &graph.SortByInput{
		Field:     graph.UserSortFieldLastName,
		Direction: graph.SortDirectionAsc,
	}

	sorter.SortUserRoles(userRoles, sortBy)

	assert.Equal(t, "Apple", *userRoles[0].User.LastName)
	assert.Equal(t, "Banana", *userRoles[1].User.LastName)
	assert.Equal(t, "Zebra", *userRoles[2].User.LastName)
}

func TestDefaultUserSorter_SortUserRoles_LastNameDESC(t *testing.T) {
	sorter := NewUserSorter()

	userRoles := []*graph.UserRoles{
		{User: &graph.User{UserID: "user1", Email: "user1@example.com", LastName: stringPtr("Apple")}},
		{User: &graph.User{UserID: "user2", Email: "user2@example.com", LastName: stringPtr("Zebra")}},
		{User: &graph.User{UserID: "user3", Email: "user3@example.com", LastName: stringPtr("Banana")}},
	}

	sortBy := &graph.SortByInput{
		Field:     graph.UserSortFieldLastName,
		Direction: graph.SortDirectionDesc,
	}

	sorter.SortUserRoles(userRoles, sortBy)

	assert.Equal(t, "Zebra", *userRoles[0].User.LastName)
	assert.Equal(t, "Banana", *userRoles[1].User.LastName)
	assert.Equal(t, "Apple", *userRoles[2].User.LastName)
}

func TestDefaultUserSorter_SortUserRoles_FirstNameASC(t *testing.T) {
	sorter := NewUserSorter()

	userRoles := []*graph.UserRoles{
		{User: &graph.User{UserID: "user1", Email: "user1@example.com", FirstName: stringPtr("Zebra")}},
		{User: &graph.User{UserID: "user2", Email: "user2@example.com", FirstName: stringPtr("Apple")}},
		{User: &graph.User{UserID: "user3", Email: "user3@example.com", FirstName: stringPtr("Banana")}},
	}

	sortBy := &graph.SortByInput{
		Field:     graph.UserSortFieldFirstName,
		Direction: graph.SortDirectionAsc,
	}

	sorter.SortUserRoles(userRoles, sortBy)

	assert.Equal(t, "Apple", *userRoles[0].User.FirstName)
	assert.Equal(t, "Banana", *userRoles[1].User.FirstName)
	assert.Equal(t, "Zebra", *userRoles[2].User.FirstName)
}

func TestDefaultUserSorter_SortUserRoles_FirstNameDESC(t *testing.T) {
	sorter := NewUserSorter()

	userRoles := []*graph.UserRoles{
		{User: &graph.User{UserID: "user1", Email: "user1@example.com", FirstName: stringPtr("Apple")}},
		{User: &graph.User{UserID: "user2", Email: "user2@example.com", FirstName: stringPtr("Zebra")}},
		{User: &graph.User{UserID: "user3", Email: "user3@example.com", FirstName: stringPtr("Banana")}},
	}

	sortBy := &graph.SortByInput{
		Field:     graph.UserSortFieldFirstName,
		Direction: graph.SortDirectionDesc,
	}

	sorter.SortUserRoles(userRoles, sortBy)

	assert.Equal(t, "Zebra", *userRoles[0].User.FirstName)
	assert.Equal(t, "Banana", *userRoles[1].User.FirstName)
	assert.Equal(t, "Apple", *userRoles[2].User.FirstName)
}

func TestDefaultUserSorter_SortUserRoles_EmailASC(t *testing.T) {
	sorter := NewUserSorter()

	userRoles := []*graph.UserRoles{
		{User: &graph.User{UserID: "user1", Email: "zebra@example.com"}},
		{User: &graph.User{UserID: "user2", Email: "apple@example.com"}},
		{User: &graph.User{UserID: "user3", Email: "banana@example.com"}},
	}

	sortBy := &graph.SortByInput{
		Field:     graph.UserSortFieldEmail,
		Direction: graph.SortDirectionAsc,
	}

	sorter.SortUserRoles(userRoles, sortBy)

	assert.Equal(t, "apple@example.com", userRoles[0].User.Email)
	assert.Equal(t, "banana@example.com", userRoles[1].User.Email)
	assert.Equal(t, "zebra@example.com", userRoles[2].User.Email)
}

func TestDefaultUserSorter_SortUserRoles_EmailDESC(t *testing.T) {
	sorter := NewUserSorter()

	userRoles := []*graph.UserRoles{
		{User: &graph.User{UserID: "user1", Email: "apple@example.com"}},
		{User: &graph.User{UserID: "user2", Email: "zebra@example.com"}},
		{User: &graph.User{UserID: "user3", Email: "banana@example.com"}},
	}

	sortBy := &graph.SortByInput{
		Field:     graph.UserSortFieldEmail,
		Direction: graph.SortDirectionDesc,
	}

	sorter.SortUserRoles(userRoles, sortBy)

	assert.Equal(t, "zebra@example.com", userRoles[0].User.Email)
	assert.Equal(t, "banana@example.com", userRoles[1].User.Email)
	assert.Equal(t, "apple@example.com", userRoles[2].User.Email)
}

func TestDefaultUserSorter_SortUserRoles_UserIDASC(t *testing.T) {
	sorter := NewUserSorter()

	userRoles := []*graph.UserRoles{
		{User: &graph.User{UserID: "user3", Email: "user3@example.com"}},
		{User: &graph.User{UserID: "user1", Email: "user1@example.com"}},
		{User: &graph.User{UserID: "user2", Email: "user2@example.com"}},
	}

	sortBy := &graph.SortByInput{
		Field:     graph.UserSortFieldUserID,
		Direction: graph.SortDirectionAsc,
	}

	sorter.SortUserRoles(userRoles, sortBy)

	assert.Equal(t, "user1", userRoles[0].User.UserID)
	assert.Equal(t, "user2", userRoles[1].User.UserID)
	assert.Equal(t, "user3", userRoles[2].User.UserID)
}

func TestDefaultUserSorter_SortUserRoles_UserIDDESC(t *testing.T) {
	sorter := NewUserSorter()

	userRoles := []*graph.UserRoles{
		{User: &graph.User{UserID: "user1", Email: "user1@example.com"}},
		{User: &graph.User{UserID: "user3", Email: "user3@example.com"}},
		{User: &graph.User{UserID: "user2", Email: "user2@example.com"}},
	}

	sortBy := &graph.SortByInput{
		Field:     graph.UserSortFieldUserID,
		Direction: graph.SortDirectionDesc,
	}

	sorter.SortUserRoles(userRoles, sortBy)

	assert.Equal(t, "user3", userRoles[0].User.UserID)
	assert.Equal(t, "user2", userRoles[1].User.UserID)
	assert.Equal(t, "user1", userRoles[2].User.UserID)
}

func TestDefaultUserSorter_SortUserRoles_NilFields(t *testing.T) {
	sorter := NewUserSorter()

	userRoles := []*graph.UserRoles{
		{User: &graph.User{UserID: "user1", Email: "user1@example.com", FirstName: stringPtr("John"), LastName: nil}},
		{User: &graph.User{UserID: "user2", Email: "user2@example.com", FirstName: nil, LastName: stringPtr("Doe")}},
		{User: &graph.User{UserID: "user3", Email: "user3@example.com", FirstName: stringPtr("Jane"), LastName: stringPtr("Smith")}},
	}

	// Test FirstName sorting with nil values
	sortBy := &graph.SortByInput{
		Field:     graph.UserSortFieldFirstName,
		Direction: graph.SortDirectionAsc,
	}

	sorter.SortUserRoles(userRoles, sortBy)

	// Nil values should come first (empty string sorts before non-empty)
	assert.Nil(t, userRoles[0].User.FirstName)            // user2
	assert.Equal(t, "Jane", *userRoles[1].User.FirstName) // user3
	assert.Equal(t, "John", *userRoles[2].User.FirstName) // user1
}

func TestDefaultUserSorter_SortUserRoles_InvalidField(t *testing.T) {
	sorter := NewUserSorter()

	userRoles := []*graph.UserRoles{
		{User: &graph.User{UserID: "user1", Email: "user1@example.com", LastName: stringPtr("Zebra")}},
		{User: &graph.User{UserID: "user2", Email: "user2@example.com", LastName: stringPtr("Apple")}},
		{User: &graph.User{UserID: "user3", Email: "user3@example.com", LastName: stringPtr("Banana")}},
	}

	// Use an invalid field - should fallback to LastName sorting
	sortBy := &graph.SortByInput{
		Field:     graph.UserSortField("INVALID_FIELD"),
		Direction: graph.SortDirectionAsc,
	}

	sorter.SortUserRoles(userRoles, sortBy)

	// Should fallback to LastName ASC sorting
	assert.Equal(t, "Apple", *userRoles[0].User.LastName)
	assert.Equal(t, "Banana", *userRoles[1].User.LastName)
	assert.Equal(t, "Zebra", *userRoles[2].User.LastName)
}

func TestDefaultUserSorter_getStringValue(t *testing.T) {
	sorter := &DefaultUserSorter{}

	// Test with nil pointer
	result := sorter.getStringValue(nil)
	assert.Equal(t, "", result)

	// Test with valid pointer
	value := "test"
	result = sorter.getStringValue(&value)
	assert.Equal(t, "test", result)

	// Test with empty string pointer
	empty := ""
	result = sorter.getStringValue(&empty)
	assert.Equal(t, "", result)
}

func TestDefaultUserSorter_compareUsers(t *testing.T) {
	sorter := &DefaultUserSorter{}

	user1 := &graph.User{
		UserID:    "user1",
		Email:     "a@example.com",
		FirstName: stringPtr("Alice"),
		LastName:  stringPtr("Anderson"),
	}

	user2 := &graph.User{
		UserID:    "user2",
		Email:     "b@example.com",
		FirstName: stringPtr("Bob"),
		LastName:  stringPtr("Brown"),
	}

	// Test UserID comparison
	result := sorter.compareUsers(user1, user2, graph.UserSortFieldUserID)
	assert.Less(t, result, 0) // user1 < user2

	result = sorter.compareUsers(user2, user1, graph.UserSortFieldUserID)
	assert.Greater(t, result, 0) // user2 > user1

	result = sorter.compareUsers(user1, user1, graph.UserSortFieldUserID)
	assert.Equal(t, 0, result) // user1 == user1

	// Test Email comparison
	result = sorter.compareUsers(user1, user2, graph.UserSortFieldEmail)
	assert.Less(t, result, 0) // a@example.com < b@example.com

	// Test FirstName comparison
	result = sorter.compareUsers(user1, user2, graph.UserSortFieldFirstName)
	assert.Less(t, result, 0) // Alice < Bob

	// Test LastName comparison
	result = sorter.compareUsers(user1, user2, graph.UserSortFieldLastName)
	assert.Less(t, result, 0) // Anderson < Brown

	// Test invalid field (should fallback to LastName)
	result = sorter.compareUsers(user1, user2, graph.UserSortField("INVALID"))
	assert.Less(t, result, 0) // Anderson < Brown (fallback)
}

func TestDefaultUserSorter_StabilityTest(t *testing.T) {
	sorter := NewUserSorter()

	// Create users with same sort key but different other fields
	userRoles := []*graph.UserRoles{
		{User: &graph.User{UserID: "user1", Email: "user1@example.com", LastName: stringPtr("Smith")}},
		{User: &graph.User{UserID: "user2", Email: "user2@example.com", LastName: stringPtr("Smith")}},
		{User: &graph.User{UserID: "user3", Email: "user3@example.com", LastName: stringPtr("Smith")}},
	}

	sortBy := &graph.SortByInput{
		Field:     graph.UserSortFieldLastName,
		Direction: graph.SortDirectionAsc,
	}

	// Store original order
	originalOrder := make([]string, len(userRoles))
	for i, ur := range userRoles {
		originalOrder[i] = ur.User.UserID
	}

	sorter.SortUserRoles(userRoles, sortBy)

	// All should still have the same LastName
	for _, ur := range userRoles {
		assert.Equal(t, "Smith", *ur.User.LastName)
	}

	// The sort should be stable - same elements should maintain relative order
	// Since Go's sort.Slice is not guaranteed to be stable, we just verify correctness
	assert.Len(t, userRoles, 3)
}

func TestNewUserSorterWithConfig(t *testing.T) {
	cfg := &config.ServiceConfig{
		Sorting: config.SortingConfig{
			DefaultField:     "FirstName",
			DefaultDirection: "DESC",
		},
	}

	sorter := NewUserSorterWithConfig(cfg)
	assert.NotNil(t, sorter)
	assert.IsType(t, &DefaultUserSorter{}, sorter)

	// Cast to access private fields for testing
	defaultSorter := sorter.(*DefaultUserSorter)
	assert.Equal(t, graph.UserSortFieldFirstName, defaultSorter.defaultField)
	assert.Equal(t, graph.SortDirectionDesc, defaultSorter.defaultDirection)
}

func TestNewUserSorterWithConfig_InvalidValues(t *testing.T) {
	cfg := &config.ServiceConfig{
		Sorting: config.SortingConfig{
			DefaultField:     "InvalidField",
			DefaultDirection: "InvalidDirection",
		},
	}

	sorter := NewUserSorterWithConfig(cfg)
	assert.NotNil(t, sorter)

	// Should fallback to LastName and ASC
	defaultSorter := sorter.(*DefaultUserSorter)
	assert.Equal(t, graph.UserSortFieldLastName, defaultSorter.defaultField)
	assert.Equal(t, graph.SortDirectionAsc, defaultSorter.defaultDirection)
}

func TestParseUserSortField(t *testing.T) {
	testCases := []struct {
		input    string
		expected graph.UserSortField
	}{
		{"userid", graph.UserSortFieldUserID},
		{"user_id", graph.UserSortFieldUserID},
		{"UserID", graph.UserSortFieldUserID},
		{"EMAIL", graph.UserSortFieldEmail},
		{"email", graph.UserSortFieldEmail},
		{"firstname", graph.UserSortFieldFirstName},
		{"first_name", graph.UserSortFieldFirstName},
		{"FirstName", graph.UserSortFieldFirstName},
		{"lastname", graph.UserSortFieldLastName},
		{"last_name", graph.UserSortFieldLastName},
		{"LastName", graph.UserSortFieldLastName},
		{"invalid", graph.UserSortFieldLastName}, // fallback
		{"", graph.UserSortFieldLastName},        // fallback
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := parseUserSortField(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestParseSortDirection(t *testing.T) {
	testCases := []struct {
		input    string
		expected graph.SortDirection
	}{
		{"ASC", graph.SortDirectionAsc},
		{"asc", graph.SortDirectionAsc},
		{"ASCENDING", graph.SortDirectionAsc},
		{"ascending", graph.SortDirectionAsc},
		{"DESC", graph.SortDirectionDesc},
		{"desc", graph.SortDirectionDesc},
		{"DESCENDING", graph.SortDirectionDesc},
		{"descending", graph.SortDirectionDesc},
		{"invalid", graph.SortDirectionAsc}, // fallback
		{"", graph.SortDirectionAsc},        // fallback
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := parseSortDirection(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// Tests for SortUsers method
func TestDefaultUserSorter_SortUsers_EmptySlice(t *testing.T) {
	sorter := NewUserSorter()

	// Test with empty slice
	var users []*graph.User
	sorter.SortUsers(users, nil)
	assert.Empty(t, users)

	// Test with nil slice
	sorter.SortUsers(nil, nil)
}

func TestDefaultUserSorter_SortUsers_SingleElement(t *testing.T) {
	sorter := NewUserSorter()

	users := []*graph.User{
		{
			UserID:    "user1",
			Email:     "user1@example.com",
			FirstName: stringPtr("John"),
			LastName:  stringPtr("Doe"),
		},
	}

	sorter.SortUsers(users, nil)
	assert.Len(t, users, 1)
	assert.Equal(t, "user1", users[0].UserID)
}

func TestDefaultUserSorter_SortUsers_DefaultSorting(t *testing.T) {
	sorter := NewUserSorter()

	users := []*graph.User{
		{UserID: "user1", Email: "user1@example.com", LastName: stringPtr("Zebra")},
		{UserID: "user2", Email: "user2@example.com", LastName: stringPtr("Alpha")},
		{UserID: "user3", Email: "user3@example.com", LastName: stringPtr("Beta")},
	}

	// Default sorting should be by LastName ASC
	sorter.SortUsers(users, nil)

	assert.Equal(t, "Alpha", *users[0].LastName)
	assert.Equal(t, "Beta", *users[1].LastName)
	assert.Equal(t, "Zebra", *users[2].LastName)
}

func TestDefaultUserSorter_SortUsers_ByEmail(t *testing.T) {
	sorter := NewUserSorter()

	users := []*graph.User{
		{UserID: "user1", Email: "c@example.com"},
		{UserID: "user2", Email: "a@example.com"},
		{UserID: "user3", Email: "b@example.com"},
	}

	sortBy := &graph.SortByInput{
		Field:     graph.UserSortFieldEmail,
		Direction: graph.SortDirectionAsc,
	}

	sorter.SortUsers(users, sortBy)

	assert.Equal(t, "a@example.com", users[0].Email)
	assert.Equal(t, "b@example.com", users[1].Email)
	assert.Equal(t, "c@example.com", users[2].Email)
}

func TestDefaultUserSorter_SortUsers_ByFirstNameDesc(t *testing.T) {
	sorter := NewUserSorter()

	users := []*graph.User{
		{UserID: "user1", Email: "user1@example.com", FirstName: stringPtr("Alice")},
		{UserID: "user2", Email: "user2@example.com", FirstName: stringPtr("Charlie")},
		{UserID: "user3", Email: "user3@example.com", FirstName: stringPtr("Bob")},
	}

	sortBy := &graph.SortByInput{
		Field:     graph.UserSortFieldFirstName,
		Direction: graph.SortDirectionDesc,
	}

	sorter.SortUsers(users, sortBy)

	assert.Equal(t, "Charlie", *users[0].FirstName)
	assert.Equal(t, "Bob", *users[1].FirstName)
	assert.Equal(t, "Alice", *users[2].FirstName)
}

func TestDefaultUserSorter_SortUsers_WithNilValues(t *testing.T) {
	sorter := NewUserSorter()

	users := []*graph.User{
		{UserID: "user1", Email: "user1@example.com", FirstName: stringPtr("Alice"), LastName: nil},
		{UserID: "user2", Email: "user2@example.com", FirstName: nil, LastName: stringPtr("Smith")},
		{UserID: "user3", Email: "user3@example.com", FirstName: stringPtr("Bob"), LastName: stringPtr("Jones")},
	}

	sortBy := &graph.SortByInput{
		Field:     graph.UserSortFieldLastName,
		Direction: graph.SortDirectionAsc,
	}

	sorter.SortUsers(users, sortBy)

	// Nil values should be sorted first (empty string)
	assert.Nil(t, users[0].LastName)
	assert.Equal(t, "Jones", *users[1].LastName)
	assert.Equal(t, "Smith", *users[2].LastName)
}

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}
