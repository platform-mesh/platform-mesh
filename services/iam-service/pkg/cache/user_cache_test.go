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

package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.platform-mesh.io/iam-service/pkg/graph"
	"k8s.io/utils/ptr"
)

func TestNewUserCache(t *testing.T) {
	ttl := 5 * time.Minute
	cache := NewUserCache(ttl)

	assert.NotNil(t, cache)
	assert.NotNil(t, cache.cache)
	assert.Equal(t, ttl, cache.ttl)
	assert.Equal(t, 0, cache.Size())
}

func TestUserCache_Get_Set(t *testing.T) {
	cache := NewUserCache(5 * time.Minute)

	user := &graph.User{
		UserID:    "user123",
		Email:     "test@example.com",
		FirstName: ptr.To("John"),
		LastName:  ptr.To("Doe"),
	}

	// Test Get non-existent user
	result := cache.Get("realm1", "test@example.com")
	assert.Nil(t, result)

	// Test Set and Get
	cache.Set("realm1", "test@example.com", user)
	result = cache.Get("realm1", "test@example.com")
	require.NotNil(t, result)
	assert.Equal(t, user.UserID, result.UserID)
	assert.Equal(t, user.Email, result.Email)
	assert.Equal(t, user.FirstName, result.FirstName)
	assert.Equal(t, user.LastName, result.LastName)

	// Test realm isolation - different realm should not find user
	result = cache.Get("realm2", "test@example.com")
	assert.Nil(t, result)
}

func TestUserCache_GetMany_SetMany(t *testing.T) {
	cache := NewUserCache(5 * time.Minute)

	users := map[string]*graph.User{
		"user1@example.com": {
			UserID:    "user1",
			Email:     "user1@example.com",
			FirstName: ptr.To("John"),
			LastName:  ptr.To("Doe"),
		},
		"user2@example.com": {
			UserID:    "user2",
			Email:     "user2@example.com",
			FirstName: ptr.To("Jane"),
			LastName:  ptr.To("Smith"),
		},
		"user3@example.com": {
			UserID:    "user3",
			Email:     "user3@example.com",
			FirstName: ptr.To("Bob"),
			LastName:  ptr.To("Johnson"),
		},
	}

	// Test GetMany with empty cache
	emails := []string{"user1@example.com", "user2@example.com", "user3@example.com"}
	found, missing := cache.GetMany("realm1", emails)
	assert.Empty(t, found)
	assert.ElementsMatch(t, emails, missing)

	// Test SetMany
	cache.SetMany("realm1", users)
	assert.Equal(t, 3, cache.Size())

	// Test GetMany with all cached
	found, missing = cache.GetMany("realm1", emails)
	assert.Len(t, found, 3)
	assert.Empty(t, missing)
	assert.Equal(t, "user1", found["user1@example.com"].UserID)
	assert.Equal(t, "user2", found["user2@example.com"].UserID)
	assert.Equal(t, "user3", found["user3@example.com"].UserID)

	// Test GetMany with partial cache hits
	emails = []string{"user1@example.com", "user4@example.com", "user2@example.com"}
	found, missing = cache.GetMany("realm1", emails)
	assert.Len(t, found, 2)
	assert.Len(t, missing, 1)
	assert.Contains(t, found, "user1@example.com")
	assert.Contains(t, found, "user2@example.com")
	assert.Contains(t, missing, "user4@example.com")

	// Test realm isolation
	found, missing = cache.GetMany("realm2", []string{"user1@example.com"})
	assert.Empty(t, found)
	assert.Contains(t, missing, "user1@example.com")
}

func TestUserCache_Delete(t *testing.T) {
	cache := NewUserCache(5 * time.Minute)

	user := &graph.User{
		UserID: "user123",
		Email:  "test@example.com",
	}

	// Set user
	cache.Set("realm1", "test@example.com", user)
	assert.Equal(t, 1, cache.Size())

	// Verify user exists
	result := cache.Get("realm1", "test@example.com")
	assert.NotNil(t, result)

	// Delete user
	cache.Delete("realm1", "test@example.com")

	// Verify user is deleted
	result = cache.Get("realm1", "test@example.com")
	assert.Nil(t, result)
	assert.Equal(t, 0, cache.Size())

	// Delete non-existent user should not panic
	cache.Delete("realm1", "nonexistent@example.com")
}

func TestUserCache_Clear(t *testing.T) {
	cache := NewUserCache(5 * time.Minute)

	users := map[string]*graph.User{
		"user1@example.com": {UserID: "user1", Email: "user1@example.com"},
		"user2@example.com": {UserID: "user2", Email: "user2@example.com"},
		"user3@example.com": {UserID: "user3", Email: "user3@example.com"},
	}

	// Add users
	cache.SetMany("realm1", users)
	assert.Equal(t, 3, cache.Size())

	// Clear cache
	cache.Clear()
	assert.Equal(t, 0, cache.Size())

	// Verify all users are gone
	for email := range users {
		result := cache.Get("realm1", email)
		assert.Nil(t, result)
	}
}

