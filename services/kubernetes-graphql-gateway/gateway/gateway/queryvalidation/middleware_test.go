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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	utilscontext "go.platform-mesh.io/kubernetes-graphql-gateway/gateway/utils/context"
)

func TestMiddleware(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":{}}`)) //nolint:errcheck
	})

	tests := []struct {
		name       string
		cfg        Config
		body       string
		method     string
		wantStatus int
		wantError  string
		wantPass   bool // whether request reaches the inner handler
	}{
		{
			name:       "valid query passes through",
			cfg:        Config{MaxDepth: 5, MaxComplexity: 100},
			body:       `{"query":"{ pods { name } }"}`,
			method:     http.MethodPost,
			wantStatus: http.StatusOK,
			wantPass:   true,
		},
		{
			name:       "deep query rejected",
			cfg:        Config{MaxDepth: 2},
			body:       `{"query":"{ a { b { c } } }"}`,
			method:     http.MethodPost,
			wantStatus: http.StatusBadRequest,
			wantError:  "query depth 3 exceeds maximum allowed depth of 2",
		},
		{
			name:       "complex query rejected",
			cfg:        Config{MaxComplexity: 3},
			body:       `{"query":"{ a b c d }"}`,
			method:     http.MethodPost,
			wantStatus: http.StatusBadRequest,
			wantError:  "query complexity 4 exceeds maximum allowed complexity of 3",
		},
		{
			name:       "disabled config passes everything",
			cfg:        Config{MaxDepth: 0, MaxComplexity: 0},
			body:       `{"query":"{ a { b { c { d { e { f } } } } } }"}`,
			method:     http.MethodPost,
			wantStatus: http.StatusOK,
			wantPass:   true,
		},
		{
			name:       "GET request passes through",
			cfg:        Config{MaxDepth: 1},
			body:       "",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
			wantPass:   true,
		},
		{
			name:       "empty query field passes through",
			cfg:        Config{MaxDepth: 1},
			body:       `{"query":""}`,
			method:     http.MethodPost,
			wantStatus: http.StatusOK,
			wantPass:   true,
		},
		{
			name:       "no parsed requests passes through",
			cfg:        Config{MaxDepth: 1},
			body:       `not json`,
			method:     http.MethodPost,
			wantStatus: http.StatusOK,
			wantPass:   true,
		},
		{
			name:       "batched query rejected when one exceeds depth",
			cfg:        Config{MaxDepth: 2},
			body:       `[{"query":"{ a { name } }"},{"query":"{ a { b { c } } }"}]`,
			method:     http.MethodPost,
			wantStatus: http.StatusBadRequest,
			wantError:  "query depth 3 exceeds maximum allowed depth of 2",
		},
		{
			name:       "batched query passes when all within limits",
			cfg:        Config{MaxDepth: 5, MaxComplexity: 100},
			body:       `[{"query":"{ a { name } }"},{"query":"{ b { name } }"}]`,
			method:     http.MethodPost,
			wantStatus: http.StatusOK,
			wantPass:   true,
		},
		{
			name:       "empty batched array passes through",
			cfg:        Config{MaxDepth: 1},
			body:       `[]`,
			method:     http.MethodPost,
			wantStatus: http.StatusOK,
			wantPass:   true,
		},
		{
			name:       "batch rejected when exceeding max batch size",
			cfg:        Config{MaxBatchSize: 2},
			body:       `[{"query":"{ a }"},{"query":"{ b }"},{"query":"{ c }"}]`,
			method:     http.MethodPost,
			wantStatus: http.StatusBadRequest,
			wantError:  "batch size 3 exceeds maximum allowed batch size of 2",
		},
		{
			name:       "batch passes when within max batch size",
			cfg:        Config{MaxBatchSize: 3},
			body:       `[{"query":"{ a }"},{"query":"{ b }"},{"query":"{ c }"}]`,
			method:     http.MethodPost,
			wantStatus: http.StatusOK,
			wantPass:   true,
		},
		{
			name:       "single request unaffected by batch size limit",
			cfg:        Config{MaxBatchSize: 2},
			body:       `{"query":"{ a }"}`,
			method:     http.MethodPost,
			wantStatus: http.StatusOK,
			wantPass:   true,
		},
		{
			name:       "batch size zero disables limit",
			cfg:        Config{MaxBatchSize: 0, MaxDepth: 10},
			body:       `[{"query":"{ a }"},{"query":"{ b }"},{"query":"{ c }"},{"query":"{ d }"},{"query":"{ e }"}]`,
			method:     http.MethodPost,
			wantStatus: http.StatusOK,
			wantPass:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var innerCalled bool
			var innerBody string
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				innerCalled = true
				if r.Body != nil {
					b, _ := io.ReadAll(r.Body)
					innerBody = string(b)
				}
				okHandler.ServeHTTP(w, r)
			})

			handler := Middleware(inner, tt.cfg)

			var body io.Reader
			if tt.body != "" {
				body = strings.NewReader(tt.body)
			}
			req := httptest.NewRequest(tt.method, "/graphql", body)
			if tt.method == http.MethodPost {
				req.Header.Set("Content-Type", "application/json")
			}

			// Simulate the request parser middleware by setting parsed requests in context.
			if reqs := parseBodies(tt.body); len(reqs) > 0 {
				ctx := utilscontext.SetParsedRequests(req.Context(), reqs)
				req = req.WithContext(ctx)
			}

			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)

			if tt.wantError != "" {
				var resp struct {
					Errors []struct {
						Message string `json:"message"`
					} `json:"errors"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Errors, 1)
				assert.Contains(t, resp.Errors[0].Message, tt.wantError)
				assert.False(t, innerCalled, "inner handler should not be called on rejection")
			}

			if tt.wantPass {
				assert.True(t, innerCalled, "inner handler should be called")
				if tt.body != "" && tt.method == http.MethodPost {
					assert.Equal(t, tt.body, innerBody, "body should be preserved for downstream handler")
				}
			}
		})
	}
}

// parseBodies simulates what the request parser middleware does: parse JSON into GraphQLRequests.
func parseBodies(body string) []utilscontext.GraphQLRequest {
	if body == "" {
		return nil
	}
	var reqs []utilscontext.GraphQLRequest
	if err := json.Unmarshal([]byte(body), &reqs); err == nil {
		return reqs
	}
	var req utilscontext.GraphQLRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		return nil
	}
	return []utilscontext.GraphQLRequest{req}
}
