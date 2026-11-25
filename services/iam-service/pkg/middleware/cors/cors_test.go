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
package cors

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func serve(t *testing.T, allowed []string, method, origin string, preflight bool) (*httptest.ResponseRecorder, bool) {
	t.Helper()

	called := false
	handler := New(allowed)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(method, "http://acme.localhost:8080/graphql", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if preflight {
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec, called
}

func TestPreflight(t *testing.T) {
	rec, called := serve(t, []string{"http://*.localhost:4300"}, http.MethodOptions, "http://acme.localhost:4300", true)

	assert.False(t, called, "a preflight request must not reach the next handler")
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "http://acme.localhost:4300", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "authorization,content-type", rec.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, allowedMethods, rec.Header().Get("Access-Control-Allow-Methods"))
}

func TestActualRequest(t *testing.T) {
	tests := []struct {
		name      string
		allowed   []string
		origin    string
		wantAllow string
	}{
		{name: "exact origin", allowed: []string{"http://portal.localhost:4300"}, origin: "http://portal.localhost:4300", wantAllow: "http://portal.localhost:4300"},
		{name: "pattern", allowed: []string{"http://*.localhost:4300"}, origin: "http://acme.localhost:4300", wantAllow: "http://acme.localhost:4300"},
		{name: "any origin", allowed: []string{"*"}, origin: "https://example.com", wantAllow: "https://example.com"},
		{name: "other port", allowed: []string{"http://*.localhost:4300"}, origin: "http://acme.localhost:4301"},
		{name: "pattern does not span a slash", allowed: []string{"http://*.localhost:4300"}, origin: "http://evil.example/.localhost:4300"},
		{name: "no origin", allowed: []string{"*"}},
		{name: "nothing allowed", origin: "http://acme.localhost:4300"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, called := serve(t, tt.allowed, http.MethodPost, tt.origin, false)

			assert.True(t, called)
			assert.Equal(t, tt.wantAllow, rec.Header().Get("Access-Control-Allow-Origin"))
		})
	}
}

func TestPreflightOfUnknownOriginIsPassedOn(t *testing.T) {
	rec, called := serve(t, []string{"http://portal.localhost:4300"}, http.MethodOptions, "http://other.localhost:4300", true)

	assert.True(t, called)
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}
