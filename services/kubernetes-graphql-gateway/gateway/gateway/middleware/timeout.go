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
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"sync"
	"time"
)

// WithTimeout returns a middleware that enforces a request timeout.
// When the timeout is exceeded, it responds with 504 Gateway Timeout and a
// GraphQL-formatted JSON error body. Setting timeout to 0 or less disables the limit.
func WithTimeout(handler http.Handler, timeout time.Duration) http.Handler {
	if timeout <= 0 {
		return handler
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		flusher, _ := w.(http.Flusher)
		tw := &timeoutWriter{wrapped: w, header: make(http.Header), flusher: flusher}
		done := make(chan struct{})
		go func() {
			handler.ServeHTTP(tw, r.WithContext(ctx))
			close(done)
		}()

		select {
		case <-done:
			tw.mu.Lock()
			defer tw.mu.Unlock()
			tw.drain()
		case <-ctx.Done():
			tw.mu.Lock()
			tw.timedOut = true
			tw.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusGatewayTimeout)
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"errors": []map[string]string{
					{"message": "request timeout"},
				},
			})
		}
	})
}

// timeoutWriter buffers the response so it can be discarded on timeout.
type timeoutWriter struct {
	wrapped  http.ResponseWriter
	header   http.Header
	buf      bytes.Buffer
	code     int
	mu       sync.Mutex
	timedOut bool
	flusher  http.Flusher // resolved once at construction; nil if unsupported
}

func (tw *timeoutWriter) Header() http.Header { return tw.header }

func (tw *timeoutWriter) Write(p []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return 0, context.DeadlineExceeded
	}
	return tw.buf.Write(p)
}

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return
	}
	tw.code = code
}

// Flush implements http.Flusher. It drains buffered data to the underlying
// writer and flushes it, enabling streaming responses (e.g. SSE subscriptions).
func (tw *timeoutWriter) Flush() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return
	}
	tw.drain()
	if tw.flusher != nil {
		tw.flusher.Flush()
	}
}

// drain forwards buffered headers, status code, and body to the underlying writer.
// Must be called with tw.mu held.
func (tw *timeoutWriter) drain() {
	maps.Copy(tw.wrapped.Header(), tw.header)
	if tw.code != 0 {
		tw.wrapped.WriteHeader(tw.code)
		tw.code = 0
	}
	if tw.buf.Len() > 0 {
		tw.wrapped.Write(tw.buf.Bytes()) //nolint:errcheck
		tw.buf.Reset()
	}
}
