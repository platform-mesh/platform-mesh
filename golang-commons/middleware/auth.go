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

	appctx "go.platform-mesh.io/golang-commons/context"
)

const AuthorizationHeader = "Authorization"

// StoreAuthHeader returns HTTP middleware that reads the request's Authorization header and stores it in the request context.
// The middleware wraps a handler, extracts the Authorization header (using AuthorizationHeader), calls
// appctx.AddAuthHeaderToContext with the existing request context and the header value, and invokes the next handler
// with the request updated to use that context. If the Authorization header is absent or empty, nothing is stored.
func StoreAuthHeader() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
			auth := request.Header.Get(AuthorizationHeader)
			ctx := request.Context()
			if auth != "" {
				ctx = appctx.AddAuthHeaderToContext(ctx, auth)
			}
			next.ServeHTTP(responseWriter, request.WithContext(ctx))
		})
	}
}
