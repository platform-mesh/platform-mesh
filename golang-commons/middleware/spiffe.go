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

	"go.platform-mesh.io/golang-commons/context"
	"go.platform-mesh.io/golang-commons/jwt"
)

// StoreSpiffeHeader returns an HTTP middleware that extracts a SPIFFE URL from the request headers
// and, if present, inserts it into the request context for downstream handlers.
//
// The middleware always calls the next handler; when a SPIFFE URL is found it updates the request's
// context with that value so subsequent handlers can retrieve it.
func StoreSpiffeHeader() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
			ctx := request.Context()
			uriVal := jwt.GetSpiffeUrlValue(request.Header)

			if uriVal != nil {
				ctx = context.AddSpiffeToContext(ctx, *uriVal)
			}
			next.ServeHTTP(responseWriter, request.WithContext(ctx))
		})
	}
}
