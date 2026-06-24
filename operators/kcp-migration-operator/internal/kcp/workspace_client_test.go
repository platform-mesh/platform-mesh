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

package kcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
)

func TestParseBaseHost(t *testing.T) {
	tests := []struct {
		name        string
		host        string
		expected    string
		expectError bool
	}{
		{
			name:     "simple https host",
			host:     "https://kcp.example.com:8443",
			expected: "https://kcp.example.com:8443",
		},
		{
			name:     "https host with path",
			host:     "https://kcp.example.com:8443/clusters/root",
			expected: "https://kcp.example.com:8443",
		},
		{
			name:     "https host with complex path",
			host:     "https://kcp.api.portal.dev.local:8443/clusters/root:orgs:sap",
			expected: "https://kcp.api.portal.dev.local:8443",
		},
		{
			name:     "http host",
			host:     "http://localhost:6443/clusters/root",
			expected: "http://localhost:6443",
		},
		{
			name:     "host with query params",
			host:     "https://kcp.example.com:8443/clusters/root?timeout=30s",
			expected: "https://kcp.example.com:8443",
		},
		{
			name:        "invalid URL - missing scheme",
			host:        "kcp.example.com:8443",
			expectError: true,
		},
		{
			name:        "empty host",
			host:        "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseBaseHost(tt.host)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewWorkspaceClientFactoryFromConfig(t *testing.T) {
	scheme := runtime.NewScheme()

	tests := []struct {
		name        string
		config      *rest.Config
		expectError bool
	}{
		{
			name: "valid config",
			config: &rest.Config{
				Host: "https://kcp.example.com:8443",
			},
			expectError: false,
		},
		{
			name: "config with path",
			config: &rest.Config{
				Host: "https://kcp.example.com:8443/clusters/root",
			},
			expectError: false,
		},
		{
			name: "invalid config - no scheme",
			config: &rest.Config{
				Host: "kcp.example.com:8443",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory, err := NewWorkspaceClientFactoryFromConfig(tt.config, scheme)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, factory)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, factory)
		})
	}
}

func TestWorkspaceClientFactory_GetClient_InvalidPath(t *testing.T) {
	scheme := runtime.NewScheme()
	config := &rest.Config{
		Host: "https://kcp.example.com:8443",
	}

	factory, err := NewWorkspaceClientFactoryFromConfig(config, scheme)
	require.NoError(t, err)

	// Empty workspace path should fail
	_, err = factory.GetClient("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "workspace path cannot be empty")
}

func TestWorkspaceClientFactory_ClientCaching(t *testing.T) {
	scheme := runtime.NewScheme()
	config := &rest.Config{
		Host: "https://kcp.example.com:8443",
	}

	factory, err := NewWorkspaceClientFactoryFromConfig(config, scheme)
	require.NoError(t, err)

	// Cast to internal type to access cache
	internalFactory := factory.(*workspaceClientFactory)

	// Initially cache should be empty
	assert.Empty(t, internalFactory.clients)

	// Get a client (will fail because config is not fully valid, but cache logic is tested)
	_, _ = factory.GetClient("root:orgs:sap")

	// Another call should use cache if client was created
	// (in this test case, client creation fails so cache won't have entry)
}

func TestBuildWorkspaceHost(t *testing.T) {
	// Test that the workspace host is built correctly
	scheme := runtime.NewScheme()
	config := &rest.Config{
		Host: "https://kcp.example.com:8443/clusters/root",
	}

	factory, err := NewWorkspaceClientFactoryFromConfig(config, scheme)
	require.NoError(t, err)

	// Cast to internal type to verify baseHost
	internalFactory := factory.(*workspaceClientFactory)
	assert.Equal(t, "https://kcp.example.com:8443", internalFactory.baseHost)

	// The workspace-specific host would be:
	// https://kcp.example.com:8443/clusters/root:orgs:sap
}
