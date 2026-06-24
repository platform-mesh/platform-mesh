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

package roundtripper

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	utilscontext "go.platform-mesh.io/kubernetes-graphql-gateway/gateway/utils/context"
)

func TestPathTemplateHandler_RoundTrip(t *testing.T) {
	tests := []struct {
		name           string
		prefix         string
		basePath       string
		clusterTarget  string
		requestPath    string
		expectedPath   string
		expectPassthru bool
	}{
		{
			name:          "prefix includes base path",
			prefix:        "/services/marketplace/clusters/{clusterTarget}",
			basePath:      "/services/marketplace",
			clusterTarget: "abc123",
			requestPath:   "/services/marketplace/api/v1/pods",
			expectedPath:  "/services/marketplace/clusters/abc123/api/v1/pods",
		},
		{
			name:          "prefix without base path",
			prefix:        "/clusters/{clusterTarget}",
			basePath:      "/services/marketplace",
			clusterTarget: "abc123",
			requestPath:   "/services/marketplace/api/v1/pods",
			expectedPath:  "/clusters/abc123/api/v1/pods",
		},
		{
			name:          "custom prefix with base path",
			prefix:        "/base/tenants/{clusterTarget}/proxy",
			basePath:      "/base",
			clusterTarget: "tenant-1",
			requestPath:   "/base/apis/apps/v1/deployments",
			expectedPath:  "/base/tenants/tenant-1/proxy/apis/apps/v1/deployments",
		},
		{
			name:          "empty base path prepends prefix",
			prefix:        "/clusters/{clusterTarget}",
			basePath:      "",
			clusterTarget: "abc123",
			requestPath:   "/api/v1/pods",
			expectedPath:  "/clusters/abc123/api/v1/pods",
		},
		{
			name:           "empty prefix passes through",
			prefix:         "",
			clusterTarget:  "abc123",
			requestPath:    "/api/v1/pods",
			expectedPath:   "/api/v1/pods",
			expectPassthru: true,
		},
		{
			name:           "missing cluster target passes through",
			prefix:         "/clusters/{clusterTarget}",
			clusterTarget:  "",
			requestPath:    "/api/v1/pods",
			expectedPath:   "/api/v1/pods",
			expectPassthru: true,
		},
		{
			name:           "no cluster target in context passes through",
			prefix:         "/clusters/{clusterTarget}",
			requestPath:    "/api/v1/pods",
			expectedPath:   "/api/v1/pods",
			expectPassthru: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedPath string
			inner := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				capturedPath = req.URL.Path
				return &http.Response{StatusCode: http.StatusOK}, nil
			})

			handler := NewPathTemplateHandler(inner, tt.prefix, tt.basePath)

			ctx := context.Background()
			if tt.clusterTarget != "" {
				ctx = utilscontext.SetClusterTarget(ctx, tt.clusterTarget)
			}

			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com"+tt.requestPath, nil)
			_, err := handler.RoundTrip(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if capturedPath != tt.expectedPath {
				t.Errorf("expected path %q, got %q", tt.expectedPath, capturedPath)
			}
		})
	}
}

func TestPathTemplateHandler_DoesNotMutateOriginalRequest(t *testing.T) {
	inner := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK}, nil
	})

	handler := NewPathTemplateHandler(inner, "/clusters/{clusterTarget}", "")

	ctx := utilscontext.SetClusterTarget(context.Background(), "abc123")
	originalURL := &url.URL{Scheme: "https", Host: "example.com", Path: "/api/v1/pods"}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, originalURL.String(), nil)

	originalPath := req.URL.Path
	_, err := handler.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.URL.Path != originalPath {
		t.Errorf("original request URL was mutated: expected %q, got %q", originalPath, req.URL.Path)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
