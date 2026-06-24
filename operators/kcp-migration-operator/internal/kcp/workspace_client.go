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
	"fmt"
	"net/url"
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// WorkspaceClientFactory creates Kubernetes clients for specific kcp workspaces
//
//go:generate mockery --name=WorkspaceClientFactory --output=../../pkg/mocks --filename=mock_workspace_client_factory.go
type WorkspaceClientFactory interface {
	// GetClient returns a Kubernetes client configured for the specified workspace path
	// The workspace path should be in kcp format, e.g., "root:orgs:sap"
	GetClient(workspacePath string) (ctrlruntimeclient.Client, error)
}

// workspaceClientFactory implements WorkspaceClientFactory
type workspaceClientFactory struct {
	baseConfig *rest.Config
	scheme     *runtime.Scheme
	baseHost   string

	// Client cache to avoid creating new clients for each request
	mu      sync.RWMutex
	clients map[string]ctrlruntimeclient.Client
}

// NewWorkspaceClientFactory creates a new WorkspaceClientFactory from a kcp kubeconfig file
func NewWorkspaceClientFactory(kcpKubeconfigPath string, scheme *runtime.Scheme) (WorkspaceClientFactory, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kcpKubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig from %s: %w", kcpKubeconfigPath, err)
	}

	// Parse the base host from the config
	baseHost, err := parseBaseHost(config.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base host from config: %w", err)
	}

	return &workspaceClientFactory{
		baseConfig: config,
		scheme:     scheme,
		baseHost:   baseHost,
		clients:    make(map[string]ctrlruntimeclient.Client),
	}, nil
}

// NewWorkspaceClientFactoryFromConfig creates a new WorkspaceClientFactory from an existing rest.Config
// This is useful for testing where you want to inject a custom config
func NewWorkspaceClientFactoryFromConfig(config *rest.Config, scheme *runtime.Scheme) (WorkspaceClientFactory, error) {
	baseHost, err := parseBaseHost(config.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base host from config: %w", err)
	}

	return &workspaceClientFactory{
		baseConfig: config,
		scheme:     scheme,
		baseHost:   baseHost,
		clients:    make(map[string]ctrlruntimeclient.Client),
	}, nil
}

// GetClient returns a Kubernetes client configured for the specified workspace path
func (f *workspaceClientFactory) GetClient(workspacePath string) (ctrlruntimeclient.Client, error) {
	if workspacePath == "" {
		return nil, fmt.Errorf("workspace path cannot be empty")
	}

	// Check cache first
	f.mu.RLock()
	if c, ok := f.clients[workspacePath]; ok {
		f.mu.RUnlock()
		return c, nil
	}
	f.mu.RUnlock()

	// Create new client
	f.mu.Lock()
	defer f.mu.Unlock()

	// Double-check after acquiring write lock
	if c, ok := f.clients[workspacePath]; ok {
		return c, nil
	}

	// Create a copy of the base config with workspace-specific host
	workspaceConfig := rest.CopyConfig(f.baseConfig)
	workspaceConfig.Host = f.baseHost + "/clusters/" + workspacePath

	c, err := ctrlruntimeclient.New(workspaceConfig, ctrlruntimeclient.Options{Scheme: f.scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create client for workspace %s: %w", workspacePath, err)
	}

	f.clients[workspacePath] = c
	return c, nil
}

// parseBaseHost extracts the base host (scheme + host) from a URL
// For example, "https://kcp.example.com:8443/clusters/root" -> "https://kcp.example.com:8443"
func parseBaseHost(host string) (string, error) {
	u, err := url.Parse(host)
	if err != nil {
		return "", fmt.Errorf("invalid host URL: %w", err)
	}

	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("host URL must have scheme and host: %s", host)
	}

	return u.Scheme + "://" + u.Host, nil
}
