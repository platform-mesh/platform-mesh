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

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// SetOtelTracingContext returns an HTTP middleware that extracts OpenTelemetry
// tracing context from incoming request headers and injects it into the request's
// context before passing the request to the next handler.
//
// The middleware uses the global OpenTelemetry text map propagator and
// propagation.HeaderCarrier to read trace/span context from the request headers.
// Any extraction behavior (including failure handling) is delegated to the
// propagator implementation.
func SetOtelTracingContext() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
			ctx := otel.GetTextMapPropagator().Extract(request.Context(), propagation.HeaderCarrier(request.Header))
			next.ServeHTTP(responseWriter, request.WithContext(ctx))
		})
	}
}
