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

	"go.platform-mesh.io/golang-commons/context/keys"
	"go.platform-mesh.io/golang-commons/logger"

	"github.com/stretchr/testify/assert"
)

func TestSetRequestIdWithIncomingHeader(t *testing.T) {

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		val := GetRequestId(r.Context())
		assert.Equal(t, "123", val)
	})

	// create the handler to test, using our custom "next" handler
	handlerToTest := SetRequestId()(nextHandler)

	// create a mock request to use
	req := httptest.NewRequest("GET", "http://testing", nil)
	req.Header.Add("X-Request-Id", "123")

	// call the handler using a mock response recorder (we'll not use that anyway)
	handlerToTest.ServeHTTP(httptest.NewRecorder(), req)
}

func TestSetRequestIdWitoutIncomingHeader(t *testing.T) {

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		val := GetRequestId(r.Context())
		assert.Len(t, val, 36)
	})

	// create the handler to test, using our custom "next" handler
	handlerToTest := SetRequestId()(nextHandler)

	// create a mock request to use
	req := httptest.NewRequest("GET", "http://testing", nil)

	// call the handler using a mock response recorder (we'll not use that anyway)
	handlerToTest.ServeHTTP(httptest.NewRecorder(), req)
}

func TestSetRequestIdInLogger(t *testing.T) {
	// This test verifies that SetRequestIdInLogger creates a request-aware logger
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The logger in context should be updated with request information
		log := logger.LoadLoggerFromContext(r.Context())
		assert.NotNil(t, log)
		w.WriteHeader(http.StatusOK)
	})

	// create the handler to test
	handlerToTest := SetRequestIdInLogger()(nextHandler)

	// create a mock request to use
	req := httptest.NewRequest("GET", "http://testing", nil)

	// call the handler using a mock response recorder
	handlerToTest.ServeHTTP(httptest.NewRecorder(), req)
}

func TestGetRequestId_WithValidContext(t *testing.T) {
	requestId := "test-request-id-123"

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		retrievedId := GetRequestId(r.Context())
		assert.Equal(t, requestId, retrievedId)
	})

	handlerToTest := SetRequestId()(nextHandler)

	req := httptest.NewRequest("GET", "http://testing", nil)
	req.Header.Add("X-Request-Id", requestId)

	handlerToTest.ServeHTTP(httptest.NewRecorder(), req)
}

func TestGetRequestId_WithEmptyContext(t *testing.T) {
	// Test GetRequestId with a context that doesn't have a request ID
	emptyCtx := context.Background()
	requestId := GetRequestId(emptyCtx)
	assert.Empty(t, requestId)
}

func TestGetRequestId_WithInvalidContextValue(t *testing.T) {
	// Test GetRequestId with a context that has an invalid request ID value
	ctx := context.WithValue(context.Background(), keys.RequestIdCtxKey, 123) // not a string
	requestId := GetRequestId(ctx)
	assert.Empty(t, requestId)
}