func TestUserCache_Size(t *testing.T) {
	cache := NewUserCache(5 * time.Minute)

	// Empty cache
	assert.Equal(t, 0, cache.Size())

	// Add one user
	cache.Set("realm1", "user1@example.com", &graph.User{UserID: "user1"})
	assert.Equal(t, 1, cache.Size())

	// Add more users
	cache.Set("realm1", "user2@example.com", &graph.User{UserID: "user2"})
	cache.Set("realm2", "user3@example.com", &graph.User{UserID: "user3"})
	assert.Equal(t, 3, cache.Size())

	// Delete one user
	cache.Delete("realm1", "user1@example.com")
	assert.Equal(t, 2, cache.Size())

	// Clear all
	cache.Clear()
	assert.Equal(t, 0, cache.Size())
}

func TestUserCache_Stats(t *testing.T) {
	ttl := 5 * time.Minute
	cache := NewUserCache(ttl)

	// Initial stats
	stats := cache.Stats()
	assert.Equal(t, 0, stats.Total)
	assert.Equal(t, 0, stats.Active)
	assert.Equal(t, 0, stats.Expired)
	assert.Equal(t, ttl, stats.TTL)
	assert.Equal(t, uint64(0), stats.Hits)
	assert.Equal(t, uint64(0), stats.Misses)

	// Add users
	cache.Set("realm1", "user1@example.com", &graph.User{UserID: "user1"})
	cache.Set("realm1", "user2@example.com", &graph.User{UserID: "user2"})

	stats = cache.Stats()
	assert.Equal(t, 2, stats.Total)
	assert.Equal(t, 2, stats.Active)
	assert.Equal(t, 0, stats.Expired)
	assert.Equal(t, ttl, stats.TTL)

	// Generate some hits and misses
	cache.Get("realm1", "user1@example.com")       // hit
	cache.Get("realm1", "nonexistent@example.com") // miss

	stats = cache.Stats()
	assert.Greater(t, stats.Hits, uint64(0))
	assert.Greater(t, stats.Misses, uint64(0))
}

func TestUserCache_TTLExpiration(t *testing.T) {
	// Use very short TTL for testing
	shortTTL := 50 * time.Millisecond
	cache := NewUserCache(shortTTL)

	user := &graph.User{
		UserID: "user123",
		Email:  "test@example.com",
	}

	// Set user
	cache.Set("realm1", "test@example.com", user)

	// Immediately retrieve - should be there
	result := cache.Get("realm1", "test@example.com")
	assert.NotNil(t, result)
	assert.Equal(t, user.UserID, result.UserID)

	// Wait for expiration (add some buffer for cleanup)
	time.Sleep(shortTTL + 10*time.Millisecond)

	// Should be expired and automatically cleaned up
	result = cache.Get("realm1", "test@example.com")
	assert.Nil(t, result)
}

func TestUserCache_RealmIsolation(t *testing.T) {
	cache := NewUserCache(5 * time.Minute)

	user1 := &graph.User{UserID: "user1", Email: "test@example.com"}
	user2 := &graph.User{UserID: "user2", Email: "test@example.com"}

	// Set same email in different realms
	cache.Set("realm1", "test@example.com", user1)
	cache.Set("realm2", "test@example.com", user2)

	// Verify realm isolation
	result1 := cache.Get("realm1", "test@example.com")
	result2 := cache.Get("realm2", "test@example.com")

	assert.NotNil(t, result1)
	assert.NotNil(t, result2)
	assert.Equal(t, "user1", result1.UserID)
	assert.Equal(t, "user2", result2.UserID)

	// Delete from one realm shouldn't affect the other
	cache.Delete("realm1", "test@example.com")

	result1 = cache.Get("realm1", "test@example.com")
	result2 = cache.Get("realm2", "test@example.com")

	assert.Nil(t, result1)
	assert.NotNil(t, result2)
	assert.Equal(t, "user2", result2.UserID)
}

func TestUserCache_BuildKey(t *testing.T) {
	cache := NewUserCache(5 * time.Minute)

	key := cache.buildKey("realm1", "user@example.com")
	assert.Equal(t, "realm1:user@example.com", key)

	// Test with empty values
	key = cache.buildKey("", "user@example.com")
	assert.Equal(t, ":user@example.com", key)

	key = cache.buildKey("realm1", "")
	assert.Equal(t, "realm1:", key)
}

func TestUserCache_GetMany_EmptySlice(t *testing.T) {
	cache := NewUserCache(5 * time.Minute)

	// Test with empty email slice
	found, missing := cache.GetMany("realm1", []string{})
	assert.Empty(t, found)
	assert.Empty(t, missing)

	// Test with nil slice
	found, missing = cache.GetMany("realm1", nil)
	assert.Empty(t, found)
	assert.Empty(t, missing)
}

func TestUserCache_SetMany_EmptyMap(t *testing.T) {
	cache := NewUserCache(5 * time.Minute)

	// Test with empty map
	cache.SetMany("realm1", map[string]*graph.User{})
	assert.Equal(t, 0, cache.Size())

	// Test with nil map
	cache.SetMany("realm1", nil)
	assert.Equal(t, 0, cache.Size())
}
