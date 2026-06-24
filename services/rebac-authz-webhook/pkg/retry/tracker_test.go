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

package retry

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCountingTracker_ShouldRetry_AllowsRetriesUnderMax(t *testing.T) {
	tracker := NewExpiringRetryTracker[string](context.Background(), 3, time.Hour)
	key := "test-key"

	for i := 0; i < 3; i++ {
		assert.Truef(t, tracker.ShouldRetry(key), "Should return true the first three times")
		tracker.Retried(key)
	}

	assert.False(t, tracker.ShouldRetry(key), "Should return false after max retries")
}

func TestCountingTracker_ShouldRetry_ReturnsTrueForNewKey(t *testing.T) {
	tracker := NewExpiringRetryTracker[string](context.Background(), 1, time.Hour)

	assert.True(t, tracker.ShouldRetry("new-key"), "ShouldRetry(new-key)")
}

func TestCountingTracker_KeysTrackedIndependently(t *testing.T) {
	tracker := NewExpiringRetryTracker[string](context.Background(), 1, time.Hour)

	tracker.Retried("key-a")
	tracker.Retried("key-b")

	assert.False(t, tracker.ShouldRetry("key-a"))
	assert.False(t, tracker.ShouldRetry("key-b"))
	assert.True(t, tracker.ShouldRetry("key-c"))
}

func TestCountingTracker_CountResetsAfterTTL(t *testing.T) {
	ttl := 500 * time.Millisecond
	tracker := NewExpiringRetryTracker[string](context.Background(), 2, ttl)
	key := "key"

	// Returns false within TTL
	tracker.Retried(key)
	tracker.Retried(key)
	assert.False(t, tracker.ShouldRetry(key))

	time.Sleep(ttl + 100*time.Millisecond)

	// Reset after TTL
	assert.True(t, tracker.ShouldRetry(key))
	tracker.Retried(key)
	assert.True(t, tracker.ShouldRetry(key))
	tracker.Retried(key)
	assert.False(t, tracker.ShouldRetry(key))
}

func TestCountingTracker_ConcurrentAccess(t *testing.T) {
	tracker := NewExpiringRetryTracker[int](context.Background(), 10, time.Hour)
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := id
			for j := 0; j < 10; j++ {
				tracker.ShouldRetry(key)
				tracker.Retried(key)
			}
		}(i)
	}
	wg.Wait()

	for i := 0; i < 5; i++ {
		assert.Falsef(t, tracker.ShouldRetry(i), "ShouldRetry(%d) after 10 retries with max 10", i)
	}
}

func TestExpiringRetryTracker_AutoExpiration_DeletesExpiredElements(t *testing.T) {
	ttl := 30 * time.Millisecond
	tracker := NewExpiringRetryTracker[string](context.Background(), 10, ttl)

	tracker.Retried("key-1")
	tracker.Retried("key-2")
	tracker.Retried("key-3")
	assert.Equal(t, 3, tracker.cache.Len(), "before expiry")

	time.Sleep(ttl * 2)

	assert.Equal(t, 0, tracker.cache.Len(), "expired elements should be deleted by cache auto-expiration")
}
