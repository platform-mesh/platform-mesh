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
	"time"

	"github.com/jellydator/ttlcache/v3"
	"go.platform-mesh.io/iam-service/pkg/graph"
)

// UserCache provides in-memory caching for user data with TTL support
type UserCache struct {
	cache *ttlcache.Cache[string, *graph.User]
	ttl   time.Duration
}

// NewUserCache creates a new user cache with the specified TTL
func NewUserCache(ttl time.Duration) *UserCache {
	cache := ttlcache.New(
		ttlcache.WithTTL[string, *graph.User](ttl),
	)

	// Start automatic expired item deletion
	go cache.Start()

	return &UserCache{
		cache: cache,
		ttl:   ttl,
	}
}

// Get retrieves a user from cache by realm and email
// Returns nil if not found or expired
func (c *UserCache) Get(realm, email string) *graph.User {
	key := c.buildKey(realm, email)
	item := c.cache.Get(key)
	if item == nil {
		return nil
	}
	return item.Value()
}

// GetMany retrieves multiple users from cache by realm and emails
// Returns a map of found users and a slice of missing emails
func (c *UserCache) GetMany(realm string, emails []string) (map[string]*graph.User, []string) {
	found := make(map[string]*graph.User)
	missing := make([]string, 0)

	for _, email := range emails {
		key := c.buildKey(realm, email)
		item := c.cache.Get(key)
		if item == nil {
			missing = append(missing, email)
			continue
		}

		found[email] = item.Value()
	}

	return found, missing
}

// Set stores a user in cache with TTL
func (c *UserCache) Set(realm, email string, user *graph.User) {
	key := c.buildKey(realm, email)
	c.cache.Set(key, user, ttlcache.DefaultTTL)
}

// SetMany stores multiple users in cache with TTL
func (c *UserCache) SetMany(realm string, users map[string]*graph.User) {
	for email, user := range users {
		key := c.buildKey(realm, email)
		c.cache.Set(key, user, ttlcache.DefaultTTL)
	}
}

// Delete removes a user from cache
func (c *UserCache) Delete(realm, email string) {
	key := c.buildKey(realm, email)
	c.cache.Delete(key)
}

// Clear removes all users from cache
func (c *UserCache) Clear() {
	c.cache.DeleteAll()
}

// Size returns the number of cached users
func (c *UserCache) Size() int {
	return int(c.cache.Len())
}

// Stats returns cache statistics
func (c *UserCache) Stats() CacheStats {
	metrics := c.cache.Metrics()

	return CacheStats{
		Total:   int(c.cache.Len()),
		Active:  int(c.cache.Len()), // ttlcache automatically removes expired items
		Expired: 0,                  // expired items are automatically cleaned up
		TTL:     c.ttl,
		Hits:    metrics.Hits,
		Misses:  metrics.Misses,
	}
}

// CacheStats represents cache statistics
type CacheStats struct {
	Total   int
	Active  int
	Expired int
	TTL     time.Duration
	Hits    uint64
	Misses  uint64
}

// buildKey creates a cache key from realm and email
func (c *UserCache) buildKey(realm, email string) string {
	return realm + ":" + email
}
