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

	"go.platform-mesh.io/golang-commons/logger"
)

// CreateMiddleware creates a middleware chain with logging, tracing, and optional authentication.
// It attaches a request-scoped logger (using the provided logger), assigns a request ID, and propagates that ID into the logger.
// When auth is true, authentication middlewares (StoreWebToken, StoreAuthHeader, StoreSpiffeHeader) are included.
func CreateMiddleware(log *logger.Logger, auth bool) []func(http.Handler) http.Handler {
	mws := []func(http.Handler) http.Handler{
		SetOtelTracingContext(),
		SentryRecoverer,
		StoreLoggerMiddleware(log),
		SetRequestId(),
		SetRequestIdInLogger(),
	}

	if auth {
		mws = append(mws, CreateAuthMiddleware()...)
	}
	return mws
}
