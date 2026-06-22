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
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestSetOtelTracingContext(t *testing.T) {
	// Set up a test propagator
	propagator := propagation.TraceContext{}
	otel.SetTextMapPropagator(propagator)

	// Create a span context to inject
	tracer := noop.NewTracerProvider().Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	span.End()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The context should have been extracted and set
		assert.NotNil(t, r.Context())
		w.WriteHeader(http.StatusOK)
	})

	middleware := SetOtelTracingContext()
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest("GET", "http://testing", nil)

	// Inject trace context into headers
	propagator.Inject(ctx, propagation.HeaderCarrier(req.Header))

	recorder := httptest.NewRecorder()

	handlerToTest.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestSetOtelTracingContext_NoTraceHeaders(t *testing.T) {
	// Set up a test propagator
	propagator := propagation.TraceContext{}
	otel.SetTextMapPropagator(propagator)

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Even without trace headers, context should be set
		assert.NotNil(t, r.Context())
		w.WriteHeader(http.StatusOK)
	})

	middleware := SetOtelTracingContext()
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest("GET", "http://testing", nil)
	recorder := httptest.NewRecorder()

	handlerToTest.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestSetOtelTracingContext_Integration(t *testing.T) {
	// Test that the middleware properly integrates with the OpenTelemetry propagation system
	propagator := propagation.TraceContext{}
	otel.SetTextMapPropagator(propagator)

	var extractedContext context.Context
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		extractedContext = r.Context()
		w.WriteHeader(http.StatusOK)
	})

	middleware := SetOtelTracingContext()
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest("GET", "http://testing", nil)

	// Add a fake trace header to test extraction
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	recorder := httptest.NewRecorder()

	handlerToTest.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.NotNil(t, extractedContext)

	// Verify that the context is different from the original request context
	assert.NotEqual(t, req.Context(), extractedContext)
}
