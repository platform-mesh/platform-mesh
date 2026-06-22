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
	"go.platform-mesh.io/golang-commons/logger"
	"go.platform-mesh.io/golang-commons/logger/testlogger"
)

func TestStoreLoggerMiddleware(t *testing.T) {
	testLog := testlogger.New()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify logger is stored in context
		logFromContext := logger.LoadLoggerFromContext(r.Context())
		assert.NotNil(t, logFromContext)

		// The logger should be the same instance we passed
		assert.Equal(t, testLog.Logger, logFromContext)

		w.WriteHeader(http.StatusOK)
	})

	middleware := StoreLoggerMiddleware(testLog.Logger)
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest("GET", "http://testing", nil)
	recorder := httptest.NewRecorder()

	handlerToTest.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestStoreLoggerMiddleware_NilLogger(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Even with nil logger, the middleware should not panic
		w.WriteHeader(http.StatusOK)
	})

	middleware := StoreLoggerMiddleware(nil)
	handlerToTest := middleware(nextHandler)

	req := httptest.NewRequest("GET", "http://testing", nil)
	recorder := httptest.NewRecorder()

	// Should not panic
	assert.NotPanics(t, func() {
		handlerToTest.ServeHTTP(recorder, req)
	})

	assert.Equal(t, http.StatusOK, recorder.Code)
}
