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

func TestStoreWebToken_WithFakeBearerToken(t *testing.T) {
	token := "fake.invalid.token"
	authHeader := "Bearer " + token

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Token parsing will fail due to fake token, which is expected in tests
		// The middleware should handle this gracefully
		_, err := context.GetWebTokenFromContext(r.Context())
		// For test purposes, we just verify the middleware doesn't crash
		// and that token validation fails as expected with fake tokens
		assert.Error(t, err) // This is expected behavior when token validation fails

		w.WriteHeader(http.StatusOK)
	})

	middleware := StoreWebToken()
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest("GET", "http://testing", nil)
	req.Header.Set(AuthorizationHeader, authHeader)
	recorder := httptest.NewRecorder()

	handlerToTest.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestStoreWebToken_WithoutAuthHeader(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Context should not have a token
		_, err := context.GetWebTokenFromContext(r.Context())
		assert.Error(t, err) // Should return an error when no token is present

		w.WriteHeader(http.StatusOK)
	})

	middleware := StoreWebToken()
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest("GET", "http://testing", nil)
	// No authorization header set
	recorder := httptest.NewRecorder()

	handlerToTest.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestStoreWebToken_WithNonBearerToken(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Context should not have a token
		_, err := context.GetWebTokenFromContext(r.Context())
		assert.Error(t, err) // Should return an error when no valid token is present

		w.WriteHeader(http.StatusOK)
	})

	middleware := StoreWebToken()
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest("GET", "http://testing", nil)
	req.Header.Set(AuthorizationHeader, "Basic dXNlcjpwYXNz") // Basic auth, not Bearer
	recorder := httptest.NewRecorder()

	handlerToTest.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestStoreWebToken_WithEmptyBearerToken(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Context should not have a token due to empty token
		_, err := context.GetWebTokenFromContext(r.Context())
		assert.Error(t, err)

		w.WriteHeader(http.StatusOK)
	})

	middleware := StoreWebToken()
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest("GET", "http://testing", nil)
	req.Header.Set(AuthorizationHeader, "Bearer ")
	recorder := httptest.NewRecorder()

	handlerToTest.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestStoreWebToken_WithFakeBearerTokenLowercase(t *testing.T) {
	token := "fake.invalid.token"
	authHeader := "bearer " + token // lowercase

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Token parsing will fail due to fake token, which is expected in tests
		// The middleware should process lowercase bearer tokens but validation will fail
		_, err := context.GetWebTokenFromContext(r.Context())
		// This is expected behavior when token validation fails with fake tokens
		assert.Error(t, err)

		w.WriteHeader(http.StatusOK)
	})

	middleware := StoreWebToken()
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest("GET", "http://testing", nil)
	req.Header.Set(AuthorizationHeader, authHeader)
	recorder := httptest.NewRecorder()

	handlerToTest.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestStoreWebToken_WithMalformedAuthHeader(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Context should not have a token
		_, err := context.GetWebTokenFromContext(r.Context())
		assert.Error(t, err)

		w.WriteHeader(http.StatusOK)
	})

	middleware := StoreWebToken()
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest("GET", "http://testing", nil)
	req.Header.Set(AuthorizationHeader, "Bearer") // Missing space and token
	recorder := httptest.NewRecorder()

	handlerToTest.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}
