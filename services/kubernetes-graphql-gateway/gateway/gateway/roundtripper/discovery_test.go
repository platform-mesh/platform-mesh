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
	"net/http"
	"testing"
)

func TestIsDiscoveryRequest(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		path     string
		expected bool
	}{
		// Plain Kubernetes API paths
		{name: "api root", method: http.MethodGet, path: "/api", expected: true},
		{name: "apis root", method: http.MethodGet, path: "/apis", expected: true},
		{name: "api version", method: http.MethodGet, path: "/api/v1", expected: true},
		{name: "api group", method: http.MethodGet, path: "/apis/apps", expected: true},
		{name: "api group version", method: http.MethodGet, path: "/apis/apps/v1", expected: true},

		// KCP paths: /services/{ws}/clusters/{cl}/...
		{name: "kcp services api", method: http.MethodGet, path: "/services/ws/clusters/cl/api", expected: true},
		{name: "kcp services api version", method: http.MethodGet, path: "/services/ws/clusters/cl/api/v1", expected: true},
		{name: "kcp services apis group", method: http.MethodGet, path: "/services/ws/clusters/cl/apis/apps", expected: true},
		{name: "kcp services apis group version", method: http.MethodGet, path: "/services/ws/clusters/cl/apis/apps/v1", expected: true},

		// KCP paths: /clusters/{cl}/...
		{name: "kcp clusters api", method: http.MethodGet, path: "/clusters/cl/api", expected: true},
		{name: "kcp clusters api version", method: http.MethodGet, path: "/clusters/cl/api/v1", expected: true},
		{name: "kcp clusters apis group", method: http.MethodGet, path: "/clusters/cl/apis/apps", expected: true},

		// Custom virtual workspace paths
		{name: "custom vw prefix api", method: http.MethodGet, path: "/tenants/t1/proxy/api", expected: true},
		{name: "custom vw prefix apis", method: http.MethodGet, path: "/tenants/t1/proxy/apis/apps/v1", expected: true},
		{name: "deep prefix api", method: http.MethodGet, path: "/a/b/c/d/api/v1", expected: true},

		// OpenAPI discovery
		{name: "openapi v2", method: http.MethodGet, path: "/openapi/v2", expected: true},
		{name: "openapi v3", method: http.MethodGet, path: "/openapi/v3", expected: true},
		{name: "openapi with prefix", method: http.MethodGet, path: "/services/ws/clusters/cl/openapi/v2", expected: true},

		// Prefix contains literal "api" segment — must not shadow the real K8s root
		{name: "prefix api segment discovery", method: http.MethodGet, path: "/services/api/clusters/cl/api/v1", expected: true},
		{name: "prefix api segment resource", method: http.MethodGet, path: "/services/api/clusters/cl/api/v1/pods", expected: false},
		{name: "prefix apis segment discovery", method: http.MethodGet, path: "/prefix/apis/proxy/apis/apps", expected: true},

		// Non-discovery requests
		{name: "resource path", method: http.MethodGet, path: "/api/v1/namespaces", expected: false},
		{name: "resource list", method: http.MethodGet, path: "/api/v1/pods", expected: false},
		{name: "custom resource", method: http.MethodGet, path: "/apis/apps/v1/deployments", expected: false},
		{name: "post method", method: http.MethodPost, path: "/api", expected: false},
		{name: "empty path", method: http.MethodGet, path: "/", expected: false},
		{name: "non-api path", method: http.MethodGet, path: "/healthz", expected: false},
		{name: "metrics path", method: http.MethodGet, path: "/metrics", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(tt.method, "https://example.com"+tt.path, nil)
			got := isDiscoveryRequest(req)
			if got != tt.expected {
				t.Errorf("isDiscoveryRequest(%s %s) = %v, want %v", tt.method, tt.path, got, tt.expected)
			}
		})
	}
}
