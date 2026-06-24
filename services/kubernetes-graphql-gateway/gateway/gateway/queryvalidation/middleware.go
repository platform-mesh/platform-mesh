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

package queryvalidation

import (
	"encoding/json"
	"fmt"
	"net/http"

	utilscontext "go.platform-mesh.io/kubernetes-graphql-gateway/gateway/utils/context"
)

// Middleware returns an http.Handler that validates incoming GraphQL queries
// against depth and complexity limits before forwarding to the next handler.
// Supports both single requests and batched query arrays.
// If all limits are zero, the middleware is a no-op passthrough.
//
// Expects the request parser middleware to have stored parsed requests in context.
func Middleware(next http.Handler, cfg Config) http.Handler {
	if cfg.MaxDepth <= 0 && cfg.MaxComplexity <= 0 && cfg.MaxBatchSize <= 0 {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs, ok := utilscontext.GetParsedRequestsFromCtx(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}

		var queries []string
		for _, req := range reqs {
			if req.Query != "" {
				queries = append(queries, req.Query)
			}
		}

		if len(queries) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		if cfg.MaxBatchSize > 0 && len(queries) > cfg.MaxBatchSize {
			writeGraphQLError(w, fmt.Sprintf("batch size %d exceeds maximum allowed batch size of %d", len(queries), cfg.MaxBatchSize), http.StatusBadRequest)
			return
		}

		for _, q := range queries {
			if err := Validate(q, cfg); err != nil {
				writeGraphQLError(w, err.Error(), http.StatusBadRequest)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func writeGraphQLError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	resp := map[string]any{
		"errors": []map[string]string{
			{"message": message},
		},
	}
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}
