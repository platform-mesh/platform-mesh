//go:build test || local

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

package local_middleware

import (
	"net/http"

	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"

	"go.platform-mesh.io/golang-commons/context"
)

// LocalMiddleware returns an HTTP middleware factory that injects a test JWT and tenant ID into each request's context.
// The returned middleware creates a lightweight, unsigned JWT whose subject is set to userId and whose issuer is "localhost:8080",
// stores that token (allowed signature algorithm "none") and the provided tenantId in the request context, then calls the next handler.
// This middleware is intended for local/test use; it will panic if token creation fails.
func LocalMiddleware(tenantId string, userId string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
			ctx := request.Context()

			claims := &jwt.RegisteredClaims{Issuer: "localhost:8080", Subject: userId, Audience: jwt.ClaimStrings{"testing"}}
			token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
			if err != nil {
				panic(err) // This shouldn't happen, and if it does, only locally
			}

			ctx = context.AddWebTokenToContext(ctx, token, []jose.SignatureAlgorithm{"none"})
			ctx = context.AddTenantToContext(ctx, tenantId)

			next.ServeHTTP(responseWriter, request.WithContext(ctx))
		})
	}
}
