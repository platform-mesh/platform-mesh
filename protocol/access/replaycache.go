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

package access

import (
	"sync"
	"time"
)

// ReplayCache tracks call credentials that have already been admitted,
// so a captured credential cannot be used twice. Call credentials
// carry a random jti and an expiry; this cache remembers each jti
// (scoped by its tokenID) until the expiry passes.
//
// The cache is size-bounded. When full, it fails closed: new entries
// are rejected as if they were replays, instead of being silently
// evicted. This keeps replay protection intact under load — a flood
// of first-time credentials cannot push a real in-window jti out of
// the cache and let an attacker re-admit it.
type ReplayCache struct {
	mu         sync.Mutex
	entries    map[string]time.Time
	maxEntries int
	now        func() time.Time
}

// NewReplayCache creates an empty cache. maxEntries <= 0 disables the
// size bound; callers exposed to untrusted input must pass a positive
// cap so an attacker cannot exhaust memory by sending a stream of
// first-time credentials.
func NewReplayCache(maxEntries int) *ReplayCache {
	return &ReplayCache{
		entries:    make(map[string]time.Time),
		maxEntries: maxEntries,
		now:        time.Now,
	}
}

// AdmitIfNew records (tokenID, jti) with its expiry, atomically with
// the check for duplicates. Returns true when this pair has not been
// seen before within its window — the caller may admit the credential.
// Returns false when the pair is already recorded and still live (a
// replay), or when the cache is at its size bound (fail-closed).
// Expired entries are treated as absent and dropped on the way.
func (c *ReplayCache) AdmitIfNew(tokenID, jti string, exp time.Time) bool {
	key := tokenID + "|" + jti
	now := c.now()

	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, ok := c.entries[key]; ok {
		if existing.After(now) {
			return false // genuine replay within window
		}
		// Expired entry: fall through and overwrite below.
	}

	if c.maxEntries > 0 && len(c.entries) >= c.maxEntries {
		// Try one sweep before failing closed; keeps a steady-state
		// cache honest when entries expire faster than Seen is called.
		c.sweepLocked(now)
		if len(c.entries) >= c.maxEntries {
			return false
		}
	}

	c.entries[key] = exp
	return true
}

// Sweep drops expired entries. Called internally on overflow; callers
// running without a background janitor may invoke it periodically.
func (c *ReplayCache) Sweep() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sweepLocked(c.now())
}

// Len returns the current number of live (non-expired) entries. Intended
// for tests and metrics; takes the lock.
func (c *ReplayCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sweepLocked(c.now())
	return len(c.entries)
}

func (c *ReplayCache) sweepLocked(now time.Time) {
	for k, exp := range c.entries {
		if !exp.After(now) {
			delete(c.entries, k)
		}
	}
}
