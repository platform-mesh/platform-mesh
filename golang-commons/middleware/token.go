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
	"strings"

	"github.com/go-jose/go-jose/v4"

	pmcontext "go.platform-mesh.io/golang-commons/context"
)

const tokenAuthPrefix = "BEARER"

var signatureAlgorithms = []jose.SignatureAlgorithm{jose.RS256}

// StoreWebToken returns middleware that extracts a JWT from the HTTP `Authorization` header
// and stores it in the request pmcontext for downstream handlers.
//
// The middleware looks for an Authorization header of the form `Bearer <token>` (scheme match is
// case-insensitive). When present, the token is added to the pmcontext via
// context.AddWebTokenToContext using the package's signatureAlgorithms. If the header is absent,
// malformed, or not a Bearer token, the request pmcontext is left unchanged.
func StoreWebToken() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
			ctx := request.Context()
			tokens := strings.Fields(request.Header.Get(AuthorizationHeader))
			if len(tokens) == 2 && strings.EqualFold(tokens[0], tokenAuthPrefix) {
				ctx = pmcontext.AddWebTokenToContext(ctx, tokens[1], signatureAlgorithms)
			}

			next.ServeHTTP(responseWriter, request.WithContext(ctx))
		})
	}
}
