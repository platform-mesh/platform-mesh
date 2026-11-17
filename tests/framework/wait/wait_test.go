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

package wait

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// recorder is a testing.TB whose Logf/Fatalf are recorded instead of
// reaching the real test, and whose context the test controls.
type recorder struct {
	testing.TB
	ctx context.Context

	mu     sync.Mutex
	logs   []string
	failed string
}

func (r *recorder) Helper()                  {}
func (r *recorder) Context() context.Context { return r.ctx }
func (r *recorder) Logf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, fmt.Sprintf(format, args...))
}
func (r *recorder) Fatalf(format string, args ...any) {
	r.mu.Lock()
	r.failed = fmt.Sprintf(format, args...)
	r.mu.Unlock()
	runtime.Goexit()
}

// run calls Eventually in its own goroutine, as the testing package runs a
// test, and returns once that goroutine ended.
func run(r *recorder, cond func() (bool, string), timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		Eventually(r, cond, timeout, 5*time.Millisecond, "thing %s", "x")
	}()
	<-done
}

func TestEventuallySucceeds(t *testing.T) {
	r := &recorder{TB: t, ctx: t.Context()}
	calls := 0
	run(r, func() (bool, string) {
		calls++
		return calls == 3, ""
	}, time.Second)
	require.Empty(t, r.failed)
	require.Equal(t, 3, calls)
}

func TestEventuallyFailsWithLastReason(t *testing.T) {
	r := &recorder{TB: t, ctx: t.Context()}
	run(r, func() (bool, string) { return false, "phase Initializing" }, 50*time.Millisecond)
	require.Contains(t, r.failed, "thing x")
	require.Contains(t, r.failed, "last: phase Initializing")
	require.Equal(t, []string{"Waiting for condition, but got: phase Initializing"}, r.logs, "a reason is logged once, when it changes")
}

// TestEventuallyStopsWhenTestEnds checks that a wait whose test is over (its
// context is cancelled) stops polling and does not use t any more: with
// require.Eventually the condition kept running in a goroutine and its
// t.Logf panicked with "Log in goroutine after Test... has completed".
func TestEventuallyStopsWhenTestEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	r := &recorder{TB: t, ctx: ctx}
	var mu sync.Mutex
	calls := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		Eventually(r, func() (bool, string) {
			mu.Lock()
			defer mu.Unlock()
			calls++
			return false, fmt.Sprintf("call %d", calls)
		}, time.Minute, 5*time.Millisecond, "never")
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Eventually kept polling after the test context was cancelled")
	}
	mu.Lock()
	n := calls
	mu.Unlock()
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, n, calls, "no polls after Eventually returned")
	require.Empty(t, r.failed)
	require.Empty(t, r.logs, "reasons are only logged after a fifth of the timeout")
}
