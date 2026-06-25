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
	"testing"

	"github.com/stretchr/testify/assert"

	"go.platform-mesh.io/golang-commons/logger/testlogger"
)

func TestCreateMiddleware_WithoutAuth(t *testing.T) {
	log := testlogger.New()
	middlewares := CreateMiddleware(log.Logger, false)

	// Should return 5 middlewares when auth is false
	assert.Len(t, middlewares, 5)

	// Each middleware should be a valid function
	for _, mw := range middlewares {
		assert.NotNil(t, mw)
	}

	// Test that middlewares can be chained
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Apply all middlewares
	var finalHandler http.Handler = handler
	for i := len(middlewares) - 1; i >= 0; i-- {
		finalHandler = middlewares[i](finalHandler)
	}

	req := httptest.NewRequest(http.MethodGet, "http://testing", nil)
	recorder := httptest.NewRecorder()

	finalHandler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestCreateMiddleware_WithAuth(t *testing.T) {
	log := testlogger.New()
	middlewares := CreateMiddleware(log.Logger, true)

	// Should return 8 middlewares when auth is true (5 base + 3 auth)
	assert.Len(t, middlewares, 8)

	// Each middleware should be a valid function
	for _, mw := range middlewares {
		assert.NotNil(t, mw)
	}

	// Test that middlewares can be chained
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Apply all middlewares
	var finalHandler http.Handler = handler
	for i := len(middlewares) - 1; i >= 0; i-- {
		finalHandler = middlewares[i](finalHandler)
	}

	req := httptest.NewRequest(http.MethodGet, "http://testing", nil)
	recorder := httptest.NewRecorder()

	finalHandler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}
