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

	"go.platform-mesh.io/golang-commons/context"
)

func TestStoreAuthHeader_WithAuthHeader(t *testing.T) {
	expectedAuth := "Bearer token123"

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header is stored in context
		authFromContext, err := context.GetAuthHeaderFromContext(r.Context())
		assert.NoError(t, err)
		assert.Equal(t, expectedAuth, authFromContext)

		w.WriteHeader(http.StatusOK)
	})

	middleware := StoreAuthHeader()
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest(http.MethodGet, "http://testing", nil)
	req.Header.Set(AuthorizationHeader, expectedAuth)
	recorder := httptest.NewRecorder()

	handlerToTest.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestStoreAuthHeader_WithoutAuthHeader(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify empty auth header returns error when not set
		_, err := context.GetAuthHeaderFromContext(r.Context())
		assert.Error(t, err) // Should return error when no auth header is set

		w.WriteHeader(http.StatusOK)
	})

	middleware := StoreAuthHeader()
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest(http.MethodGet, "http://testing", nil)
	// No authorization header set
	recorder := httptest.NewRecorder()

	handlerToTest.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestStoreAuthHeader_WithEmptyAuthHeader(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify empty auth header returns error when empty
		_, err := context.GetAuthHeaderFromContext(r.Context())
		assert.Error(t, err) // Should return error when auth header is empty

		w.WriteHeader(http.StatusOK)
	})

	middleware := StoreAuthHeader()
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest(http.MethodGet, "http://testing", nil)
	req.Header.Set(AuthorizationHeader, "")
	recorder := httptest.NewRecorder()

	handlerToTest.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestStoreAuthHeader_MultipleAuthHeaders(t *testing.T) {
	// Test behavior when multiple authorization headers are present
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should get the first header value since http.Header.Get returns only the first value
		authFromContext, err := context.GetAuthHeaderFromContext(r.Context())
		assert.NoError(t, err)
		assert.Equal(t, "Bearer token1", authFromContext)

		w.WriteHeader(http.StatusOK)
	})

	middleware := StoreAuthHeader()
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest(http.MethodGet, "http://testing", nil)
	req.Header.Add(AuthorizationHeader, "Bearer token1")
	req.Header.Add(AuthorizationHeader, "Bearer token2")
	recorder := httptest.NewRecorder()

	handlerToTest.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}
