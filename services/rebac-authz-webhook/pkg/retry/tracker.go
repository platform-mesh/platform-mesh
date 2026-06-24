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
	"time"

	"github.com/jellydator/ttlcache/v3"

	"k8s.io/klog/v2"
)

// Tracker tracks whether something should be retried.
type Tracker[K comparable] interface {
	ShouldRetry(key K) bool
	Retried(key K)
}

// ExpiringRetryTracker tracks how often something for a given key was tried and
// stops tracking when a given maximum or TTL was reached.
type ExpiringRetryTracker[K comparable] struct {
	cache *ttlcache.Cache[K, *uint]
	max   uint
}

// NewExpiringRetryTracker returns a Tracker that tracks up to max per key,
// resetting the count when no count has occurred for ttl. Internal cleanup is
// stopped when ctx in cancelled.
func NewExpiringRetryTracker[K comparable](ctx context.Context, max uint, ttl time.Duration) *ExpiringRetryTracker[K] {
	cache := ttlcache.New[K, *uint](
		ttlcache.WithTTL[K, *uint](ttl),
		ttlcache.WithDisableTouchOnHit[K, *uint](),
	)
	go func() {
		cache.Start()
		<-ctx.Done()
		cache.Stop()
	}()
	return &ExpiringRetryTracker[K]{cache: cache, max: max}
}

// retries returns the current retry conut for key, or 0 if the entry does not
// exist.
func (t *ExpiringRetryTracker[K]) retries(key K) uint {
	item := t.cache.Get(key)
	if item == nil {
		return 0
	}

	return *item.Value()
}

// ShouldRetry reports whether the key should be retried, i.e. the maximum retries have not been reached.
func (t *ExpiringRetryTracker[K]) ShouldRetry(key K) bool {
	c := t.retries(key)
	should := c < t.max
	klog.V(5).InfoS("Should retry", "key", key, "count", c, "max", t.max, "should", should)
	return should
}

// Retried records that the key was tried.
func (t *ExpiringRetryTracker[K]) Retried(key K) {
	item := t.cache.Get(key)
	if item == nil {
		u := uint(1)
		t.cache.Set(key, &u, ttlcache.DefaultTTL)
		klog.V(5).InfoS("Recorded retry", "key", key, "count", 1, "max", t.max)
		return
	}
	*item.Value()++
	klog.V(5).InfoS("Recorded retry", "key", key, "count", *item.Value(), "max", t.max)
}

var _ Tracker[string] = (*ExpiringRetryTracker[string])(nil)
