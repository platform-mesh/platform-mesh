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
// Package cors provides the CORS handling of the GraphQL endpoint.
package cors

import (
	"net/http"
	"path"
	"strings"
)

const (
	defaultAllowedHeaders = "Authorization, Content-Type"
	allowedMethods        = "GET, POST, OPTIONS"
	maxAge                = "600"
)

// New returns a middleware that answers CORS preflight requests and sets the
// CORS response headers for requests of an allowed origin.
//
// An allowed origin is either an exact origin ("http://portal.localhost:4300"),
// a pattern in which "*" matches any sequence of characters other than "/"
// ("http://*.localhost:4300"), or "*" for any origin. Requests of other origins,
// and requests without an Origin header, are passed on untouched.
func New(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" || !originAllowed(allowedOrigins, origin) {
				next.ServeHTTP(w, r)
				return
			}

			header := w.Header()
			header.Add("Vary", "Origin")
			header.Set("Access-Control-Allow-Origin", origin)

			if r.Method != http.MethodOptions || r.Header.Get("Access-Control-Request-Method") == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Preflight requests carry no credentials and must not reach the
			// authentication middlewares.
			header.Add("Vary", "Access-Control-Request-Method")
			header.Add("Vary", "Access-Control-Request-Headers")
			header.Set("Access-Control-Allow-Methods", allowedMethods)
			if requested := r.Header.Get("Access-Control-Request-Headers"); requested != "" {
				header.Set("Access-Control-Allow-Headers", requested)
			} else {
				header.Set("Access-Control-Allow-Headers", defaultAllowedHeaders)
			}
			header.Set("Access-Control-Max-Age", maxAge)
			w.WriteHeader(http.StatusNoContent)
		})
	}
}

func originAllowed(allowedOrigins []string, origin string) bool {
	for _, allowed := range allowedOrigins {
		if allowed == "*" || strings.EqualFold(allowed, origin) {
			return true
		}
		if strings.Contains(allowed, "*") {
			if ok, err := path.Match(strings.ToLower(allowed), strings.ToLower(origin)); err == nil && ok {
				return true
			}
		}
	}

	return false
}
