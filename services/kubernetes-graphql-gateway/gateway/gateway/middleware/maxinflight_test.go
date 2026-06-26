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

package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithMaxInFlightRequests(t *testing.T) {
	tests := []struct {
		name         string
		maxInFlight  int
		handler      func(started chan<- struct{}, release <-chan struct{}) http.Handler
		sendRequests func(t *testing.T, serverURL string, started chan struct{}, release chan struct{})
	}{
		{
			name:        "disabled when maxInFlight is not positive",
			maxInFlight: 0,
			handler: func(_ chan<- struct{}, _ <-chan struct{}) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				})
			},
			sendRequests: func(t *testing.T, serverURL string, _ chan struct{}, _ chan struct{}) {
				t.Helper()
				resp, err := http.Get(serverURL)
				require.NoError(t, err)
				defer resp.Body.Close() //nolint:errcheck
				assert.Equal(t, http.StatusOK, resp.StatusCode)
			},
		},
		{
			name:        "rejects when all slots are occupied",
			maxInFlight: 3,
			handler: func(started chan<- struct{}, release <-chan struct{}) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					started <- struct{}{}
					<-release
					w.WriteHeader(http.StatusOK)
				})
			},
			sendRequests: func(t *testing.T, serverURL string, started chan struct{}, release chan struct{}) {
				t.Helper()

				var wg sync.WaitGroup
				// Fill all 3 slots
				for range 3 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						resp, err := http.Get(serverURL)
						require.NoError(t, err)
						defer resp.Body.Close() //nolint:errcheck //nolint:errcheck
						assert.Equal(t, http.StatusOK, resp.StatusCode)
					}()
					<-started
				}

				// 4th request should be rejected
				resp, err := http.Get(serverURL)
				require.NoError(t, err)
				defer resp.Body.Close() //nolint:errcheck
				assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)

				close(release)
				wg.Wait()
			},
		},
		{
			name:        "slot released after handler returns",
			maxInFlight: 1,
			handler: func(_ chan<- struct{}, _ <-chan struct{}) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				})
			},
			sendRequests: func(t *testing.T, serverURL string, _ chan struct{}, _ chan struct{}) {
				t.Helper()
				for range 5 {
					resp, err := http.Get(serverURL)
					require.NoError(t, err)
					resp.Body.Close() //nolint:errcheck
					assert.Equal(t, http.StatusOK, resp.StatusCode)
				}
			},
		},
		{
			name:        "slot released after handler panics",
			maxInFlight: 1,
			handler: func(_ chan<- struct{}, _ <-chan struct{}) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					defer func() {
						if r := recover(); r != nil {
							w.WriteHeader(http.StatusInternalServerError)
						}
					}()
					panic("test panic")
				})
			},
			sendRequests: func(t *testing.T, serverURL string, _ chan struct{}, _ chan struct{}) {
				t.Helper()

				// First request panics
				resp, err := http.Get(serverURL)
				require.NoError(t, err)
				resp.Body.Close() //nolint:errcheck

				// Second request should succeed (slot was released)
				done := make(chan struct{})
				go func() {
					defer close(done)
					resp, err := http.Get(serverURL)
					require.NoError(t, err)
					resp.Body.Close() //nolint:errcheck
				}()

				select {
				case <-done:
					// success
				case <-time.After(2 * time.Second):
					t.Fatal("second request timed out — slot was not released after panic")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			started := make(chan struct{}, 10)
			release := make(chan struct{})

			handler := WithMaxInFlightRequests(tt.handler(started, release), tt.maxInFlight, nil)
			server := httptest.NewServer(handler)
			defer server.Close()

			tt.sendRequests(t, server.URL, started, release)
		})
	}
}

func TestWithMaxInFlightRequestsMetrics(t *testing.T) {
	t.Run("metrics incremented on successful request", func(t *testing.T) {
		m := &fakeMetrics{}
		metrics := &InFlightMetrics{
			Active:   m,
			Total:    &fakeCounter{&m.totalInc},
			Rejected: &fakeCounter{&m.rejectedInc},
		}

		handler := WithMaxInFlightRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Inside handler: active should be 1
			assert.Equal(t, int64(1), m.active.Load())
			w.WriteHeader(http.StatusOK)
		}), 10, metrics)

		server := httptest.NewServer(handler)
		defer server.Close()

		resp, err := http.Get(server.URL)
		require.NoError(t, err)
		defer resp.Body.Close() //nolint:errcheck

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, int64(0), m.active.Load())
		assert.Equal(t, int64(1), m.totalInc.Load())
		assert.Equal(t, int64(0), m.rejectedInc.Load())
	})

	t.Run("rejected metric incremented at capacity", func(t *testing.T) {
		m := &fakeMetrics{}
		metrics := &InFlightMetrics{
			Active:   m,
			Total:    &fakeCounter{&m.totalInc},
			Rejected: &fakeCounter{&m.rejectedInc},
		}

		started := make(chan struct{})
		release := make(chan struct{})

		handler := WithMaxInFlightRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started <- struct{}{}
			<-release
			w.WriteHeader(http.StatusOK)
		}), 1, metrics)

		server := httptest.NewServer(handler)
		defer server.Close()

		// Fill the single slot
		go func() {
			resp, _ := http.Get(server.URL)
			if resp != nil {
				resp.Body.Close() //nolint:errcheck
			}
		}()
		<-started

		// This request should be rejected
		resp, err := http.Get(server.URL)
		require.NoError(t, err)
		defer resp.Body.Close() //nolint:errcheck

		assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
		assert.Equal(t, int64(1), m.active.Load())
		assert.Equal(t, int64(1), m.totalInc.Load())
		assert.Equal(t, int64(1), m.rejectedInc.Load())

		close(release)
	})
}

// fakeMetrics implements the Active gauge interface (Inc/Dec) using atomics.
type fakeMetrics struct {
	active      atomic.Int64
	totalInc    atomic.Int64
	rejectedInc atomic.Int64
}

func (f *fakeMetrics) Inc() { f.active.Add(1) }
func (f *fakeMetrics) Dec() { f.active.Add(-1) }

// fakeCounter implements the counter interface (Inc) using an atomic.
type fakeCounter struct {
	val *atomic.Int64
}

func (f *fakeCounter) Inc() { f.val.Add(1) }
