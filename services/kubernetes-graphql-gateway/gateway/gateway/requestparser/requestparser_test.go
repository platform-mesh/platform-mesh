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

package requestparser

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	utilscontext "go.platform-mesh.io/kubernetes-graphql-gateway/gateway/utils/context"
)

func TestMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		body           string
		expectParsed   bool
		expectedCount  int
		expectedQuery  string
		expectedExtKey string
	}{
		{
			name:          "single request",
			method:        http.MethodPost,
			body:          `{"query":"{ pods { name } }"}`,
			expectParsed:  true,
			expectedCount: 1,
			expectedQuery: "{ pods { name } }",
		},
		{
			name:           "single request with extensions",
			method:         http.MethodPost,
			body:           `{"query":"{ pods }","extensions":{"clusterTarget":"abc"}}`,
			expectParsed:   true,
			expectedCount:  1,
			expectedQuery:  "{ pods }",
			expectedExtKey: "clusterTarget",
		},
		{
			name:          "batched request",
			method:        http.MethodPost,
			body:          `[{"query":"{ pods }"},{"query":"{ nodes }"}]`,
			expectParsed:  true,
			expectedCount: 2,
			expectedQuery: "{ pods }",
		},
		{
			name:         "GET request skips parsing",
			method:       http.MethodGet,
			body:         "",
			expectParsed: false,
		},
		{
			name:         "invalid JSON",
			method:       http.MethodPost,
			body:         `not json`,
			expectParsed: false,
		},
		{
			name:         "empty body",
			method:       http.MethodPost,
			body:         "",
			expectParsed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedReqs []utilscontext.GraphQLRequest
			var found bool

			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedReqs, found = utilscontext.GetParsedRequestsFromCtx(r.Context())
				w.WriteHeader(http.StatusOK)
			})

			handler := Middleware(next)

			var body io.Reader
			if tt.body != "" {
				body = bytes.NewBufferString(tt.body)
			}
			req := httptest.NewRequest(tt.method, "/graphql", body)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if tt.expectParsed {
				if !found {
					t.Fatal("expected parsed requests in context, but none found")
				}
				if len(capturedReqs) != tt.expectedCount {
					t.Fatalf("expected %d requests, got %d", tt.expectedCount, len(capturedReqs))
				}
				if capturedReqs[0].Query != tt.expectedQuery {
					t.Errorf("expected query %q, got %q", tt.expectedQuery, capturedReqs[0].Query)
				}
				if tt.expectedExtKey != "" {
					if capturedReqs[0].Extensions == nil {
						t.Fatal("expected extensions, got nil")
					}
					if _, ok := capturedReqs[0].Extensions[tt.expectedExtKey]; !ok {
						t.Errorf("expected extension key %q not found", tt.expectedExtKey)
					}
				}
			} else {
				if found {
					t.Errorf("expected no parsed requests, but found %d", len(capturedReqs))
				}
			}
		})
	}
}

func TestMiddleware_RestoresBody(t *testing.T) {
	originalBody := `{"query":"{ pods { name } }","extensions":{"clusterTarget":"abc123"}}`

	var capturedBody string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read body in next handler: %v", err)
		}
		capturedBody = string(data)
	})

	handler := Middleware(next)

	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewBufferString(originalBody))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if capturedBody != originalBody {
		t.Errorf("body was not restored: expected %q, got %q", originalBody, capturedBody)
	}
}
