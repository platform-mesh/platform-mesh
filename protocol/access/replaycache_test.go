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
	"testing"
	"time"
)

func TestReplayCacheFirstAcceptsThenRejects(t *testing.T) {
	c := NewReplayCache(0)
	exp := time.Now().Add(time.Minute)

	if !c.AdmitIfNew("id", "jti-1", exp) {
		t.Fatal("first Seen should return true")
	}
	if c.AdmitIfNew("id", "jti-1", exp) {
		t.Fatal("second Seen for same (tokenID, jti) should return false")
	}
}

func TestReplayCacheScopedByTokenID(t *testing.T) {
	c := NewReplayCache(0)
	exp := time.Now().Add(time.Minute)
	if !c.AdmitIfNew("id-a", "jti", exp) {
		t.Fatal("first Seen id-a should accept")
	}
	if !c.AdmitIfNew("id-b", "jti", exp) {
		t.Fatal("same jti under different tokenID must accept (distinct Identities)")
	}
}

func TestReplayCacheExpiredEntriesEvicted(t *testing.T) {
	c := NewReplayCache(0)

	// Pin the clock so we can step it deterministically.
	now := time.Now()
	c.now = func() time.Time { return now }

	if !c.AdmitIfNew("id", "jti", now.Add(time.Second)) {
		t.Fatal("first Seen should accept")
	}

	// Advance past expiry; the next Seen on the same key should accept
	// (treat the expired entry as absent).
	now = now.Add(2 * time.Second)
	if !c.AdmitIfNew("id", "jti", now.Add(time.Second)) {
		t.Fatal("after expiry, Seen should accept again")
	}
}

func TestReplayCacheMaxEntriesFailsClosed(t *testing.T) {
	c := NewReplayCache(2)
	now := time.Now()
	c.now = func() time.Time { return now }

	exp := now.Add(time.Hour)
	if !c.AdmitIfNew("id", "a", exp) {
		t.Fatal("seat 1 should accept")
	}
	if !c.AdmitIfNew("id", "b", exp) {
		t.Fatal("seat 2 should accept")
	}
	if c.AdmitIfNew("id", "c", exp) {
		t.Fatal("over cap: Seen must return false (fail closed)")
	}
}

func TestReplayCacheSweepReclaimsCapacity(t *testing.T) {
	c := NewReplayCache(2)
	now := time.Now()
	c.now = func() time.Time { return now }

	if !c.AdmitIfNew("id", "a", now.Add(time.Second)) {
		t.Fatal("seat 1")
	}
	if !c.AdmitIfNew("id", "b", now.Add(time.Second)) {
		t.Fatal("seat 2")
	}

	// Advance past the two entries' expiry; a third insert should
	// reclaim capacity on its own via the on-overflow sweep.
	now = now.Add(2 * time.Second)
	if !c.AdmitIfNew("id", "c", now.Add(time.Minute)) {
		t.Fatal("after expiry, a new Seen should reclaim capacity and accept")
	}
	if got := c.Len(); got != 1 {
		t.Errorf("Len after sweep = %d, want 1", got)
	}
}
