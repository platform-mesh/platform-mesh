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
	"go.platform-mesh.io/golang-commons/jwt"
)

func TestStoreSpiffeHeader_WithValidSpiffeHeader(t *testing.T) {
	expectedSpiffeID := "spiffe://example.org/workload"

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify SPIFFE ID is stored in context
		spiffeFromContext, err := context.GetSpiffeFromContext(r.Context())
		assert.NoError(t, err)
		assert.Equal(t, expectedSpiffeID, spiffeFromContext)

		w.WriteHeader(http.StatusOK)
	})

	middleware := StoreSpiffeHeader()
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest("GET", "http://testing", nil)
	// Set the SPIFFE header that jwt.GetSpiffeUrlValue expects
	req.Header.Set("X-Forwarded-Client-Cert", "Subject=\"CN=test\";URI="+expectedSpiffeID)
	recorder := httptest.NewRecorder()

	handlerToTest.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestStoreSpiffeHeader_WithoutSpiffeHeader(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Context should not have SPIFFE ID when no header is present
		_, err := context.GetSpiffeFromContext(r.Context())
		assert.Error(t, err) // Should return an error when no SPIFFE ID is present

		w.WriteHeader(http.StatusOK)
	})

	middleware := StoreSpiffeHeader()
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest("GET", "http://testing", nil)
	// No SPIFFE header set
	recorder := httptest.NewRecorder()

	handlerToTest.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestStoreSpiffeHeader_WithEmptySpiffeHeader(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Context should not have SPIFFE ID when header is empty
		_, err := context.GetSpiffeFromContext(r.Context())
		assert.Error(t, err)

		w.WriteHeader(http.StatusOK)
	})

	middleware := StoreSpiffeHeader()
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest("GET", "http://testing", nil)
	req.Header.Set("X-Forwarded-Client-Cert", "")
	recorder := httptest.NewRecorder()

	handlerToTest.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestStoreSpiffeHeader_WithInvalidSpiffeHeader(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Context should not have SPIFFE ID when header is invalid
		_, err := context.GetSpiffeFromContext(r.Context())
		assert.Error(t, err)

		w.WriteHeader(http.StatusOK)
	})

	middleware := StoreSpiffeHeader()
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest("GET", "http://testing", nil)
	req.Header.Set("X-Forwarded-Client-Cert", "InvalidHeaderValue")
	recorder := httptest.NewRecorder()

	handlerToTest.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestStoreSpiffeHeader_WithMultipleSpiffeHeaders(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// With multiple headers, the behavior depends on jwt.GetSpiffeUrlValue implementation
		// It should either return the first valid one or handle concatenated headers
		spiffeFromContext, err := context.GetSpiffeFromContext(r.Context())
		if err == nil {
			// If we get a value, it should be a valid SPIFFE ID
			assert.Contains(t, spiffeFromContext, "spiffe://")
		}

		w.WriteHeader(http.StatusOK)
	})

	middleware := StoreSpiffeHeader()
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest("GET", "http://testing", nil)
	req.Header.Add("X-Forwarded-Client-Cert", "Subject=\"CN=test1\";URI=spiffe://example.org/workload1")
	req.Header.Add("X-Forwarded-Client-Cert", "Subject=\"CN=test2\";URI=spiffe://example.org/workload2")
	recorder := httptest.NewRecorder()

	handlerToTest.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestStoreSpiffeHeader_Integration(t *testing.T) {
	// Test the integration with jwt.GetSpiffeUrlValue function
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify that the middleware properly uses jwt.GetSpiffeUrlValue
		spiffeValue := jwt.GetSpiffeUrlValue(r.Header)

		if spiffeValue != nil {
			spiffeFromContext, err := context.GetSpiffeFromContext(r.Context())
			assert.NoError(t, err)
			assert.Equal(t, *spiffeValue, spiffeFromContext)
		} else {
			_, err := context.GetSpiffeFromContext(r.Context())
			assert.Error(t, err)
		}

		w.WriteHeader(http.StatusOK)
	})

	middleware := StoreSpiffeHeader()
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest("GET", "http://testing", nil)
	// Test without header first
	recorder := httptest.NewRecorder()
	handlerToTest.ServeHTTP(recorder, req)
	assert.Equal(t, http.StatusOK, recorder.Code)

	// Test with valid header
	req = httptest.NewRequest("GET", "http://testing", nil)
	req.Header.Set("X-Forwarded-Client-Cert", "Subject=\"CN=test\";URI=spiffe://example.org/test")
	recorder = httptest.NewRecorder()
	handlerToTest.ServeHTTP(recorder, req)
	assert.Equal(t, http.StatusOK, recorder.Code)
}
