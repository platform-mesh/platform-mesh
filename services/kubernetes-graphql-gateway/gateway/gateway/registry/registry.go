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

package registry

import (
	"context"
	"sync"
	"time"

	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/config"
	"go.platform-mesh.io/kubernetes-graphql-gateway/gateway/gateway/endpoint"

	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Registry manages multiple endpoints (cluster + GraphQL handler pairs).
type Registry struct {
	mu        sync.RWMutex
	endpoints map[string]*endpoint.Endpoint
	failed    sets.Set[string]
	config    config.Gateway
}

// New creates a new endpoint registry.
func New(cfg config.Gateway) *Registry {
	return &Registry{
		endpoints: make(map[string]*endpoint.Endpoint),
		failed:    sets.New[string](),
		config:    cfg,
	}
}

// OnSchemaChanged implements watcher.SchemaEventHandler.
// It is called when a schema is created or updated.
func (r *Registry) OnSchemaChanged(ctx context.Context, clusterName string, schema []byte) {
	logger := log.FromContext(ctx)
	logger.V(4).Info("Loading endpoint", "cluster", clusterName)

	// Use a scoped timeout so that a slow endpoint creation does not block
	// the watcher indefinitely. The timeout only applies to creation, not to
	// the endpoint's lifetime.
	createCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Create endpoint outside the lock to avoid holding it during slow operations
	ep, err := endpoint.New(
		createCtx,
		clusterName,
		schema,
		r.config.GraphQL,
		r.config.Limits,
		r.config.TokenReviewCacheTTL,
		r.config.Validator,
		r.config.Metrics,
		r.config.ClusterOptions,
	)

	r.mu.Lock()
	defer r.mu.Unlock()

	if old, exists := r.endpoints[clusterName]; exists {
		old.Close()
		delete(r.endpoints, clusterName)
		logger.V(4).Info("Removed existing endpoint", "cluster", clusterName)
	}

	if err != nil {
		logger.Error(err, "Failed to create endpoint", "cluster", clusterName)
		r.failed.Insert(clusterName)
		return
	}

	r.failed.Delete(clusterName)
	r.endpoints[clusterName] = ep
	logger.Info("Successfully loaded endpoint", "cluster", clusterName)
}

// OnSchemaDeleted implements watcher.SchemaEventHandler.
// It is called when a schema is removed.
func (r *Registry) OnSchemaDeleted(ctx context.Context, clusterName string) {
	logger := log.FromContext(ctx)

	r.mu.Lock()
	defer r.mu.Unlock()

	logger.V(4).Info("Removing endpoint", "cluster", clusterName)

	r.failed.Delete(clusterName)
	old, exists := r.endpoints[clusterName]
	if !exists {
		logger.V(2).Info("Attempted to remove non-existent endpoint", "cluster", clusterName)
		return
	}

	old.Close()
	delete(r.endpoints, clusterName)
	logger.Info("Successfully removed endpoint", "cluster", clusterName)
}

// GetEndpoint returns an endpoint by cluster name.
func (r *Registry) GetEndpoint(name string) (*endpoint.Endpoint, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ep, exists := r.endpoints[name]
	return ep, exists
}

// SchemaFailed reports whether the last schema received for a cluster could not be loaded.
func (r *Registry) SchemaFailed(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.failed.Has(name)
}
