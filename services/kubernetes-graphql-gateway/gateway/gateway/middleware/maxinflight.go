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

import "net/http"

// InFlightMetrics provides optional instrumentation for the max-inflight middleware.
// When non-nil, the middleware calls these methods on acquire, release, and reject.
type InFlightMetrics struct {
	Active interface {
		Inc()
		Dec()
	}
	Total    interface{ Inc() }
	Rejected interface{ Inc() }
}

// WithMaxInFlightRequests returns a middleware that limits the number of concurrent
// requests being processed. When the limit is reached, new requests receive a
// 429 Too Many Requests response. Setting maxInFlight to 0 or less disables the limit.
// When metrics is non-nil, the middleware tracks active, total, and rejected counts.
func WithMaxInFlightRequests(handler http.Handler, maxInFlight int, metrics *InFlightMetrics) http.Handler {
	if maxInFlight <= 0 {
		return handler
	}
	sem := make(chan struct{}, maxInFlight)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case sem <- struct{}{}:
			if metrics != nil {
				metrics.Active.Inc()
				metrics.Total.Inc()
			}
			defer func() {
				if metrics != nil {
					metrics.Active.Dec()
				}
				<-sem
			}()
			handler.ServeHTTP(w, r)
		default:
			if metrics != nil {
				metrics.Rejected.Inc()
			}
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		}
	})
}
